// Package supervisor runs a long-lived worker function and restarts it with exponential
// backoff when it fails.
package supervisor

import (
	"context"
	"log/slog"
	"time"
)

const (
	minBackoff = time.Second
	maxBackoff = 60 * time.Second
	// A worker that ran this long is considered healthy and resets the backoff.
	stableAfter = 5 * time.Minute
)

type State string

const (
	Starting State = "starting"
	Running  State = "running"
	Degraded State = "degraded"
	Offline  State = "offline"
	Disabled State = "disabled"
)

// Hooks receive state changes; they must not block.
type Hooks struct {
	OnStart   func()
	OnFailure func(err error, failures int, retryIn time.Duration)
}

// Run calls fn until ctx is cancelled. fn should block while working and return an error
// when it stops unexpectedly.
func Run(ctx context.Context, log *slog.Logger, fn func(ctx context.Context) error, h Hooks) {
	backoff := minBackoff
	failures := 0
	for ctx.Err() == nil {
		if h.OnStart != nil {
			h.OnStart()
		}
		started := time.Now()
		err := fn(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > stableAfter {
			backoff, failures = minBackoff, 0
		}
		failures++
		log.Warn("worker stopped", "err", err, "failures", failures, "retry_in", backoff.String())
		if h.OnFailure != nil {
			h.OnFailure(err, failures, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}
