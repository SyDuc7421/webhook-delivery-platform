// Package app builds the handlers and components each process runs, so cmd/* and
// integration tests share one wiring. It holds no business logic.
package app

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	goredis "github.com/redis/go-redis/v9"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/cache/redis"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/config"
	apihttp "github.com/SyDuc7421/webhook-delivery-platform/backend/internal/http"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/observability"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/authn"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/secretbox"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
)

// APIDeps are the api process's connected dependencies.
type APIDeps struct {
	Config config.Config
	Pool   *pgxpool.Pool
	Redis  *goredis.Client
	Logger *slog.Logger
	Health *observability.Health
	// Registry receives the ingest metrics; nil means a private registry (tests).
	Registry *prometheus.Registry
}

// NewAPIHandler wires repositories, services and the router for the api process.
func NewAPIHandler(d APIDeps) (http.Handler, error) {
	clock := platform.SystemClock{}
	jwt := authn.NewJWT(d.Config.Auth.JWTSecret, d.Config.Auth.AccessTTL, clock)
	auth := service.NewAuthService(service.AuthDeps{
		Tx:         postgres.NewTransactor(d.Pool),
		Orgs:       postgres.NewOrgRepo(d.Pool),
		Users:      postgres.NewUserRepo(d.Pool),
		Refresh:    redis.NewRefreshStore(d.Redis),
		JWT:        jwt,
		RefreshTTL: d.Config.Auth.RefreshTTL,
		Clock:      clock,
		Logger:     d.Logger,
	})
	apiKeys := service.NewAPIKeyService(postgres.NewAPIKeyRepo(d.Pool), clock)
	box, err := secretbox.New(d.Config.Secrets.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("secret box: %w", err)
	}
	endpoints := service.NewEndpointService(
		postgres.NewEndpointRepo(d.Pool), postgres.NewConsumerRepo(d.Pool), postgres.NewEventTypeRepo(d.Pool), box, clock)
	catalog := service.NewCatalogService(postgres.NewEventTypeRepo(d.Pool), postgres.NewConsumerRepo(d.Pool), clock)
	reg := d.Registry
	if reg == nil {
		reg = prometheus.NewRegistry()
	}
	ingested := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "webhook_events_ingested_total", Help: "Events handled by POST /v1/events, by result.",
	}, []string{"result"})
	failOpen := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "webhook_rate_limit_fail_open_total", Help: "Requests let through because the rate limiter was unavailable.",
	})
	reg.MustRegister(ingested, failOpen)
	for _, result := range []string{"accepted", "duplicate", "rejected", "rate_limited"} {
		ingested.WithLabelValues(result) // expose zero-valued series from the start
	}
	ingest := service.NewIngestService(
		postgres.NewTransactor(d.Pool), postgres.NewEventRepo(d.Pool), postgres.NewOutboxRepo(d.Pool),
		postgres.NewConsumerRepo(d.Pool), postgres.NewEventTypeRepo(d.Pool), d.Config.Ingest.MaxPayloadBytes, clock, ingested)
	limiter := redis.NewRateLimiter(d.Redis, d.Config.Ingest.RateLimitPerMin, time.Minute)

	return apihttp.NewRouter(apihttp.RouterDeps{
		Logger: d.Logger, Health: d.Health, Auth: auth, JWT: jwt, APIKeys: apiKeys, Catalog: catalog, Endpoints: endpoints,
		Ingest: ingest, MaxPayloadBytes: d.Config.Ingest.MaxPayloadBytes, RateLimiter: limiter,
		RateLimitFailOpen: failOpen, RateLimitDenied: ingested.WithLabelValues("rate_limited"),
	}), nil
}
