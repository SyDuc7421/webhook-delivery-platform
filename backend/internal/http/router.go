package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/observability"
)

// NewRouter builds the api router. Feature routes (/v1/...) are mounted from M1.
func NewRouter(logger *slog.Logger, health *observability.Health) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestIDHeader)
	r.Use(accessLog(logger))
	r.Use(recoverer(logger))

	r.Get("/healthz", health.Live)
	r.Get("/readyz", health.Ready)

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
