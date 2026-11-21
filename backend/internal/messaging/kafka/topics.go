package kafka

// Topic names (docs/MESSAGING.md). They must match kafka-init in deploy/docker-compose.yml
// and the kafka Terraform module; TestTopicsMatchCompose guards the compose side.
const (
	TopicEvents       = "events"
	TopicDeliveries   = "deliveries"
	TopicRetry1m      = "deliveries.retry.1m"
	TopicRetry10m     = "deliveries.retry.10m"
	TopicRetry1h      = "deliveries.retry.1h"
	TopicRetry6h      = "deliveries.retry.6h"
	TopicDeliveriesDL = "deliveries.dlq"
)

// Consumer groups.
const (
	GroupDispatcher     = "dispatcher"
	GroupDeliveryWorker = "delivery-worker"
	GroupRetryWorker    = "retry-worker" // one group per tier: GroupRetryWorker + "." + tier
)

// AllTopics lists every topic the platform uses, in pipeline order.
func AllTopics() []string {
	return []string{
		TopicEvents, TopicDeliveries,
		TopicRetry1m, TopicRetry10m, TopicRetry1h, TopicRetry6h,
		TopicDeliveriesDL,
	}
}

// Name applies the configured prefix (empty locally; unique per integration test).
func Name(prefix, topic string) string { return prefix + topic }
