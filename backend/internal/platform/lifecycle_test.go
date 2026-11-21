package platform

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

var discard = slog.New(slog.NewJSONHandler(io.Discard, nil))

// fake blocks in Start until Stop is called or failStart is set.
type fake struct {
	name      string
	failStart error
	failStop  error
	blockStop bool

	mu      sync.Mutex
	stopped bool
	order   *[]string
	done    chan struct{}
	once    sync.Once
	started chan struct{}
}

func newFake(name string, order *[]string) *fake {
	return &fake{name: name, order: order, done: make(chan struct{}), started: make(chan struct{})}
}

func (f *fake) Name() string { return f.name }

func (f *fake) Start(ctx context.Context) error {
	close(f.started)
	if f.failStart != nil {
		return f.failStart
	}
	select {
	case <-f.done:
	case <-ctx.Done():
	}
	return nil
}

func (f *fake) Stop(ctx context.Context) error {
	f.mu.Lock()
	f.stopped = true
	if f.order != nil {
		*f.order = append(*f.order, f.name)
	}
	f.mu.Unlock()
	f.once.Do(func() { close(f.done) })
	if f.blockStop {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.failStop
}

func (f *fake) wasStopped() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopped
}

func TestRunStopsAllInReverseOrderOnCancel(t *testing.T) {
	t.Parallel()
	var order []string
	a, b := newFake("a", &order), newFake("b", &order)

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- Run(ctx, discard, time.Second, a, b) }()
	<-a.started
	<-b.started
	cancel()

	if err := <-errc; err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if len(order) != 2 || order[0] != "b" || order[1] != "a" {
		t.Errorf("stop order = %v, want [b a]", order)
	}
}

func TestRunFailingComponentStopsTheRest(t *testing.T) {
	t.Parallel()
	boom := errors.New("bind: address in use")
	bad, good := newFake("bad", nil), newFake("good", nil)
	bad.failStart = boom

	err := Run(context.Background(), discard, time.Second, bad, good)
	if !errors.Is(err, boom) {
		t.Fatalf("Run = %v, want %v", err, boom)
	}
	if !good.wasStopped() {
		t.Error("healthy component was not stopped")
	}
}

func TestRunReportsStopErrors(t *testing.T) {
	t.Parallel()
	stopErr := errors.New("flush failed")
	c := newFake("c", nil)
	c.failStop = stopErr

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- Run(ctx, discard, time.Second, c) }()
	<-c.started
	cancel()

	if err := <-errc; !errors.Is(err, stopErr) {
		t.Fatalf("Run = %v, want %v", err, stopErr)
	}
}

func TestRunShutdownIsBounded(t *testing.T) {
	t.Parallel()
	c := newFake("stuck", nil)
	c.blockStop = true

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- Run(ctx, discard, 20*time.Millisecond, c) }()
	<-c.started
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run = %v, want deadline exceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the shutdown timeout")
	}
}

func TestIdle(t *testing.T) {
	t.Parallel()
	i := NewIdle("dispatcher", "M2", discard)
	if i.Name() != "dispatcher" {
		t.Errorf("name = %q", i.Name())
	}
	done := make(chan error, 1)
	go func() { done <- i.Start(context.Background()) }()
	if err := i.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := i.Stop(context.Background()); err != nil {
		t.Fatalf("second stop must be a no-op: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("start: %v", err)
	}
}
