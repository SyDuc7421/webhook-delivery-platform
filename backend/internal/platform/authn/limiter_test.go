package authn

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

func TestLimiterBoundsConcurrency(t *testing.T) {
	t.Parallel()
	l := NewLimiter(3)
	var running, peak atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := l.Do(context.Background(), func() error {
				n := running.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				running.Add(-1)
				return nil
			})
			if err != nil {
				t.Errorf("do: %v", err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() > 3 {
		t.Errorf("peak concurrency = %d, want at most 3", peak.Load())
	}
}

func TestLimiterGivesUpWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	l := NewLimiter(1)
	started, release := make(chan struct{}), make(chan struct{})
	go func() { _ = l.Do(context.Background(), func() error { close(started); <-release; return nil }) }()
	<-started
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	ran := false
	err := l.Do(ctx, func() error { ran = true; return nil })
	if !errors.Is(err, domain.ErrUnavailable) || ran {
		t.Errorf("err = %v, ran = %v; a full limiter must answer ErrUnavailable once the context ends", err, ran)
	}
}

func TestLimiterReturnsTheFunctionsError(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	if err := NewLimiter(1).Do(context.Background(), func() error { return boom }); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}
