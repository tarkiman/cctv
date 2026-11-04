// Package go2rtc is a small client for the go2rtc HTTP API.
package go2rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Client struct {
	base string
	http *http.Client
}

func New(base string) *Client {
	return &Client{base: base, http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) Base() string { return c.base }

type Producer struct {
	URL string `json:"url"`
}

type Stream struct {
	Producers []Producer      `json:"producers"`
	Consumers json.RawMessage `json:"consumers"`
}

// IsXiaomi reports whether the stream's source uses Xiaomi P2P (which needs a cloud session).
func (s Stream) IsXiaomi() bool {
	for _, p := range s.Producers {
		if strings.HasPrefix(p.URL, "xiaomi://") {
			return true
		}
	}
	return false
}

// Streams returns every stream configured in go2rtc, keyed by name.
func (c *Client) Streams(ctx context.Context) (map[string]Stream, error) {
	var out map[string]Stream
	if err := c.getJSON(ctx, "/api/streams", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Version returns the go2rtc version string, or "" when unknown.
func (c *Client) Version(ctx context.Context) string {
	var info struct {
		Version string `json:"version"`
	}
	if err := c.getJSON(ctx, "/api", &info); err != nil {
		return ""
	}
	return info.Version
}

// PutStream creates or replaces a stream. go2rtc persists PUT changes to go2rtc.yaml
// (PATCH only changes memory), so this is the call to use for durable sync.
func (c *Client) PutStream(ctx context.Context, name, src string) error {
	q := url.Values{"name": {name}, "src": {src}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+"/api/streams?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// DeleteStream removes a stream; like PUT, go2rtc writes the change to go2rtc.yaml.
func (c *Client) DeleteStream(ctx context.Context, name string) error {
	q := url.Values{"src": {name}}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+"/api/streams?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// Sources returns each stream's configured source URLs from go2rtc.yaml. Unlike
// Streams, this works whether or not the stream is running (a running producer
// reports its format, not its URL).
func (c *Client) Sources(ctx context.Context) (map[string][]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/config", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("go2rtc GET /api/config: %s", resp.Status)
	}
	var cfg struct {
		Streams map[string]any `yaml:"streams"`
	}
	if err := yaml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&cfg); err != nil && err != io.EOF {
		return nil, fmt.Errorf("parse go2rtc config: %w", err)
	}
	return ParseSources(cfg.Streams), nil
}

// ParseSources normalises the streams section: each value may be a string or a list.
func ParseSources(streams map[string]any) map[string][]string {
	out := map[string][]string{}
	for name, v := range streams {
		switch t := v.(type) {
		case string:
			out[name] = []string{t}
		case []any:
			for _, x := range t {
				if s, ok := x.(string); ok {
					out[name] = append(out[name], s)
				}
			}
		default:
			out[name] = nil
		}
	}
	return out
}

// IsXiaomiSource reports whether any of the sources uses Xiaomi P2P.
func IsXiaomiSource(srcs []string) bool {
	for _, s := range srcs {
		if strings.HasPrefix(s, "xiaomi://") {
			return true
		}
	}
	return false
}

// WebRTC exchanges an SDP offer for an answer for the given stream.
func (c *Client) WebRTC(ctx context.Context, stream string, offer io.Reader) ([]byte, error) {
	q := url.Values{"src": {stream}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/webrtc?"+q.Encode(), offer)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/sdp")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("go2rtc webrtc: %s", resp.Status)
	}
	return body, nil
}

// WSURL is the websocket URL for live MSE streaming of a stream.
func (c *Client) WSURL(stream string) string {
	u := strings.Replace(c.base, "http", "ws", 1)
	return u + "/api/ws?" + url.Values{"src": {stream}}.Encode()
}

func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, v)
}

func (c *Client) do(req *http.Request, v any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("go2rtc %s %s: %s", req.Method, req.URL.Path, resp.Status)
	}
	if v == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
