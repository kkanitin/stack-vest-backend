// Package worker runs background jobs alongside the HTTP server.
package worker

import (
	"context"
	"time"
)

// Periodic runs a job once at start and then on a fixed interval until stopped.
// Runs never overlap: a run that outlasts the interval delays the next one.
type Periodic struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// StartPeriodic starts the job in its own goroutine. fn receives a context that is
// cancelled when Stop is called, so a long run can abandon its work on shutdown.
func StartPeriodic(interval time.Duration, fn func(ctx context.Context)) *Periodic {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Periodic{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			fn(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			// When both are ready select picks at random; never start a run after Stop.
			if ctx.Err() != nil {
				return
			}
		}
	}()
	return p
}

// Stop cancels the job and waits for an in-flight run to return, or for ctx to end,
// whichever comes first. It is safe to call more than once.
func (p *Periodic) Stop(ctx context.Context) {
	p.cancel()
	select {
	case <-p.done:
	case <-ctx.Done():
	}
}
