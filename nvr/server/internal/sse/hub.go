// Package sse fans out server-sent events to connected browsers.
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type message struct {
	event string
	data  []byte
}

type Hub struct {
	mu   sync.Mutex
	subs map[chan message]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan message]struct{}{}} }

// Publish sends an event to every subscriber. Slow subscribers miss events rather than
// blocking the publisher.
func (h *Hub) Publish(event string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- message{event, data}:
		default:
		}
	}
}

// Serve streams events until the client disconnects. hello, if set, is sent first so
// the client starts from a complete state.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, hello func(send func(event string, v any))) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan message, 32)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}()

	write := func(event string, data []byte) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	}
	fmt.Fprint(w, "retry: 3000\n\n")
	if hello != nil {
		hello(func(event string, v any) {
			if data, err := json.Marshal(v); err == nil {
				write(event, data)
			}
		})
	}
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-ch:
			write(m.event, m.data)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
