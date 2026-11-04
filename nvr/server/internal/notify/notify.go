// Package notify sends alerts (camera offline, disk low, ...) to webhooks. The body
// follows the target:
//   - Discord webhook (discord.com/api/webhooks/...): an embed with fields, native
//     timestamps and, when available, the camera's latest snapshot attached.
//   - go-whatsapp-web-multidevice (gowa), URL ending in /send/message?phone=62xxx:
//     a WhatsApp text message; user:password in the URL becomes basic auth.
//   - anything else: the Message as JSON.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"time"
)

type Level string

const (
	Alert    Level = "alert"
	Warning  Level = "warning"
	Resolved Level = "resolved"
	Info     Level = "info"
)

// Field is one labelled value. When At is set the value is a moment in time, rendered
// natively by Discord (viewer's own time zone) and as WIB text elsewhere.
type Field struct {
	Name   string     `json:"name"`
	Value  string     `json:"value,omitempty"`
	At     *time.Time `json:"at,omitempty"`
	Inline bool       `json:"-"`
}

type Message struct {
	Kind   string    `json:"kind"` // camera_offline, camera_recovered, disk_low, disk_ok, startup, test
	Level  Level     `json:"level"`
	Title  string    `json:"title"`
	Text   string    `json:"text"`
	Fields []Field   `json:"fields,omitempty"`
	Camera string    `json:"camera,omitempty"`
	URL    string    `json:"url,omitempty"`
	At     time.Time `json:"at"`
	// Image is a local JPEG attached where the target supports it.
	Image      string `json:"-"`
	ImageLabel string `json:"-"`
}

type Notifier struct {
	URLs     []string
	Client   *http.Client
	Location *time.Location
	Footer   string
	Log      *slog.Logger
}

func New(urls []string, loc *time.Location, footer string, log *slog.Logger) *Notifier {
	if loc == nil {
		loc = time.UTC
	}
	return &Notifier{URLs: urls, Client: &http.Client{Timeout: 15 * time.Second}, Location: loc, Footer: footer, Log: log}
}

func (n *Notifier) Enabled() bool { return n != nil && len(n.URLs) > 0 }

// Send delivers m to every webhook, retrying each a few times; it returns the joined errors.
func (n *Notifier) Send(ctx context.Context, m Message) error {
	if !n.Enabled() {
		return nil
	}
	if m.At.IsZero() {
		m.At = time.Now().UTC()
	}
	var errs []error
	for _, u := range n.URLs {
		if err := n.post(ctx, u, m); err != nil {
			n.Log.Warn("notification failed", "kind", m.Kind, "target", redact(u), "err", err)
			errs = append(errs, fmt.Errorf("%s: %w", redact(u), err))
		}
	}
	return errors.Join(errs...)
}

func (n *Notifier) post(ctx context.Context, u string, m Message) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * 2 * time.Second):
			}
		}
		var req *http.Request
		if req, err = n.request(ctx, u, m); err != nil {
			return err
		}
		var resp *http.Response
		if resp, err = n.Client.Do(req); err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode < 300 {
			return nil
		}
		err = fmt.Errorf("HTTP %d", resp.StatusCode)
		if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return err // a bad URL or payload will not fix itself
		}
	}
	return err
}

// request builds the POST for one target. Credentials in the URL move to an
// Authorization header, and gowa's phone parameter moves into the body.
func (n *Notifier) request(ctx context.Context, u string, m Message) (*http.Request, error) {
	p, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	user := p.User
	p.User = nil
	contentType := "application/json"
	var body []byte
	switch {
	case isGowa(u):
		body, err = json.Marshal(map[string]string{"phone": p.Query().Get("phone"), "message": n.plainText(m)})
		p.RawQuery = ""
	case isDiscord(u):
		body, contentType, err = n.discord(m)
	default:
		body, err = json.Marshal(m)
	}
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "nvr-lite")
	if user != nil {
		pw, _ := user.Password()
		req.SetBasicAuth(user.Username(), pw)
	}
	return req, nil
}

var levelColor = map[Level]int{Alert: 0xE5484D, Warning: 0xF5A524, Resolved: 0x30A46C, Info: 0x3E63DD}

// discord renders m as an embed; with an image it becomes a multipart upload so the
// snapshot shows inline without being publicly hosted anywhere.
func (n *Notifier) discord(m Message) ([]byte, string, error) {
	fields := make([]map[string]any, 0, len(m.Fields))
	for _, f := range m.Fields {
		v := f.Value
		if f.At != nil {
			v = fmt.Sprintf("<t:%d:f>\n<t:%d:R>", f.At.Unix(), f.At.Unix())
		}
		if v == "" {
			v = "—"
		}
		fields = append(fields, map[string]any{"name": f.Name, "value": truncate(v, 1024), "inline": f.Inline})
	}
	embed := map[string]any{
		"title":       truncate(m.Title, 256),
		"description": truncate(m.Text, 4096),
		"color":       levelColor[m.Level],
		"timestamp":   m.At.UTC().Format(time.RFC3339),
		"fields":      fields,
		"footer":      map[string]string{"text": n.Footer},
	}
	if m.URL != "" {
		embed["url"] = m.URL
	}
	payload := map[string]any{"username": "NVR Lite", "embeds": []any{embed}}

	img, _ := os.ReadFile(m.Image)
	if m.Image == "" || len(img) == 0 || len(img) > 8<<20 {
		b, err := json.Marshal(payload)
		return b, "application/json", err
	}
	embed["image"] = map[string]string{"url": "attachment://snapshot.jpg"}
	payload["attachments"] = []any{map[string]any{"id": 0, "filename": "snapshot.jpg", "description": m.ImageLabel}}
	pj, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("payload_json", string(pj)); err != nil {
		return nil, "", err
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="files[0]"; filename="snapshot.jpg"`)
	h.Set("Content-Type", "image/jpeg")
	fw, err := mw.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	if _, err := fw.Write(img); err != nil {
		return nil, "", err
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// plainText renders m for chat apps without embeds (WhatsApp markup).
func (n *Notifier) plainText(m Message) string {
	var b strings.Builder
	b.WriteString("*" + m.Title + "*")
	if m.Text != "" {
		b.WriteString("\n" + m.Text)
	}
	for _, f := range m.Fields {
		v := f.Value
		if f.At != nil {
			v = f.At.In(n.Location).Format("02 Jan 15:04 MST")
		}
		b.WriteString("\n• " + f.Name + ": " + strings.ReplaceAll(v, "\n", " "))
	}
	if m.URL != "" {
		b.WriteString("\n" + m.URL)
	}
	return b.String()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func isGowa(u string) bool {
	p, err := url.Parse(u)
	return err == nil && strings.HasSuffix(p.Path, "/send/message") && p.Query().Get("phone") != ""
}

func isDiscord(u string) bool {
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	h := strings.ToLower(p.Hostname())
	return (h == "discord.com" || h == "discordapp.com" || strings.HasSuffix(h, ".discord.com")) &&
		strings.HasPrefix(p.Path, "/api/webhooks/")
}

// redact keeps webhook secrets (credentials, path, query) out of logs and API responses.
func redact(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return "webhook"
	}
	return p.Scheme + "://" + p.Host + "/…"
}
