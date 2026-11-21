package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Server is the api process's public HTTP server as a platform.Component.
type Server struct {
	srv    *http.Server
	logger *slog.Logger
}

// NewServer wraps handler with timeouts. requestTimeout bounds handler run time.
func NewServer(port int, requestTimeout time.Duration, handler http.Handler, logger *slog.Logger) *Server {
	return &Server{
		srv: &http.Server{
			Addr:              ":" + strconv.Itoa(port),
			Handler:           http.TimeoutHandler(handler, requestTimeout, `{"error":{"code":"TIMEOUT","message":"request timed out","details":[]}}`),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      requestTimeout + 5*time.Second,
			IdleTimeout:       60 * time.Second,
		},
		logger: logger,
	}
}

// Name implements platform.Component.
func (s *Server) Name() string { return "http-server" }

// Start implements platform.Component.
func (s *Server) Start(ctx context.Context) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.srv.Addr, err)
	}
	s.logger.InfoContext(ctx, "http server listening", slog.String("addr", ln.Addr().String()))
	if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve http: %w", err)
	}
	return nil
}

// Stop implements platform.Component: stop accepting, drain in-flight requests.
func (s *Server) Stop(ctx context.Context) error {
	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	return nil
}
