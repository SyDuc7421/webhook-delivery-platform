// Package http contains the chi router, middleware, handlers, DTOs and the
// single error-to-response mapping. It serves the dashboard API, the ingest API
// (POST /v1/events) and the built-in test receiver. Handlers are thin:
// decode, validate, map, call a service, respond.
package http
