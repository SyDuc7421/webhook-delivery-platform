// Package app builds the handlers and components each process runs, so cmd/* and
// integration tests share one wiring. It holds no business logic.
package app

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/cache/redis"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/config"
	apihttp "github.com/SyDuc7421/webhook-delivery-platform/backend/internal/http"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/observability"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/authn"
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
	catalog := service.NewCatalogService(postgres.NewEventTypeRepo(d.Pool), postgres.NewConsumerRepo(d.Pool), clock)
	return apihttp.NewRouter(apihttp.RouterDeps{
		Logger: d.Logger, Health: d.Health, Auth: auth, JWT: jwt, APIKeys: apiKeys, Catalog: catalog,
	}), nil
}
