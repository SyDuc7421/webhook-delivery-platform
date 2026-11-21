// Package observability wires log/slog (JSON, with trace_id), OpenTelemetry
// tracing (propagated in HTTP and Kafka record headers), Prometheus metrics
// and the health/readiness handlers.
package observability
