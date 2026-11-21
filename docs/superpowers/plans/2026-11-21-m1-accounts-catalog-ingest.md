# M1 — Accounts, Catalog, Ingest Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.


**Goal:** An org can register, log in, create API keys, event types, consumers and endpoints, and
send events to `POST /v1/events`; every accepted event reaches the Kafka `events` topic through the
outbox relay, even if Kafka was down when it was accepted.

**Architecture:** Three sequential PRs on top of M0. Layering per `AGENTS.md` §4:
`http → service → domain ← repository/postgres (sqlc) | cache/redis | messaging/kafka`. Services
own transactions through a `domain.Transactor`; repositories read the active tx from `ctx`.
`internal/app` builds the api handler and the relay so `cmd/*` and integration tests share wiring.

**Tech Stack:** Go 1.27, chi, pgx/v5 + sqlc, go-redis/v9, franz-go, testcontainers. New deps:
`golang.org/x/crypto` (argon2id), `github.com/golang-jwt/jwt/v5`, `github.com/google/uuid` (v7),
`github.com/go-playground/validator/v10` (ADR-0009).

**Spec:** `docs/SPEC.md` (FR-A, FR-K, FR-T, FR-E1–E4, FR-I, NFR-D1), `docs/ARCHITECT.md` §2, §4, §5,
`docs/API.md`, `docs/DATABASE.md`, `docs/CACHING.md`, `docs/MESSAGING.md`.

**Decisions taken (2026-11-21):** 3 PRs (M1a/M1b/M1c) · sqlc · JWT HS256 access + opaque single-use
refresh in Redis · endpoint signing secrets AES-256-GCM with key from env · OpenAPI hand-written in
`docs/openapi.yaml`.

## Global Constraints

- Error body is always `{"error":{"code","message","details":[]},"request_id"}` via `respondError` (`backend/internal/http/respond.go`).
- Every dashboard query is scoped by `org_id` from the auth context; another org's resource → `404 RESOURCE_NOT_FOUND`.
- Payload is JSON, max 256 KB (`INGEST_MAX_PAYLOAD_BYTES=262144`); over → `413 PAYLOAD_TOO_LARGE`.
- Over the rate limit → `429` with `Retry-After`; Redis unavailable → rate limit fails open (logged) (NFR-D1).
- API key: full key shown exactly once; only SHA-256 hash + visible prefix stored; revoked key rejected immediately.
- Event type name unique per org; consumer `uid` unique per org → `409 ALREADY_EXISTS`.
- Lists: cursor pagination only, `limit` default 20, max 100.
- Access token TTL `JWT_ACCESS_TTL=15m`; refresh `JWT_REFRESH_TTL=168h`, single use (`GETDEL rt:{hash}`).
- Passwords: argon2id. Never log passwords, tokens, API keys, secrets or payloads (NFR-S5).
- No Kafka produce in the ingest request; only the relay produces (ADR-0002).
- Migrations append-only, each with a real `down`; IDs UUIDv7 generated in app.
- `make check` and `make test-int` pass at the end of every task.

## Review Focus

1. **Email casing/whitespace** — `" Alice@Example.com "` registers; `alice@example.com` logs in as the same user; registering `ALICE@example.com` again → 409. Test in Task A6.
2. **Refresh token replay** — a refresh token used once is dead; two concurrent refreshes with the same token → exactly one 200, one 401. Test in Task A4 + A7.
3. **Cross-org access by id** — org B's `GET/PATCH/DELETE` on org A's API key, event type, consumer, endpoint → 404, and nothing changes. Test in Task B5.
4. **Concurrent duplicate `Idempotency-Key`** — 10 parallel ingests with the same key → all 202 with the same id, exactly 1 `events` row and 1 `outbox` row. Test in Task C1.
5. **Ingest boundaries** — payload of exactly 262144 bytes → 202, 262145 → 413; trailing garbage / unknown field → 400; Redis stopped → still 202. Test in Task C3 + C2.

---

## File map

```
backend/
  sqlc.yaml                                  modify: jsonb → json.RawMessage override
  migrations/000002_create_orgs_users.*      A
  migrations/000003_create_api_keys.*        B
  migrations/000004_create_catalog.*         B   event_types, consumers, endpoints
  migrations/000005_create_events_outbox.*   C
  internal/config/config.go                  modify: Auth, Secrets, Ingest, Outbox sections
  internal/domain/                           org.go apikey.go catalog.go event.go repos.go errors.go(modify) validation.go
  internal/platform/ids.go clock.go pagination.go
  internal/platform/authn/                   password.go jwt.go tokens.go
  internal/platform/secretbox/secretbox.go
  internal/repository/postgres/
    queries/*.sql                            sqlc input (one file per table group)
    db/                                      sqlc output (generated, committed)
    tx.go users.go apikeys.go catalog.go events.go outbox.go
  internal/cache/redis/refresh.go ratelimit.go
  internal/messaging/kafka/producer.go
  internal/service/                          auth.go apikeys.go catalog.go endpoints.go ingest.go relay.go
  internal/http/                             decode.go auth_middleware.go ratelimit_middleware.go
                                             auth_handler.go apikey_handler.go eventtype_handler.go
                                             consumer_handler.go endpoint_handler.go ingest_handler.go router.go(modify)
  internal/platform/poller.go                generic interval Component
  internal/app/api.go relay.go               shared wiring for cmd/* and tests
  cmd/api/main.go cmd/relay/main.go          modify
  test/apitest/apitest.go                    httptest server over real deps
  test/m1a_auth_test.go m1b_catalog_test.go m1c_ingest_test.go
docs/openapi.yaml (new), docs/API.md, docs/SPEC.md (status), .env.example
```

---

# PR M1a — schema + auth (FR-A1–A3)

Branch: `feat/m1a-auth`.

### Task A1: Config sections, dependencies, sqlc wiring

**Files:**
- Modify: `backend/internal/config/config.go`, `backend/internal/config/config_test.go`, `.env.example`, `backend/sqlc.yaml`, `backend/go.mod`

**Interfaces:**
- Produces:
  ```go
  type Auth struct { JWTSecret []byte; AccessTTL, RefreshTTL time.Duration }
  type Secrets struct { EncryptionKey []byte } // 32 bytes, from base64 SECRETS_ENCRYPTION_KEY
  type Ingest struct { MaxPayloadBytes int; RateLimitPerMin int }
  type Outbox struct { PollInterval time.Duration; BatchSize int }
  // Config gains fields: Auth, Secrets, Ingest, Outbox
  ```

- [ ] **Step 1: Failing tests** in `config_test.go` table: `JWT_SECRET` shorter than 32 bytes → error mentions `JWT_SECRET must be at least 32 bytes`; `SECRETS_ENCRYPTION_KEY` not base64 or not 32 bytes after decode → `SECRETS_ENCRYPTION_KEY must be 32 bytes base64`; defaults `AccessTTL=15m`, `RefreshTTL=168h`, `MaxPayloadBytes=262144`, `RateLimitPerMin=600`, `PollInterval=200ms`, `BatchSize=100`; `BatchSize` 0 → error. Update `minimal()` to include valid `JWT_SECRET` and `SECRETS_ENCRYPTION_KEY`.
- [ ] **Step 2:** `cd backend && go test ./internal/config/` → FAIL (unknown fields).
- [ ] **Step 3:** Implement the four sections in `load()` using the existing `reader` helpers; both secrets are `required`. Update `.env.example`: `JWT_SECRET` = 40-char dummy, add `SECRETS_ENCRYPTION_KEY=` a dummy 32-byte base64 value with comment `# openssl rand -base64 32`. `go get golang.org/x/crypto github.com/golang-jwt/jwt/v5 github.com/google/uuid github.com/go-playground/validator/v10`. Add to `sqlc.yaml` overrides: `db_type: jsonb → encoding/json.RawMessage` and `db_type: jsonb, nullable: true → encoding/json.RawMessage`.
- [ ] **Step 4:** `go test ./internal/config/` → PASS; `make check` → PASS (`cmd/internal/boot/boot_test.go` `setEnv` gets the two new vars).
- [ ] **Step 5:** Commit `feat(config): add auth, secrets, ingest and outbox settings`.

### Task A2: Platform primitives — ids, clock, passwords, JWT, opaque tokens

**Files:**
- Create: `backend/internal/platform/ids.go`, `clock.go`, `backend/internal/platform/authn/{password,jwt,tokens}.go` + `_test.go`

**Interfaces:**
- Produces:
  ```go
  // platform
  func NewID() uuid.UUID                       // uuid.Must(uuid.NewV7())
  type Clock interface{ Now() time.Time }      // SystemClock{} returns time.Now().UTC()
  // authn
  func HashPassword(pw string) (string, error)               // PHC string $argon2id$v=19$m=65536,t=3,p=2$salt$hash
  func VerifyPassword(encoded, pw string) (bool, error)      // constant-time compare
  type Claims struct{ UserID, OrgID uuid.UUID; Role string; ExpiresAt time.Time }
  type JWT struct{ /* secret, ttl, clock */ }
  func NewJWT(secret []byte, ttl time.Duration, clock platform.Clock) *JWT
  func (j *JWT) Issue(c Claims) (token string, expiresAt time.Time, err error) // iss "webhook-platform", aud "dashboard", HS256
  func (j *JWT) Parse(token string) (Claims, error)          // any failure → wraps domain.ErrUnauthorized
  func NewOpaqueToken() (token string, hash string, err error) // 32 random bytes base64url; hash = hex(sha256(token))
  func HashOpaqueToken(token string) string
  ```

- [ ] **Step 1: Failing tests.**
  - `TestHashVerifyPassword`: verify correct → true; wrong → false; two hashes of same password differ (salt); malformed encoded → error.
  - `TestJWT`: issued token parses to same `UserID/OrgID/Role`; token from other secret → `errors.Is(err, domain.ErrUnauthorized)`; fake clock advanced past ttl → ErrUnauthorized; `alg: none` token → ErrUnauthorized; wrong `aud` → ErrUnauthorized.
  - `TestOpaqueToken`: two tokens differ; `HashOpaqueToken(tok) == hash`; token decodes to 32 bytes.
- [ ] **Step 2:** `go test ./internal/platform/...` → FAIL.
- [ ] **Step 3:** Implement. argon2id params m=64 MiB, t=3, p=2, salt 16 B, key 32 B. JWT: `jwt.NewWithClaims(jwt.SigningMethodHS256, …)`, parse with `jwt.WithValidMethods([]string{"HS256"})`, `jwt.WithAudience("dashboard")`, `jwt.WithIssuer("webhook-platform")`, `jwt.WithTimeFunc(clock.Now)`.
- [ ] **Step 4:** tests PASS, `make check` PASS.
- [ ] **Step 5:** Commit `feat(platform): add ids, clock, password hashing and tokens`.

### Task A3: Orgs/users schema, sqlc queries, Transactor, user repository

**Files:**
- Create: `backend/migrations/000002_create_orgs_users.{up,down}.sql`, `backend/internal/repository/postgres/queries/users.sql`, `backend/internal/repository/postgres/{tx.go,users.go}`, generated `db/`
- Create: `backend/internal/domain/{org.go,repos.go}`
- Test: `backend/test/m1a_auth_test.go` (repository part)

**Interfaces:**
- Produces:
  ```go
  // domain
  type Role string; const (RoleOwner Role = "owner"; RoleMember Role = "member")
  type Org struct{ ID uuid.UUID; Name string; CreatedAt time.Time }
  type User struct{ ID, OrgID uuid.UUID; Email, PasswordHash string; Role Role; CreatedAt time.Time }
  type Transactor interface{ WithinTx(ctx context.Context, fn func(ctx context.Context) error) error }
  type OrgRepository interface{ Create(ctx context.Context, o Org) error; Get(ctx context.Context, id uuid.UUID) (Org, error) }
  type UserRepository interface{
    Create(ctx context.Context, u User) error            // duplicate email → ErrAlreadyExists
    GetByEmail(ctx context.Context, email string) (User, error) // ErrNotFound
    Get(ctx context.Context, id uuid.UUID) (User, error)
  }
  // postgres
  func NewTransactor(pool *pgxpool.Pool) *Transactor
  func NewOrgRepo(pool *pgxpool.Pool) *OrgRepo; func NewUserRepo(pool *pgxpool.Pool) *UserRepo
  func queries(ctx context.Context, pool *pgxpool.Pool) *db.Queries // tx from ctx if present
  func mapErr(err error) error  // pgx.ErrNoRows → ErrNotFound; 23505 → ErrAlreadyExists; 23503 → ErrNotFound
  ```
- Schema: `orgs(id uuid pk, name text not null check(length(name) between 1 and 100), created_at, updated_at)`; `users(id uuid pk, org_id uuid not null references orgs on delete cascade, email text not null, password_hash text not null, role text not null check(role in ('owner','member')), created_at, updated_at)`, `create unique index users_email_key on users (email)` (service stores lowercased), index on `org_id`.

- [ ] **Step 1: Failing integration test** `TestUserRepository` (`//go:build integration`, uses `testenv.Start`): create org+user inside `WithinTx` → `GetByEmail` finds it; second user same email → `errors.Is(err, domain.ErrAlreadyExists)`; `WithinTx` whose fn returns error → org not persisted (`Get` → ErrNotFound); `GetByEmail("missing@x.io")` → ErrNotFound.
- [ ] **Step 2:** `make test-int` → FAIL.
- [ ] **Step 3:** Write migration, `queries/users.sql` (`CreateOrg`, `GetOrg`, `CreateUser`, `GetUserByEmail`, `GetUser` — explicit columns), run `make gen`, implement repos + Transactor (`pool.BeginTx` read committed, ctx key holds `pgx.Tx`, commit on nil, rollback otherwise).
- [ ] **Step 4:** `make test-int` PASS; `make check` PASS; `make migrate-up && make migrate-down && make migrate-up` locally PASS.
- [ ] **Step 5:** Commit `feat(repo): add orgs and users schema with sqlc repositories`.

### Task A4: Redis refresh-token store

**Files:**
- Create: `backend/internal/cache/redis/refresh.go`; Modify: `backend/internal/domain/repos.go`
- Test: `backend/test/m1a_auth_test.go` (refresh part)

**Interfaces:**
- Produces:
  ```go
  // domain
  type RefreshStore interface{
    Save(ctx context.Context, tokenHash string, userID uuid.UUID, ttl time.Duration) error
    Consume(ctx context.Context, tokenHash string) (uuid.UUID, error) // GETDEL; missing → ErrUnauthorized; redis error → ErrUnavailable
    Delete(ctx context.Context, tokenHash string) error
  }
  // redis
  func NewRefreshStore(c *goredis.Client) *RefreshStore   // keys via RefreshTokenKey(hash)
  ```

- [ ] **Step 1: Failing integration test** `TestRefreshStore`: Save then Consume → same userID; second Consume → ErrUnauthorized; 20 goroutines Consume the same hash → exactly 1 success; TTL set (`TTL(key) > 0`); Redis stopped → Consume error `errors.Is(err, domain.ErrUnavailable)`.
- [ ] **Step 2:** FAIL. **Step 3:** implement with `SET … EX`, `GETDEL`, `DEL`. **Step 4:** PASS + `make check`.
- [ ] **Step 5:** Commit `feat(cache): add single-use refresh token store`.

### Task A5: Request decoding, validation errors with details

**Files:**
- Create: `backend/internal/domain/validation.go`, `backend/internal/http/decode.go`; Modify: `respond.go`, `errors.go`, `http_test.go`

**Interfaces:**
- Produces:
  ```go
  // domain
  type FieldError struct{ Field, Issue string }
  type ValidationError struct{ Fields []FieldError }  // Error(); Is(target) true for ErrInvalidInput
  var ErrUnprocessable = errors.New("unprocessable")  // mapped 422 "UNPROCESSABLE_ENTITY"
  // http
  func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error
  // MaxBytesReader; DisallowUnknownFields; rejects trailing data; body > max → domain.ErrPayloadTooBig;
  // malformed → ValidationError{Fields:[{"body","malformed JSON"}]}; then validator.Struct(dst) → ValidationError with json field names
  ```
  `respondError` copies `ValidationError.Fields` into `error.details`.

- [ ] **Step 1: Failing tests** `TestDecodeJSON` table: valid body → nil; unknown field → 400 detail field `body`; trailing `{}{}` → 400; body over max → `ErrPayloadTooBig`; struct `{Email string \`json:"email" validate:"required,email"\`}` with `"x"` → details `[{field:"email", issue:"must be a valid email"}]`. `TestRespondErrorIncludesDetails`.
- [ ] **Step 2:** FAIL. **Step 3:** implement; one package-level `*validator.Validate` with `RegisterTagNameFunc` returning json tag names; issue text per tag (`required`→"is required", `email`→"must be a valid email", `min`/`max`→"must be at least/at most N characters", default → "is invalid").
- [ ] **Step 4:** PASS. **Step 5:** Commit `feat(http): add json decoding and validation error details`.

### Task A6: Auth service

**Files:**
- Create: `backend/internal/service/auth.go`, `auth_test.go` (fakes in `fakes_test.go`)

**Interfaces:**
- Consumes: A2 `authn.*`, `platform.NewID/Clock`; A3 repos + `Transactor`; A4 `RefreshStore`.
- Produces:
  ```go
  type AuthService struct{ /* deps */ }
  type AuthDeps struct{ Tx domain.Transactor; Orgs domain.OrgRepository; Users domain.UserRepository;
    Refresh domain.RefreshStore; JWT *authn.JWT; RefreshTTL time.Duration; Clock platform.Clock }
  func NewAuthService(d AuthDeps) *AuthService
  type RegisterInput struct{ Email, Password, OrgName string }
  type Session struct{ User domain.User; Org domain.Org; AccessToken string; AccessExpiresAt time.Time; RefreshToken string }
  func (s *AuthService) Register(ctx context.Context, in RegisterInput) (Session, error) // FR-A1
  func (s *AuthService) Login(ctx context.Context, email, password string) (Session, error) // FR-A2
  func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (Session, error)  // rotates
  func (s *AuthService) Logout(ctx context.Context, refreshToken string) error
  func (s *AuthService) Me(ctx context.Context, userID uuid.UUID) (domain.User, domain.Org, error)
  ```
  Rules: email `strings.ToLower(strings.TrimSpace)`; password 12–128 chars else ValidationError; org name 1–100; Register creates org + owner user in one tx; Login unknown email still runs `VerifyPassword` against a fixed dummy hash, returns `ErrUnauthorized` for both unknown email and bad password; Refresh: `Consume` → ErrUnauthorized propagates, ErrUnavailable → ErrUnauthorized + `Warn` log (CACHING.md: refresh rejects when Redis down).

- [ ] **Step 1: Failing table tests:** register happy path (role owner, org created, tokens non-empty, email lowercased from `" Alice@Example.com "`); register duplicate (any case) → ErrAlreadyExists; short password → ValidationError field `password`; login wrong password and unknown email → both `ErrUnauthorized` with identical message; refresh rotates (old token consumed, new token saved); refresh with Redis unavailable → ErrUnauthorized; logout deletes.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS, `make check`.
- [ ] **Step 5:** Commit `feat(service): add register, login, refresh and logout`.

### Task A7: Auth HTTP, auth middleware, app wiring

**Files:**
- Create: `backend/internal/http/{auth_handler.go,auth_middleware.go}`, `backend/internal/app/api.go`, `backend/test/apitest/apitest.go`
- Modify: `backend/internal/http/router.go`, `backend/cmd/api/main.go`, `http_test.go`
- Test: `backend/test/m1a_auth_test.go` (HTTP part)

**Interfaces:**
- Produces:
  ```go
  // http
  type Principal struct{ UserID, OrgID uuid.UUID; Role domain.Role }
  func PrincipalFrom(ctx context.Context) (Principal, bool)
  type RouterDeps struct{ Logger *slog.Logger; Health *observability.Health; Auth *service.AuthService; JWT *authn.JWT
    /* M1b adds APIKeys, Catalog, Endpoints; M1c adds Ingest, RateLimiter, APIKeyAuth */ }
  func NewRouter(d RouterDeps) http.Handler   // replaces NewRouter(logger, health)
  func requireUser(j *authn.JWT) func(http.Handler) http.Handler   // Bearer JWT → Principal; else 401
  func requireRole(role domain.Role) func(http.Handler) http.Handler // else 403 FORBIDDEN
  // app
  type APIDeps struct{ Config config.Config; Pool *pgxpool.Pool; Redis *goredis.Client; Logger *slog.Logger; Health *observability.Health; Registry *prometheus.Registry }
  func NewAPIHandler(d APIDeps) (http.Handler, error)
  // apitest
  func New(t *testing.T, env *testenv.Env) *Client   // httptest.Server over app.NewAPIHandler
  func (c *Client) Do(method, path, token string, body any) (*http.Response, []byte)
  ```
- Routes: `POST /v1/auth/register` → 201 `{"data":{"user":{id,email,role},"org":{id,name},"access_token","expires_at","refresh_token"}}`; `POST /v1/auth/login` → 200 same shape; `POST /v1/auth/refresh` `{"refresh_token"}` → 200; `POST /v1/auth/logout` → 204; `GET /v1/me` (requireUser) → 200 `{user, org}`.

- [ ] **Step 1: Failing tests.** Unit (`http_test.go`): `TestRequireUser` — missing header / `Basic x` / expired token → 401 envelope; valid → handler sees Principal. Integration `TestAuthFlow`: register → 201; `/v1/me` with access token → org matches; login → 200; refresh → new pair; reuse old refresh → 401; logout then refresh → 401; 2 concurrent refreshes same token → one 200 one 401.
- [ ] **Step 2:** FAIL. **Step 3:** implement handlers (decode → service → respond), router groups `/v1/auth` (public) and authed group; `app.NewAPIHandler` builds repos/services; `cmd/api` calls it.
- [ ] **Step 4:** `make check`, `make test-int` PASS; `make up-apps` + manual register via test client.
- [ ] **Step 5:** Commit `feat(http): add auth endpoints and jwt middleware`.

### Task A8: Docs + PR

- [ ] Write `docs/openapi.yaml` (OpenAPI 3.1: auth endpoints, error envelope component, bearer scheme); `docs/API.md` link it and record "hand-written OpenAPI"; `docs/SPEC.md` FR-A1–A3 → ✅; roadmap unchanged until M1c.
- [ ] `make check && make test-int` PASS → commit `docs(docs): document auth api` → PR `feat/m1a-auth` → `main`.
- [ ] **User action (CI file is deny-listed for agents):** add step to `lint` job: `cd backend && sqlc diff` (fails if generated code is stale).

---

# PR M1b — API keys, event types, consumers, endpoints (FR-K, FR-T, FR-E1–E4)

Branch: `feat/m1b-catalog` from updated `main`.

### Task B1: Schema + sqlc repositories for keys and catalog

**Files:**
- Create: `backend/migrations/000003_create_api_keys.*`, `000004_create_catalog.*`, `queries/{apikeys,catalog}.sql`, `repository/postgres/{apikeys.go,catalog.go}`, `domain/{apikey.go,catalog.go}`; Modify `domain/repos.go`
- Test: `backend/test/m1b_catalog_test.go` (repo part)

**Interfaces:**
- Schema:
  - `api_keys(id uuid pk, org_id fk cascade, name text 1–100, prefix text not null, key_hash bytea not null unique, created_by uuid fk users on delete set null, created_at, revoked_at timestamptz null)`; index `(org_id, created_at desc, id desc)`.
  - `event_types(id, org_id fk cascade, name text, description text not null default '', example_payload jsonb null, created_at, updated_at, unique(org_id,name))`.
  - `consumers(id, org_id fk cascade, uid text, name text not null default '', created_at, updated_at, unique(org_id,uid))`.
  - `endpoints(id, org_id fk cascade, consumer_id fk consumers cascade, url text, description text default '', event_types text[] not null default '{}', secret_ciphertext bytea not null, enabled bool not null default true, created_at, updated_at)`; index `(consumer_id) where enabled`.
- Produces (domain):
  ```go
  type APIKey struct{ ID, OrgID uuid.UUID; Name, Prefix string; CreatedAt time.Time; RevokedAt *time.Time }
  type EventType struct{ ID, OrgID uuid.UUID; Name, Description string; ExamplePayload json.RawMessage; CreatedAt, UpdatedAt time.Time }
  type Consumer struct{ ID, OrgID uuid.UUID; UID, Name string; CreatedAt, UpdatedAt time.Time }
  type Endpoint struct{ ID, OrgID, ConsumerID uuid.UUID; URL, Description string; EventTypes []string; Enabled bool; SecretCiphertext []byte; CreatedAt, UpdatedAt time.Time }
  type Cursor struct{ CreatedAt time.Time; ID uuid.UUID }   // domain owns it; platform only encodes
  type Page struct{ Limit int; After *Cursor }
  type APIKeyRepository interface{ Create(ctx, k APIKey, hash []byte) error; List(ctx, orgID uuid.UUID, p Page) ([]APIKey, error);
    Revoke(ctx, orgID, id uuid.UUID, at time.Time) error; GetActiveByHash(ctx, hash []byte) (APIKey, error) }
  type EventTypeRepository interface{ Create; Get(ctx, orgID, id); GetByNames(ctx, orgID uuid.UUID, names []string) ([]EventType, error); List; Update; Delete }
  type ConsumerRepository interface{ Create; Get(ctx, orgID, id); GetByUID(ctx, orgID uuid.UUID, uid string); List; Update; Delete }
  type EndpointRepository interface{ Create; Get(ctx, orgID, id); ListByConsumer(ctx, orgID, consumerID uuid.UUID, p Page); Update; UpdateSecret(ctx, orgID, id uuid.UUID, ct []byte) error; Delete }
  ```
  Every `Get/Update/Delete/Revoke` filters `WHERE org_id = $1 AND id = $2`; zero rows → ErrNotFound. Lists: `WHERE org_id=$1 AND (created_at,id) < ($2,$3) ORDER BY created_at DESC, id DESC LIMIT $4` (limit+1 fetched by service to compute `has_more`).

- [ ] **Step 1: Failing integration tests** `TestCatalogRepositories`: duplicate event type name same org → ErrAlreadyExists, same name other org → ok; duplicate consumer uid → ErrAlreadyExists; `Get` with other org's id → ErrNotFound; delete consumer cascades endpoints; `GetActiveByHash` on revoked key → ErrNotFound; keyset list of 25 rows in pages of 10 returns all 25 exactly once in `created_at desc` order.
- [ ] **Step 2:** FAIL. **Step 3:** migrations + queries + `make gen` + repos. **Step 4:** PASS + migrate up/down/up. **Step 5:** Commit `feat(repo): add api keys and catalog schema`.

### Task B2: Cursor pagination and secretbox

**Files:** Create `backend/internal/platform/pagination.go`, `backend/internal/platform/secretbox/secretbox.go` + tests. (`domain.Cursor`/`domain.Page` come from B1.)

**Interfaces:**
- Produces:
  ```go
  func EncodeCursor(c domain.Cursor) string               // base64url(JSON), opaque to clients
  func DecodeCursor(s string) (domain.Cursor, error)      // bad → ValidationError field "cursor"
  func ParseLimit(s string) (int, error)           // "" → 20; 1..100 else ValidationError field "limit"
  // secretbox
  func New(key []byte) (*Box, error)               // len 32 else error
  func (b *Box) Seal(plaintext []byte) ([]byte, error)  // nonce(12) || ciphertext, AES-256-GCM
  func (b *Box) Open(sealed []byte) ([]byte, error)
  func NewSigningSecret() (string, error)          // "whsec_" + base64std(24 random bytes)
  ```

- [ ] **Step 1: Failing tests:** cursor round-trip; garbage cursor → ValidationError; limit "0","101","x" → error, "" → 20; Seal/Open round-trip; tampered byte → Open error; two Seals of same plaintext differ; `NewSigningSecret` has prefix `whsec_` and decodes to 24 bytes.
- [ ] **Step 2–4:** FAIL → implement → PASS. **Step 5:** Commit `feat(platform): add cursors and secret encryption`.

### Task B3: API keys — service + HTTP (owner only)

**Files:** Create `service/apikeys.go`, `http/apikey_handler.go` + tests; Modify `router.go`, `app/api.go`, `authn/tokens.go` (add key helpers).

**Interfaces:**
- Produces:
  ```go
  func NewAPIKey() (full, prefix string, hash []byte, err error) // "whk_" + base64url(32 bytes); prefix = full[:12]; hash = sha256(full)
  func HashAPIKey(full string) []byte
  type APIKeyService struct{...}; func NewAPIKeyService(repo domain.APIKeyRepository, clock platform.Clock) *APIKeyService
  func (s *APIKeyService) Create(ctx, orgID, userID uuid.UUID, name string) (domain.APIKey, string /*full key*/, error)
  func (s *APIKeyService) List(ctx, orgID uuid.UUID, p domain.Page) ([]domain.APIKey, *domain.Cursor /*next*/, error)
  func (s *APIKeyService) Revoke(ctx, orgID, id uuid.UUID) error
  func (s *APIKeyService) Authenticate(ctx context.Context, fullKey string) (orgID uuid.UUID, err error) // used in M1c; unknown/revoked → ErrUnauthorized
  ```
- Routes (requireUser + requireRole(owner)): `POST /v1/api-keys {name}` → 201 `{id,name,prefix,key,created_at}`; `GET /v1/api-keys?limit&cursor` → `{data:[{id,name,prefix,created_at,revoked_at}], meta:{next_cursor,limit,has_more}}` (never `key`); `DELETE /v1/api-keys/{id}` → 204.

- [ ] **Step 1: Failing tests:** unit — create returns full key starting `whk_`, stored hash = sha256(key), `Authenticate(revoked)` → ErrUnauthorized; integration — member token → 403 (seed a member via repo); list never contains field `key`; revoke then `Authenticate` → ErrUnauthorized (FR-K3).
- [ ] **Step 2–4:** FAIL → implement → PASS. **Step 5:** Commit `feat(http): add api key management`.

### Task B4: Event types — service + HTTP

**Files:** Create `service/catalog.go` (event types + consumers), `http/eventtype_handler.go` + tests.

**Interfaces:**
- Produces:
  ```go
  type CatalogService struct{...}; func NewCatalogService(et domain.EventTypeRepository, c domain.ConsumerRepository, clock platform.Clock) *CatalogService
  type EventTypeInput struct{ Name, Description string; ExamplePayload json.RawMessage }
  func (s *CatalogService) CreateEventType(ctx, orgID uuid.UUID, in EventTypeInput) (domain.EventType, error)
  func (s *CatalogService) GetEventType / ListEventTypes / DeleteEventType
  func (s *CatalogService) UpdateEventType(ctx, orgID, id uuid.UUID, description *string, example *json.RawMessage) (domain.EventType, error)
  ```
  Name regex `^[a-z0-9][a-z0-9_.-]{0,99}$`, immutable after create (PATCH with `name` → 400 unknown field). Example payload must be valid JSON ≤ 262144 bytes (FR-T2); never schema-validated (FR-T3).
- Routes (requireUser): `POST/GET /v1/event-types`, `GET/PATCH/DELETE /v1/event-types/{id}`.

- [ ] **Step 1: Failing tests:** name `Payment.Paid` → 400 field `name`; duplicate → 409 `ALREADY_EXISTS`; example `"not json"` rejected → 400; PATCH description only keeps example; list pagination meta.
- [ ] **Step 2–4.** **Step 5:** Commit `feat(http): add event type crud`.

### Task B5: Consumers and endpoints — service + HTTP

**Files:** Create `service/endpoints.go`, `http/{consumer_handler.go,endpoint_handler.go}` + tests; extend `service/catalog.go` with consumer methods.

**Interfaces:**
- Produces:
  ```go
  func (s *CatalogService) CreateConsumer(ctx, orgID uuid.UUID, uid, name string) (domain.Consumer, error) // uid ^[A-Za-z0-9_.:-]{1,128}$
  func (s *CatalogService) GetConsumer / ListConsumers / UpdateConsumer(name) / DeleteConsumer
  type EndpointService struct{...}
  func NewEndpointService(eps domain.EndpointRepository, consumers domain.ConsumerRepository, types domain.EventTypeRepository, box *secretbox.Box, clock platform.Clock) *EndpointService
  type EndpointInput struct{ URL, Description string; EventTypes []string; Enabled *bool }
  func (s *EndpointService) Create(ctx, orgID, consumerID uuid.UUID, in EndpointInput) (domain.Endpoint, string /*secret*/, error)
  func (s *EndpointService) Get / ListByConsumer / Update(ctx, orgID, id uuid.UUID, in EndpointPatch) / Delete
  func (s *EndpointService) RotateSecret(ctx, orgID, id uuid.UUID) (string, error)   // FR-E3
  ```
  URL: absolute, scheme `http` or `https`, host required, ≤ 2048 chars (https-only is FR-E5, M2). `EventTypes` deduplicated; every name must exist in the org (`GetByNames`) else `ErrUnprocessable` with details naming the unknown types; empty = all (FR-E2). `Enabled` default true (FR-E4).
- Routes (requireUser): `POST/GET /v1/consumers`, `GET/PATCH/DELETE /v1/consumers/{id}`, `POST/GET /v1/consumers/{id}/endpoints`, `GET/PATCH/DELETE /v1/endpoints/{id}`, `POST /v1/endpoints/{id}/rotate-secret` → `{secret}`. `secret` appears only in create and rotate responses.

- [ ] **Step 1: Failing tests:** endpoint with unknown event type → 422 `UNPROCESSABLE_ENTITY` + details; `ftp://x` → 400 field `url`; create returns `whsec_…`, GET does not contain `secret`; rotate → new secret, DB ciphertext changed and decrypts to it; disable via PATCH `{"enabled":false}` persists; **cross-org test** `TestCrossOrgIsolation`: org B gets 404 on GET/PATCH/DELETE of org A's api key, event type, consumer, endpoint, rotate-secret, and org A's rows are unchanged afterwards.
- [ ] **Step 2–4.** **Step 5:** Commit `feat(http): add consumer and endpoint crud`.

### Task B6: Docs + PR

- [ ] `docs/openapi.yaml` add all B routes + pagination schema; `docs/SPEC.md` FR-K1–K3, FR-T1–T3, FR-E1–E4 → ✅; `.env.example` comment for `SECRETS_ENCRYPTION_KEY` rotation note ("rotating the key needs a re-encryption migration — not supported in M1").
- [ ] `make check && make test-int` → commit `docs(docs): document catalog api` → PR `feat/m1b-catalog`.

---

# PR M1c — ingest, outbox, relay, rate limit (FR-I1–I5, NFR-D1)

Branch: `feat/m1c-ingest`.

### Task C1: Events + outbox schema and repositories

**Files:** Create `migrations/000005_create_events_outbox.*`, `queries/{events,outbox}.sql`, `repository/postgres/{events.go,outbox.go}`, `domain/event.go`; Test `test/m1c_ingest_test.go` (repo part).

**Interfaces:**
- Schema: `events(id uuid pk, org_id fk cascade, consumer_id fk consumers cascade, event_type_id fk event_types on delete restrict, consumer_uid text not null, type text not null, payload jsonb not null, idempotency_key text null, traceparent text null, created_at)`, `unique(org_id, idempotency_key)`, index `(org_id, created_at desc, id desc)`; `outbox(id bigserial pk, event_id uuid not null references events on delete cascade, created_at)`.
- Produces:
  ```go
  type Event struct{ ID, OrgID, ConsumerID, EventTypeID uuid.UUID; ConsumerUID, Type string; Payload json.RawMessage; IdempotencyKey *string; Traceparent string; CreatedAt time.Time }
  type EventRepository interface{
    Insert(ctx context.Context, e Event) (created bool, err error)        // ON CONFLICT (org_id, idempotency_key) DO NOTHING
    GetByIdempotencyKey(ctx context.Context, orgID uuid.UUID, key string) (Event, error)
  }
  type OutboxRecord struct{ OutboxID int64; Event Event }
  type OutboxRepository interface{
    Enqueue(ctx context.Context, eventID uuid.UUID) error
    LockBatch(ctx context.Context, limit int) ([]OutboxRecord, error)    // FOR UPDATE OF o SKIP LOCKED, ORDER BY o.id; must run inside WithinTx
    Delete(ctx context.Context, ids []int64) error
    Backlog(ctx context.Context) (int64, error)
  }
  ```

- [ ] **Step 1: Failing integration tests:** `TestIdempotentInsertConcurrent` — 10 goroutines each `WithinTx{Insert; if created Enqueue}` with the same key → 1 events row, 1 outbox row, 9 `created=false`; two concurrent `LockBatch(10)` transactions over 15 rows return disjoint sets; `LockBatch` outside a tx → error.
- [ ] **Step 2–4.** **Step 5:** Commit `feat(repo): add events and outbox`.

### Task C2: Redis rate limiter + middleware (fail open)

**Files:** Create `cache/redis/ratelimit.go`, `http/ratelimit_middleware.go` + tests.

**Interfaces:**
- Produces:
  ```go
  type RateLimiter interface{ Allow(ctx context.Context, orgID uuid.UUID) (allowed bool, retryAfter time.Duration, err error) } // domain
  func NewRateLimiter(c *goredis.Client, limit int, window time.Duration) *RateLimiter // window = 1m
  func rateLimit(rl domain.RateLimiter, logger *slog.Logger, failOpen prometheus.Counter) func(http.Handler) http.Handler
  ```
  Lua: `local n = redis.call('INCR', KEYS[1]); if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end; return {n, redis.call('PTTL', KEYS[1])}`, key `OrgRateLimitKey(orgID)`. Denied → 429 `RATE_LIMITED`, `Retry-After: ceil(pttl s)`. Error → allow, `Warn` "rate limit unavailable, failing open", counter `webhook_rate_limit_fail_open_total` += 1.

- [ ] **Step 1: Failing tests:** integration — limit 3: 3 allowed, 4th denied with `0 < retryAfter ≤ 1m`; other org unaffected; Redis stopped → `Allow` returns err; unit middleware — limiter error → handler called (200) and counter == 1; denied → 429 with `Retry-After` header.
- [ ] **Step 2–4.** **Step 5:** Commit `feat(cache): add per-org ingest rate limit`.

### Task C3: Ingest service, API-key auth, `POST /v1/events`

**Files:** Create `service/ingest.go`, `http/ingest_handler.go`, `http/apikey_middleware.go` + tests; Modify `router.go`, `app/api.go`.

**Interfaces:**
- Consumes: B3 `APIKeyService.Authenticate`, B1 `ConsumerRepository.GetByUID`, `EventTypeRepository.GetByNames`, C1 repos, C2 limiter.
- Produces:
  ```go
  type IngestService struct{...}
  func NewIngestService(tx domain.Transactor, events domain.EventRepository, outbox domain.OutboxRepository,
    consumers domain.ConsumerRepository, types domain.EventTypeRepository, maxPayload int, clock platform.Clock, ingested *prometheus.CounterVec) *IngestService
  type IngestInput struct{ OrgID uuid.UUID; ConsumerUID, Type string; Payload json.RawMessage; IdempotencyKey, Traceparent string }
  type IngestResult struct{ EventID uuid.UUID; Duplicate bool }
  func (s *IngestService) Ingest(ctx context.Context, in IngestInput) (IngestResult, error)
  func requireAPIKey(auth *service.APIKeyService) func(http.Handler) http.Handler // sets OrgID in ctx; 401 otherwise
  ```
  Rules: body limit = `maxPayload + 16384`; payload must be a JSON object, `len(payload) ≤ maxPayload` else ErrPayloadTooBig; unknown consumer uid / event type → `ErrUnprocessable` with details; `Idempotency-Key` 1–255 printable ASCII; duplicate → same `EventID`, `Duplicate=true`, header `Idempotent-Replayed: true`. Response `202 {"data":{"id","status":"accepted"}}`. Counter `webhook_events_ingested_total{result=accepted|duplicate|rejected|rate_limited}`. Middleware order on this route: requestID → apiKey → rateLimit → handler.

- [ ] **Step 1: Failing tests:** unit (fakes) — duplicate path returns existing id and does not enqueue; payload array → 400 field `payload`. Integration `TestIngest`: happy → 202 + one events row + one outbox row; payload of exactly 262144 bytes → 202, 262145 → 413; trailing garbage → 400; unknown field → 400; unknown consumer → 422; revoked key → 401; same key twice → same id; **Redis stopped → 202** (NFR-D1); **Kafka not involved: no relay running, event still accepted and outbox row present** (FR-I4).
- [ ] **Step 2–4.** **Step 5:** Commit `feat(http): add event ingest with outbox`.

### Task C4: Kafka producer, relay service, relay process

**Files:** Create `messaging/kafka/producer.go`, `service/relay.go`, `platform/poller.go`, `app/relay.go` + tests; Modify `cmd/relay/main.go`.

**Interfaces:**
- Produces:
  ```go
  type EventPublisher interface{ PublishEvents(ctx context.Context, events []Event) error } // domain; all-or-error, waits for acks
  func NewEventProducer(cl *kgo.Client, topicPrefix string) *EventProducer
  // record: topic Name(prefix, TopicEvents), key consumer_uid, value {"event_id","org_id","consumer_uid","type","accepted_at"}, headers schema-version=1, traceparent (if set)
  type Relay struct{...}; func NewRelay(tx domain.Transactor, outbox domain.OutboxRepository, pub domain.EventPublisher, batch int, backlog prometheus.Gauge) *Relay
  func (r *Relay) RunOnce(ctx context.Context) (moved int, err error)  // WithinTx{LockBatch → PublishEvents → Delete}; publish error → rollback, rows stay
  func NewPoller(name string, interval time.Duration, fn func(ctx context.Context) error, logger *slog.Logger) platform.Component // runs fn immediately then every interval; Stop waits for the running fn
  func NewRelayComponent(d RelayDeps) (platform.Component, error)   // app
  ```

- [ ] **Step 1: Failing tests:** unit `TestRelayRunOnce` with fakes — publish error → `Delete` not called, error returned; success → deleted ids equal locked ids. Unit `TestPoller` — fn called ≥2 times with fake short interval, Stop returns after in-flight fn finishes. Integration `TestIngestToKafka`: ingest 3 events with relay stopped → 3 outbox rows; `RunOnce` → 0 rows, consumer on `<prefix>events` reads 3 records keyed by `consumer_uid` with `schema-version=1`; publish to a non-existent topic (prefix without topics, auto-create off) → rows remain.
- [ ] **Step 2–4.** **Step 5:** Commit `feat(kafka): add outbox relay to events topic`.

### Task C5: Docs, roadmap, PR

- [ ] `docs/openapi.yaml` add `POST /v1/events` (API-key scheme, 202/400/401/413/422/429); `docs/SPEC.md` FR-I1–I5, NFR-D1 → ✅; `docs/ROADMAP.md` M1 ✅ with date; `docs/OBSERVABILITY.md` add `webhook_rate_limit_fail_open_total`.
- [ ] `make up-apps` smoke: register → create consumer + event type → API key → `POST /v1/events` → relay log shows moved=1 → `kafka-console-consumer` on `events` shows record.
- [ ] `make check && make test-int` → commit `docs(docs): document ingest and close m1` → PR `feat/m1c-ingest`.

---

## Verification (end of M1)

1. `make check` and `make test-int` green on each PR; CI green (incl. user-added `sqlc diff`).
2. Compose smoke in C5 passes end to end.
3. Stop Kafka (`docker compose stop kafka`), ingest 5 events → 202 each; start Kafka → relay drains outbox to 0, 5 records on `events` (FR-I4).
4. Stop Redis → ingest still 202, log line "rate limit unavailable, failing open" (NFR-D1); refresh returns 401 (users log in again).

## Execution

Recommend **Native** (implement in this session, one whole-branch review per PR): tasks are sequential and share interfaces tightly; 3 PRs already give review gates.
