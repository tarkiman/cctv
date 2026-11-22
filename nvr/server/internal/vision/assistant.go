package vision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Assistant forwards door events to the home voice assistant (Echa, kawan-bridge
// POST /cctv): it greets a known person by name and asks a stranger at the gate what
// they need, on the speaker at home, and puts a notice on Abi's phone. The URL carries
// its token (…/cctv?token=…); nil or an empty URL = off.
type Assistant struct {
	URL  string
	Log  *slog.Logger
	http *http.Client
}

func NewAssistant(url string, log *slog.Logger) *Assistant {
	if url == "" {
		return nil
	}
	return &Assistant{URL: url, Log: log, http: &http.Client{Timeout: 10 * time.Second}}
}

// AssistantEvent is what the assistant receives.
type AssistantEvent struct {
	Kind    string    `json:"kind"` // "person_arrived" or "visitor"
	Camera  string    `json:"camera"`
	Name    string    `json:"name,omitempty"`    // person_arrived
	Score   float64   `json:"score,omitempty"`   // person_arrived: face similarity
	Seconds int       `json:"seconds,omitempty"` // visitor: how long they have been at the gate
	At      time.Time `json:"at"`
}

// Send posts an event in the background (the assistant may be slow or restarting; a
// greeting that arrives late is not worth retrying).
func (a *Assistant) Send(e AssistantEvent) {
	if a == nil {
		return
	}
	go func() {
		body, _ := json.Marshal(e)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := a.http.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				err = fmt.Errorf("status %s", resp.Status)
			}
		}
		if err != nil {
			// The error may quote the URL, which carries the token.
			a.Log.Warn("assistant event failed", "kind", e.Kind, "err", redactURL(err.Error(), a.URL))
			return
		}
		a.Log.Info("assistant event sent", "kind", e.Kind, "name", e.Name)
	}()
}

func redactURL(s, url string) string {
	return string(bytes.ReplaceAll([]byte(s), []byte(url), []byte("<assistant-url>")))
}
