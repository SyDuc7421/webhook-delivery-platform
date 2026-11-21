# Go conventions

Anything not listed here: follow Effective Go + Google Go Style Guide. `golangci-lint` is the
tie-breaker.

## Packages & files

- Package name = short lowercase noun, no underscores, no plurals. Banned: `utils`, `common`,
  `helpers`, `base`, `misc`, `models`.
- One concept per file. `endpoint_handler.go`, `endpoint_service.go`, `endpoint_repository.go`.
- Test file next to source: `endpoint_service_test.go`. Integration tests: `_integration_test.go`
  with `//go:build integration`.

## Functions

- `ctx context.Context` is always the first parameter for anything doing I/O.
- Return `(T, error)`. Never return a bare `interface{}`/`any`.
- Accept interfaces, return concrete structs.
- >5 parameters → pass an options struct.

## Errors

```go
// domain/errors.go
var (
    ErrNotFound      = errors.New("not found")
    ErrAlreadyExists = errors.New("already exists")
    ErrInvalidInput  = errors.New("invalid input")
    ErrForbidden     = errors.New("forbidden")
    ErrConflict      = errors.New("conflict")
    ErrUnauthorized  = errors.New("unauthorized")     // missing or revoked API key / token
    ErrRateLimited   = errors.New("rate limited")
    ErrPayloadTooBig = errors.New("payload too large")
    ErrBlockedHost   = errors.New("blocked host")     // SSRF guard
)
```

- Wrap with context: `fmt.Errorf("get endpoint %s: %w", id, err)`. Lowercase, no punctuation, no
  "failed to" (the caller adds that).
- Compare with `errors.Is` / `errors.As`. Never `err.Error() == "..."`.
- Handle or return — never both log and return.
- `panic` only in `main()` bootstrap. Middleware recovers everything else.

## Structs & data

- Zero value should be usable, or the constructor is mandatory (`NewX`).
- Time: always `time.Time` in UTC; store `timestamptz`; format RFC3339 at the boundary.
- IDs: UUIDv7 — sortable and safe to expose. `message_id` doubles as `webhook-id`.
- Secrets: API keys and refresh tokens stored as hashes; signing secrets encrypted or at least
  never returned after creation/rotation. Use `crypto/subtle` for comparisons.
- Payloads: keep as `json.RawMessage` end to end; never unmarshal an org's payload into a map.

## Concurrency

- Every goroutine has a clear owner and a stop condition (`ctx` or `errgroup`).
- No goroutine started per request or per record without a bounded worker pool.
- Kafka consumers process a partition sequentially (ordering, FR-D5); parallelism comes from
  partitions and replicas, not from goroutines per record.
- Guard shared state with a mutex or a channel — race detector runs in CI.

## Logging

```go
slog.InfoContext(ctx, "delivery attempt finished",
    slog.String("message_id", msgID),
    slog.String("endpoint_id", endpointID),
    slog.Int("status", resp.StatusCode),
    slog.Duration("latency", latency))
```

Levels: `Debug` local only · `Info` state changes · `Warn` recoverable/degraded ·
`Error` needs a human. Never log PII, API keys, signing secrets, tokens, or payload/response
bodies (NFR-S5). The `*Context` variants are mandatory so `trace_id` is attached.

## Comments

Exported identifiers get a doc comment starting with the name. Inline comments explain **why**,
never what. Delete commented-out code.
