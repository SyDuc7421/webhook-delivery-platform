// Package kafka owns topic names, producers and consumer-group loops for the
// delivery pipeline (events, deliveries, deliveries.retry.*, deliveries.dlq).
// Offsets are committed only after the work for a record is durable; retry-tier
// consumers pause partitions until a record is due instead of sleeping.
// Trace context travels in record headers.
package kafka
