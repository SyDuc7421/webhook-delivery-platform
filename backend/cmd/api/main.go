// Command api serves the dashboard API, the ingest API and the test receiver.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/cmd/internal/boot"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/app"
	apihttp "github.com/SyDuc7421/webhook-delivery-platform/backend/internal/http"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	boot.Main("api", func(ctx context.Context, p *boot.Process) error {
		pool, err := p.Postgres(ctx)
		if err != nil {
			return err
		}
		handler, err := app.NewAPIHandler(app.APIDeps{
			Config: p.Config, Pool: pool, Redis: p.Redis(), Logger: p.Logger, Health: p.Health(), Registry: p.Registry,
		})
		if err != nil {
			return err
		}
		p.Add(apihttp.NewServer(p.Config.HTTP.Port, p.Config.HTTP.RequestTimeout, handler, p.Logger))
		return nil
	})
}

// healthcheck lets the distroless image run its own compose healthcheck (no shell, no wget).
func healthcheck() int {
	port := 8080 // tiny probe: reads one env var instead of loading and validating full config
	if v, err := strconv.Atoi(os.Getenv("API_PORT")); err == nil && v > 0 && v < 65536 {
		port = v
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/healthz", nil) //nolint:gosec // loopback only, port is an int
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // loopback only, port is an int
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
