# ADR-0009: chi router and log/slog for the Go backend

- Status: accepted · Date: 2026-11-21

## Context

The design (2026-11-18) said to reuse `shorten-link` conventions, which use Gin and zap. The
backend template this repo starts from mandates `log/slog`. The project values a small
dependency surface and idiomatic `net/http` code that interviewers can read without framework
knowledge. Request validation needs struct-tag rules once Gin's `binding` is gone.

## Options

| Option | For | Against |
| --- | --- | --- |
| Gin + zap (as `shorten-link`) | Familiar; copy code across | Two non-stdlib APIs at the core; `*gin.Context` leaks into handlers; same story as the other project |
| **`net/http` + chi + `log/slog`** | Handlers are plain `http.HandlerFunc`; chi is a thin router over the stdlib mux model; slog is stdlib and OTel-bridgeable | Some helpers (`decodeJSON`, `respondJSON`) written by hand; a separate validator library |
| stdlib `http.ServeMux` (Go 1.22 patterns) + slog | Zero router dependency | No route groups / per-group middleware; more boilerplate |

## Decision

`net/http` + `github.com/go-chi/chi/v5` for routing and middleware, `log/slog` (JSON handler)
for logging, `github.com/go-playground/validator/v10` for DTO shape validation.

## Consequences

- `ARCHITECT.md` §7 and the design overview now say slog instead of zap.
- Handlers take `(w http.ResponseWriter, r *http.Request)`; helpers live in `internal/http`.
- The golangci `forbidigo` rule already bans `fmt.Print*` in favour of slog.

## Revisit when

chi becomes unmaintained, or stdlib `ServeMux` gains route groups/middleware.
