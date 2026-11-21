# System Specification — webhook-platform

What the system must do. Each requirement is tagged with the milestone that delivers it
(see [ROADMAP.md](ROADMAP.md)).

- **Status legend:** ✅ met · ⚠️ partial · ❌ not met · 🗓️ planned (Mx)
- Architecture: [ARCHITECT.md](ARCHITECT.md) · Deploy: [DEPLOY.md](DEPLOY.md) · Testing: [TESTING.md](TESTING.md)

---

## 1. Purpose and scope

A webhook delivery platform, a small version of Svix / Hookdeck. A SaaS company (the **org**) calls one
API whenever something happens in its product (`payment.paid`). The platform delivers that event as a
signed HTTP POST to every endpoint the org's customers registered, retries failures, keeps per-endpoint
order, records every attempt, and lets people replay deliveries.

**Out of scope:** live always-on hosting, multi-region, billing, SSO, email, exactly-once delivery.

---

## 2. Concepts

| Concept | Meaning |
|---|---|
| **User** | A person who logs into the dashboard. Belongs to one org. Role `owner` or `member`. |
| **Org** | The SaaS company using the platform (tenant). Owns API keys, event types and consumers. |
| **API key** | Credential the org's backend uses to call the ingest API. Stored as hash + visible prefix. |
| **Event type** | A named kind of event (`payment.paid`) with an optional example payload. |
| **Consumer** | One of the org's customers (e.g. a shop), identified by the org's own `uid`. |
| **Endpoint** | A URL owned by a consumer, with a signing secret and a list of subscribed event types. |
| **Event** | One thing that happened: org, consumer `uid`, type, JSON payload, optional idempotency key. |
| **Message** | One event addressed to one endpoint. Status `pending`, `succeeded`, `failed` or `dead`. |
| **Delivery attempt** | One HTTP try for a message: request, response, latency, error. |

---

## 3. Functional requirements

### 3.1 Accounts and orgs

| ID | Requirement | Status |
|---|---|---|
| FR-A1 | A user registers with email + password; registering creates a new org with the user as `owner` | ✅ |
| FR-A2 | Login returns an access token and a single-use refresh token | ✅ |
| FR-A3 | Every dashboard API call is scoped to the caller's org; no cross-org reads or writes | ✅ |

### 3.2 API keys

| ID | Requirement | Status |
|---|---|---|
| FR-K1 | An owner creates an API key; the full key is shown exactly once | ✅ |
| FR-K2 | Only a hash and a short prefix are stored | ✅ |
| FR-K3 | A revoked key is rejected immediately | ✅ |

### 3.3 Event types

| ID | Requirement | Status |
|---|---|---|
| FR-T1 | CRUD event types; name is unique per org | ✅ |
| FR-T2 | An event type may carry an example payload, used by "send test event" | ✅ (stored; "send test event" is M4) |
| FR-T3 | Payloads are **not** validated against a schema (MVP) | ✅ |

### 3.4 Consumers and endpoints

| ID | Requirement | Status |
|---|---|---|
| FR-E1 | CRUD consumers; `uid` is unique per org | ✅ |
| FR-E2 | CRUD endpoints under a consumer: URL, description, subscribed event types (empty = all) | ✅ |
| FR-E3 | Each endpoint has a signing secret; the secret can be rotated | ✅ |
| FR-E4 | An endpoint can be disabled; disabled endpoints receive nothing | ⚠️ (flag stored; the dispatcher enforces it in M2) |
| FR-E5 | Endpoint URLs must be `https` (plain `http` allowed only in local dev) | 🗓️ M2 |

### 3.5 Ingest

| ID | Requirement | Status |
|---|---|---|
| FR-I1 | `POST /v1/events` with an API key accepts `{consumer_uid, type, payload}` | ✅ |
| FR-I2 | Payload is JSON, max 256 KB | ✅ |
| FR-I3 | With an `Idempotency-Key` header, a repeated request returns the original event and creates nothing new | ✅ |
| FR-I4 | An accepted event is never lost, even if Kafka is down at the moment of the request | ✅ |
| FR-I5 | Ingest is rate-limited per org, with a limit shared by all `api` instances (Redis); over the limit returns `429` with `Retry-After` | ✅ |

### 3.6 Delivery

| ID | Requirement | Status |
|---|---|---|
| FR-D1 | Every enabled endpoint of the consumer that subscribes to the event type gets one message | 🗓️ M2 |
| FR-D2 | Each message is sent as an HTTP POST with `webhook-id`, `webhook-timestamp` and `webhook-signature` headers | 🗓️ M2 |
| FR-D3 | A `2xx` response marks the message `succeeded`; anything else, or a timeout, schedules a retry | 🗓️ M2 |
| FR-D4 | Retries follow tiers 1 min → 10 min → 1 h → 6 h; after the last tier the message is `dead` | 🗓️ M2 |
| FR-D5 | First attempts to the same endpoint are made in the order events were accepted | 🗓️ M2 |
| FR-D6 | Every attempt is recorded: request headers, response status, truncated response body, latency, error | 🗓️ M2 |

### 3.7 Replay

| ID | Requirement | Status |
|---|---|---|
| FR-R1 | A user replays one message; it is sent again with the same `webhook-id` | 🗓️ M2 |
| FR-R2 | Replay works for `succeeded`, `failed` and `dead` messages | 🗓️ M2 |

### 3.8 Dashboard

| ID | Requirement | Status |
|---|---|---|
| FR-W1 | Screens for API keys, event types, consumers and endpoints (all CRUD above) | 🗓️ M4 |
| FR-W2 | Event log filterable by type, status, consumer and time; filters live in the URL | 🗓️ M4 |
| FR-W3 | All lists use cursor pagination | 🗓️ M4 |
| FR-W4 | Event detail shows payload, messages per endpoint and every attempt, with a replay button | 🗓️ M4 |
| FR-W5 | "Send test event" from an endpoint, using the event type's example payload | 🗓️ M4 |

### 3.9 Test receiver

| ID | Requirement | Status |
|---|---|---|
| FR-X1 | The platform hosts a test endpoint that records what it receives | 🗓️ M4 |
| FR-X2 | The test endpoint can be switched to "return 500" or "delay 5 s" from the dashboard | 🗓️ M4 |

---

## 4. Non-functional requirements

### 4.1 Reliability

| ID | Requirement | Status |
|---|---|---|
| NFR-R1 | At-least-once delivery: killing any worker mid-batch loses no message (proven by a test) | 🗓️ M5 |
| NFR-R2 | One failing endpoint does not delay deliveries to other endpoints | 🗓️ M2 |
| NFR-R3 | Every process shuts down gracefully, finishing or handing back in-flight work | 🗓️ M2 |

### 4.2 Security

| ID | Requirement | Status |
|---|---|---|
| NFR-S1 | Outbound requests never reach private, loopback, link-local, CGNAT or cloud-metadata addresses, checked at connect time (DNS rebinding safe) | 🗓️ M2 |
| NFR-S2 | Only the built-in test receiver is allowlisted, by config | 🗓️ M4 |
| NFR-S3 | Outbound requests: 15 s timeout, redirects not followed, response body read capped | 🗓️ M2 |
| NFR-S4 | Signatures let receivers reject forged and replayed requests (timestamp tolerance) | 🗓️ M2 |
| NFR-S5 | Secrets (API keys, signing secrets) never appear in logs or traces | 🗓️ M3 |

### 4.3 Performance

| ID | Requirement | Status |
|---|---|---|
| NFR-P1 | Throughput, end-to-end p95 latency and lag recovery are measured with k6 and published | 🗓️ M5 |
| NFR-P2 | API key and endpoint-subscription lookups are cached in Redis, added only after a baseline run; before/after results are published | 🗓️ M5 |
| NFR-P3 | Caching never delays a revocation or an endpoint change: the cache entry is deleted on write, and TTL ≤ 60 s bounds any race | 🗓️ M5 |

No target numbers are set before the first measurement.

### 4.4 Degraded mode

| ID | Requirement | Status |
|---|---|---|
| NFR-D1 | If Redis is unavailable, ingest and delivery keep working: rate limits fail open (logged), caches fall back to Postgres | ✅ |

### 4.5 Observability

| ID | Requirement | Status |
|---|---|---|
| NFR-O1 | One trace follows an event from the API request to the outbound HTTP call | 🗓️ M3 |
| NFR-O2 | Metrics: ingest rate, success rate, attempt latency, retry-tier depth, DLQ count, consumer lag, outbox backlog | 🗓️ M3 |
| NFR-O3 | Structured logs carry `trace_id` | 🗓️ M3 |
| NFR-O4 | Dashboards and alert rules are provisioned as code | 🗓️ M3 |

### 4.6 Cost and operability

| ID | Requirement | Status |
|---|---|---|
| NFR-C1 | The full AWS environment can be created and destroyed from GitHub Actions | 🗓️ M6 |
| NFR-C2 | Nothing on AWS is billed beyond Free plan credits | 🗓️ M6 |

---

## 5. Not in the MVP

Live event log (SSE), endpoint health charts, bulk replay, embeddable consumer portal with scoped
tokens, RBAC beyond owner/member, per-endpoint circuit breaker and send rate limit (both shared
across workers through Redis).
See [ROADMAP.md](ROADMAP.md#stretch).
