// Package webhook sends outbound deliveries: Standard Webhooks signing
// (webhook-id, webhook-timestamp, webhook-signature) and an HTTP client with an
// SSRF guard checked at connect time, a 15 s timeout, no redirects and a capped
// response-body read (NFR-S1, NFR-S3, NFR-S4). All outbound HTTP goes through here.
package webhook
