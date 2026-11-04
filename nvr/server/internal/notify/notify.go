// Package notify sends alerts (camera offline, disk low, ...) to webhooks. The body
// follows the target:
//   - Discord webhook (discord.com/api/webhooks/...): a Discord embed.
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
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Level string

const (
	Alert    Level = "alert"
	Resolved Level = "resolved"
	Info     Level = "info"
)

type Message struct {
	Kind   string    `json:"kind"` // camera_offline, camera_recovered, disk_low, disk_ok, startup, test
	Level  Level     `json:"level"`
	Title  string    `json:"title"`
	Text   string    `json:"text"`
	Camera string    `json:"camera,omitempty"`
	URL    string    `json:"url,omitempty"`
	At     time.Time `json:"at"`
}

type Notifier struct {
	URLs   []string
	Client *http.Client
	Log    *slog.Logger
}

func New(urls []string, log *slog.Logger) *Notifier {
	return &Notifier{URLs: urls, Client: &http.Client{Timeout: 10 * time.Second}, Log: log}
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
		body, err := payload(u, m)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := n.post(ctx, u, body); err != nil {
			n.Log.Warn("notification failed", "kind", m.Kind, "target", redact(u), "err", err)
			errs = append(errs, fmt.Errorf("%s: %w", redact(u), err))
		}
	}
	return errors.Join(errs...)
}

func (n *Notifier) post(ctx context.Context, u string, body []byte) error {
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
		if req, err = newRequest(ctx, u, body); err != nil {
			return err
		}
		req.Header.Set("User-Agent", "nvr-lite")
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

// newRequest builds the POST; credentials in the URL move to an Authorization header, and
// gowa's phone parameter moves into the body.
func newRequest(ctx context.Context, u string, body []byte) (*http.Request, error) {
	p, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	user := p.User
	p.User = nil
	if isGowa(u) {
		p.RawQuery = ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if user != nil {
		pw, _ := user.Password()
		req.SetBasicAuth(user.Username(), pw)
	}
	return req, nil
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

func payload(u string, m Message) ([]byte, error) {
	if isGowa(u) {
		p, _ := url.Parse(u)
		text := "*" + m.Title + "*"
		if m.Text != "" {
			text += "\n" + m.Text
		}
		if m.URL != "" {
			text += "\n" + m.URL
		}
		return json.Marshal(map[string]string{"phone": p.Query().Get("phone"), "message": text})
	}
	if !isDiscord(u) {
		return json.Marshal(m)
	}
	color := map[Level]int{Alert: 0xE5484D, Resolved: 0x30A46C, Info: 0x3E63DD}[m.Level]
	embed := map[string]any{
		"title":       m.Title,
		"description": m.Text,
		"color":       color,
		"timestamp":   m.At.Format(time.RFC3339),
		"footer":      map[string]string{"text": "NVR Lite"},
	}
	if m.URL != "" {
		embed["url"] = m.URL
	}
	return json.Marshal(map[string]any{"username": "NVR Lite", "embeds": []any{embed}})
}

// redact keeps webhook secrets (path, query) out of logs and API responses.
func redact(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return "webhook"
	}
	return p.Scheme + "://" + p.Host + "/…"
}
