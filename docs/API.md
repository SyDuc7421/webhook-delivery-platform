# HTTP API conventions

Router: `net/http` + [chi](https://github.com/go-chi/chi) (ADR-0009). OpenAPI is the contract.

## Surfaces

| Surface | Prefix | Auth | Used by |
| --- | --- | --- | --- |
| Ingest | `POST /v1/events` | API key: `Authorization: Bearer whk_…` | the org's backend |
| Dashboard | `/v1/…` | JWT access token; refresh via single-use refresh token | `web` |
| Auth | `/v1/auth/register`, `/login`, `/refresh`, `/logout` | none / refresh token | `web` |
| Test receiver | `/receiver/{id}` | none; allowlisted outbound host (NFR-S2) | the platform itself |
| Ops | `/healthz`, `/readyz` on the API port; `/metrics` on `METRICS_ADDR` | none | LB, Prometheus |

## URLs

- `/v{n}/<plural-noun>` — kebab-case, nouns only. Nested resources follow ownership:
  `/v1/consumers/{consumer_id}/endpoints`. Verbs only for true actions:
  `POST /v1/messages/{id}/replay`, `POST /v1/endpoints/{id}/rotate-secret`,
  `POST /v1/endpoints/{id}/test`.
- Every dashboard route is scoped to the caller's org from the token, never from the path or
  body (FR-A3). A resource from another org is `404`, not `403`.
- Version in the path. Breaking change → new version.

## Methods & status codes

| Action | Method | Success |
| --- | --- | --- |
| List | GET | 200 |
| Read | GET | 200 / 404 |
| Create | POST | 201 + `Location` |
| Partial update | PATCH | 200 |
| Delete | DELETE | 204 |
| Ingest, replay | POST | 202 |

`400` malformed · `401` unauthenticated / revoked key · `403` role not allowed (member vs owner) ·
`404` missing · `409` conflict (duplicate name/uid) · `413` payload > 256 KB · `422` semantically
invalid · `429` rate limited (+ `Retry-After`) · `500` unexpected · `503` dependency down.

## Ingest contract (FR-I1–I5)

```http
POST /v1/events
Authorization: Bearer whk_live_…
Idempotency-Key: order-123-paid          (optional)
Content-Type: application/json

{ "consumer_uid": "shop_42", "type": "payment.paid", "payload": { … } }
```

→ `202 { "data": { "id": "01J…", "status": "accepted" } }`. A repeated `Idempotency-Key` for the
same org returns the original event (same `id`) and creates nothing (Postgres unique
`(org_id, idempotency_key)`, not Redis). Kafka is never touched in the request.

## Response envelope

```json
{ "data": { }, "meta": { } }
{ "data": [], "meta": { "next_cursor": "…", "limit": 20, "has_more": true } }
```

Error — **always** this shape, built only by `internal/http/errors.go`:

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "human readable, safe to show",
    "details": [{ "field": "url", "issue": "must be https" }]
  },
  "request_id": "01J…"
}
```

`code` is a stable SCREAMING_SNAKE string — clients switch on it, never on `message`.

## Pagination and filtering

- **Cursor only** (FR-W3): `?limit=20&cursor=…`. The cursor is an opaque, encoded
  `(created_at, id)` keyset. No offset pagination.
- `limit` default 20, max 100.
- Filters are explicit query params (`?type=payment.paid&status=dead&consumer_uid=…&from=…&to=…`)
  so the web app can keep them in the URL (FR-W2). No free-form query language.

## Headers

- `X-Request-ID` accepted from the edge, generated if absent, echoed in every response and log.
- `traceparent` accepted and propagated (NFR-O1).
- `Content-Type: application/json; charset=utf-8`.

## Decoding and validation

- `http.MaxBytesReader` (256 KB on ingest), `json.Decoder` with `DisallowUnknownFields`.
- Shape rules: `validate` tags on the DTO (go-playground/validator, ADR-0009). Domain rules
  (https-only URL, event type exists) are checked in the service.
- Handlers map DTO → service input and never pass an `*http.Request` or chi type deeper than the
  handler.

## Handler skeleton

```go
func (h *EndpointHandler) Create(w http.ResponseWriter, r *http.Request) {
    var req CreateEndpointRequest
    if err := decodeJSON(w, r, &req); err != nil {
        respondError(w, r, err); return
    }
    out, err := h.svc.CreateEndpoint(r.Context(), auth.OrgID(r.Context()),
        chi.URLParam(r, "consumer_id"), req.ToInput())
    if err != nil {
        respondError(w, r, err); return
    }
    w.Header().Set("Location", "/v1/endpoints/"+out.ID)
    respondJSON(w, http.StatusCreated, toEndpointResponse(out))
}
```

No `http.Error(w, err.Error(), 500)` anywhere. Ever. Signing secrets are returned only by the
create and rotate responses, never by GET.

## Documentation

OpenAPI lives at [`docs/openapi.yaml`](openapi.yaml) and is **hand-written** (decided in M1): it is the
single source for the contract. A PR that changes a contract without updating it fails review.
