// Package redis implements per-org rate limits, single-use refresh tokens and the
// cache-aside lookups (API key -> org, consumer -> endpoints). Redis holds only
// loss-tolerant state: every key has a TTL and every caller degrades when Redis
// is down (NFR-D1). Every key is built by keys.go.
package redis
