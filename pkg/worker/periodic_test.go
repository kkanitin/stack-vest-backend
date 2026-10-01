package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestStartPeriodic_RunsAtStartAndThenOnTheInterval(t *testing.T) {
	var runs atomic.Int64
	// An interval far longer than the test proves the first run does not wait for a tick.
	p := StartPeriodic(time.Hour, func(context.Context) { runs.Add(1) })
	waitFor(t, "the immediate first run", func() bool { return runs.Load() == 1 })
	p.Stop(context.Background())

	runs.Store(0)
	p = StartPeriodic(5*time.Millisecond, func(context.Context) { runs.Add(1) })
	waitFor(t, "repeat runs", func() bool { return runs.Load() >= 3 })
	p.Stop(context.Background())
}

func TestStop_WaitsForAnInFlightRunAndPreventsFurtherRuns(t *testing.T) {
	var runs atomic.Int64
	started := make(chan struct{})
	var finished atomic.Bool
	p := StartPeriodic(time.Millisecond, func(ctx context.Context) {
		if runs.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done() // a run that only returns once it is told to stop
		time.Sleep(20 * time.Millisecond)
		finished.Store(true)
	})

	<-started
	p.Stop(context.Background())

	if !finished.Load() {
		t.Fatal("Stop returned before the in-flight run finished")
	}
	after := runs.Load()
	time.Sleep(20 * time.Millisecond)
	if got := runs.Load(); got != after {
		t.Fatalf("expected no runs after Stop, went from %d to %d", after, got)
	}
	p.Stop(context.Background()) // a second Stop must not block or panic
}

func TestStop_GivesUpWhenItsContextEnds(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	p := StartPeriodic(time.Hour, func(context.Context) { <-release }) // ignores cancellation

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	returned := make(chan struct{})
	go func() {
		p.Stop(ctx)
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not honour its context deadline")
	}
}
