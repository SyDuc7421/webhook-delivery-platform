package platform

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollerRunsImmediatelyAndRepeatedly(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	p := NewPoller("relay", 10*time.Millisecond, func(context.Context) error {
		calls.Add(1)
		return nil
	}, discard)

	done := make(chan error, 1)
	go func() { done <- p.Start(context.Background()) }()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 3 {
		t.Fatalf("calls = %d, want at least 3", calls.Load())
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("start returned %v after stop", err)
	}
	after := calls.Load()
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != after {
		t.Error("fn must not run after Stop")
	}
	if p.Name() != "relay" {
		t.Errorf("name = %q", p.Name())
	}
}

func TestPollerSurvivesErrors(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	p := NewPoller("relay", 5*time.Millisecond, func(context.Context) error {
		if calls.Add(1) <= 2 {
			return errors.New("kafka down")
		}
		return nil
	}, discard)
	go func() { _ = p.Start(context.Background()) }()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 4 {
		t.Errorf("calls = %d; an error must not stop the loop (an outage must not crash the relay)", calls.Load())
	}
	_ = p.Stop(context.Background())
}

func TestPollerStopWaitsForInFlightWork(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	var finished atomic.Bool
	p := NewPoller("relay", time.Hour, func(ctx context.Context) error {
		close(started)
		<-ctx.Done() // work that notices the shutdown, then cleans up
		time.Sleep(50 * time.Millisecond)
		finished.Store(true)
		return ctx.Err()
	}, discard)
	go func() { _ = p.Start(context.Background()) }()
	<-started

	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !finished.Load() {
		t.Error("Stop must wait for the running fn to finish")
	}
}

func TestPollerStopIsBoundedByItsContext(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	p := NewPoller("relay", time.Hour, func(context.Context) error {
		close(started)
		<-release // stuck, ignores cancellation
		return nil
	}, discard)
	go func() { _ = p.Start(context.Background()) }()
	<-started
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := p.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("stop err = %v, want deadline exceeded", err)
	}
}
