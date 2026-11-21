package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewRegistry returns a Prometheus registry with Go runtime and process collectors.
// Each process registers its own metrics on it; there is no global registry.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return reg
}

// OpsServer serves /metrics, /healthz and /readyz on METRICS_ADDR for every process.
type OpsServer struct {
	srv    *http.Server
	logger *slog.Logger
}

// NewOpsServer builds the ops server. It does not listen until Start.
func NewOpsServer(addr string, reg *prometheus.Registry, health *Health, logger *slog.Logger) *OpsServer {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Ready)
	return &OpsServer{
		srv: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
		},
		logger: logger,
	}
}

// Name implements platform.Component.
func (s *OpsServer) Name() string { return "ops-server" }

// Start listens and serves until Stop. It returns once the listener fails or closes.
func (s *OpsServer) Start(ctx context.Context) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.srv.Addr, err)
	}
	s.logger.InfoContext(ctx, "ops server listening", slog.String("addr", ln.Addr().String()))
	if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve ops: %w", err)
	}
	return nil
}

// Stop shuts the server down gracefully.
func (s *OpsServer) Stop(ctx context.Context) error {
	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown ops server: %w", err)
	}
	return nil
}
