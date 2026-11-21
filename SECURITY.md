# Security Policy

## Reporting

Use GitHub private vulnerability reporting (Security → Report a vulnerability) — do not open a
public issue.

## Baseline requirements for all code

- Secrets come from env/secret manager only. `.env` is never committed; `gitleaks` runs in
  pre-commit and CI.
- All SQL is parameterised. No dynamic SQL string building.
- All input validated at the HTTP boundary (`validate` tags + custom validators); never trust
  client-supplied IDs for authorization. Org comes from the token / API key only (FR-A3).
- AuthZ is checked in the service layer, not only in middleware.
- Rate limiting on ingest per org (Redis, fails open by design — NFR-D1).
- API keys and refresh tokens stored as hashes; full API key shown once (FR-K1–K2).
- Outbound webhooks: SSRF guard at connect time (private, loopback, link-local, CGNAT, metadata
  blocked; DNS-rebinding safe), 15 s timeout, no redirects, capped body (NFR-S1, S3).
- Signatures: HMAC-SHA256, Standard Webhooks headers, timestamp tolerance (NFR-S4).
- Passwords: argon2id. Tokens: opaque + hashed at rest, or short-lived JWT with rotation.
- TLS terminated at the edge; internal traffic on a private network.
- Dependencies scanned with `govulncheck` + Dependabot; images scanned with Trivy in CI.
- Logs and traces must never contain passwords, API keys, signing secrets, tokens, cookies, or
  payload/response bodies (NFR-S5).
- AWS: GitHub Actions authenticates with OIDC; no stored AWS keys.
