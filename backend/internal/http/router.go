package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/observability"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/authn"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
)

// RouterDeps are the handlers' collaborators. Later milestones add fields.
type RouterDeps struct {
	Logger *slog.Logger
	Health *observability.Health
	Auth   *service.AuthService
	JWT    *authn.JWT

	APIKeys   *service.APIKeyService
	Catalog   *service.CatalogService
	Endpoints *service.EndpointService

	Ingest            *service.IngestService
	MaxPayloadBytes   int
	RateLimiter       domain.RateLimiter
	RateLimitFailOpen prometheus.Counter
	RateLimitDenied   prometheus.Counter
}

// NewRouter builds the api router.
func NewRouter(d RouterDeps) http.Handler {
	logger, health := d.Logger, d.Health
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestIDHeader)
	r.Use(accessLog(logger))
	r.Use(recoverer(logger))

	r.Get("/healthz", health.Live)
	r.Get("/readyz", health.Ready)

	if d.Auth != nil {
		auth := &authHandler{svc: d.Auth, logger: logger}
		r.Route("/v1", func(r chi.Router) {
			r.Post("/auth/register", auth.register)
			r.Post("/auth/login", auth.login)
			r.Post("/auth/refresh", auth.refresh)
			r.Post("/auth/logout", auth.logout)

			if d.Ingest != nil {
				ingest := &ingestHandler{svc: d.Ingest, maxPayload: d.MaxPayloadBytes, logger: logger}
				r.With(requireAPIKey(d.APIKeys, logger), rateLimit(d.RateLimiter, logger, d.RateLimitFailOpen, d.RateLimitDenied)).
					Post("/events", ingest.handle)
			}

			r.Group(func(r chi.Router) {
				r.Use(requireUser(d.JWT, logger))
				r.Get("/me", auth.me)

				types := &eventTypeHandler{svc: d.Catalog, logger: logger}
				r.Route("/event-types", func(r chi.Router) {
					r.Post("/", types.create)
					r.Get("/", types.list)
					r.Get("/{id}", types.get)
					r.Patch("/{id}", types.update)
					r.Delete("/{id}", types.delete)
				})

				consumers := &consumerHandler{svc: d.Catalog, logger: logger}
				endpoints := &endpointHandler{svc: d.Endpoints, logger: logger}
				r.Route("/consumers", func(r chi.Router) {
					r.Post("/", consumers.create)
					r.Get("/", consumers.list)
					r.Get("/{id}", consumers.get)
					r.Patch("/{id}", consumers.update)
					r.Delete("/{id}", consumers.delete)
					r.Post("/{id}/endpoints", endpoints.create)
					r.Get("/{id}/endpoints", endpoints.listByConsumer)
				})
				r.Route("/endpoints", func(r chi.Router) {
					r.Get("/{id}", endpoints.get)
					r.Patch("/{id}", endpoints.update)
					r.Delete("/{id}", endpoints.delete)
					r.Post("/{id}/rotate-secret", endpoints.rotateSecret)
				})

				keys := &apiKeyHandler{svc: d.APIKeys, logger: logger}
				r.Route("/api-keys", func(r chi.Router) {
					r.Use(requireRole(domain.RoleOwner, logger))
					r.Post("/", keys.create)
					r.Get("/", keys.list)
					r.Delete("/{id}", keys.revoke)
				})
			})
		})
	}

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, logger, domain.ErrNotFound)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusMethodNotAllowed, errorBody{
			Error: errorDetail{Code: "METHOD_NOT_ALLOWED", Message: "method not allowed", Details: []FieldDetail{}},
		})
	})
	return r
}

// requestIDHeader echoes the request id (from X-Request-ID or generated) on every response.
func requestIDHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(middleware.RequestIDHeader, middleware.GetReqID(r.Context()))
		next.ServeHTTP(w, r)
	})
}

// accessLog logs one line per request with the route template, never the raw path's ids.
func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "/healthz" || route == "/readyz" {
				return
			}
			logger.InfoContext(r.Context(), "http request",
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", ww.Status()),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", middleware.GetReqID(r.Context())))
		})
	}
}

// recoverer turns a handler panic into a 500 envelope and an error log.
func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer recoverPanic(w, r, logger)
			next.ServeHTTP(w, r)
		})
	}
}

// recoverPanic must be deferred directly so recover() sees the handler's panic.
func recoverPanic(w http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	rec := recover()
	if rec == nil {
		return
	}
	if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
		panic(rec) //nolint:forbidigo // net/http contract: re-panic ErrAbortHandler to abort the response
	}
	logger.ErrorContext(r.Context(), "panic in handler",
		slog.Any("panic", rec),
		slog.String("request_id", middleware.GetReqID(r.Context())))
	writeJSON(w, http.StatusInternalServerError, errorBody{
		Error:     errorDetail{Code: internalError.code, Message: internalError.message, Details: []FieldDetail{}},
		RequestID: middleware.GetReqID(r.Context()),
	})
}
