package boot

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
)

func setEnv(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_USER", "u")
	t.Setenv("POSTGRES_PASSWORD", "p")
	t.Setenv("POSTGRES_DB", "d")
	t.Setenv("METRICS_ADDR", "127.0.0.1:0")
	t.Setenv("SHUTDOWN_TIMEOUT", "2s")
	t.Setenv("LOG_LEVEL", "error")
}

func TestRunInvalidConfig(t *testing.T) {
	t.Setenv("POSTGRES_USER", "")
	t.Setenv("POSTGRES_PASSWORD", "")
	t.Setenv("POSTGRES_DB", "")
	if code := run("api", func(context.Context, *Process) error { return nil }); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestRunSetupFailure(t *testing.T) {
	setEnv(t)
	closed := false
	code := run("relay", func(_ context.Context, p *Process) error {
		p.OnClose(func() { closed = true })
		return errors.New("postgres unreachable")
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !closed {
		t.Error("closers must run even when setup fails")
	}
}

func TestRunStopsCleanlyOnSIGTERM(t *testing.T) {
	setEnv(t)
	started := make(chan struct{})
	var p *Process
	go func() {
		<-started
		// Signal handling is registered before setup runs, so this cannot kill the test binary.
		time.Sleep(100 * time.Millisecond) // let platform.Run start the components
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()
	code := run("dispatcher", func(_ context.Context, proc *Process) error {
		p = proc
		proc.Redis() // clients are lazy: no network needed to construct them
		if _, err := proc.Kafka(); err != nil {
			return err
		}
		proc.Add(platform.NewIdle("dispatcher", "M2", proc.Logger))
		close(started)
		return nil
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := len(p.checks); got != 2 {
		t.Errorf("checks = %d, want redis + kafka", got)
	}
	if first := p.Health(); first != p.Health() {
		t.Error("Health must return the same instance the ops server uses")
	}
	if p.Config.App.Version != Version {
		t.Errorf("version = %q, want build version %q when APP_VERSION is unset", p.Config.App.Version, Version)
	}
}
