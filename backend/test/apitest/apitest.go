//go:build integration

// Package apitest serves the real api handler over real dependencies for integration tests.
package apitest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/app"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/cache/redis"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/observability"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/test/testenv"
)

// Response is a fully read response; the body is already closed.
type Response struct {
	StatusCode int
	Header     http.Header
}

// Client talks to an httptest server running app.NewAPIHandler.
type Client struct {
	t   *testing.T
	srv *httptest.Server
}

// New starts the api handler against env's Postgres and Redis.
func New(t *testing.T, env *testenv.Env) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := postgres.NewPool(ctx, env.Config.Postgres)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	rc := redis.NewClient(env.Config.Redis)
	t.Cleanup(func() { _ = rc.Close() })

	h, err := app.NewAPIHandler(app.APIDeps{
		Config: env.Config, Pool: pool, Redis: rc,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Health: observability.NewHealth(),
	})
	if err != nil {
		t.Fatalf("api handler: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{t: t, srv: srv}
}

// Do sends a JSON request; token, if set, is sent as a Bearer token. body may be nil.
func (c *Client) Do(method, path, token string, body any) (Response, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal: %v", err)
		}
		rd = bytes.NewReader(b)
	}
	return c.DoRaw(method, path, token, rd, nil)
}

// DoRaw sends a raw body with extra headers.
func (c *Client) DoRaw(method, path, token string, body io.Reader, headers map[string]string) (Response, []byte) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, c.srv.URL+path, body)
	if err != nil {
		c.t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.srv.Client().Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read body: %v", err)
	}
	return Response{StatusCode: resp.StatusCode, Header: resp.Header}, b
}
