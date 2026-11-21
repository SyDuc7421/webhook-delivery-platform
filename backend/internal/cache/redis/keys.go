package redis

// Every Redis key is built here and nowhere else (docs/CACHING.md).

// OrgRateLimitKey is the ingest rate-limit counter for one org (FR-I5).
func OrgRateLimitKey(orgID string) string { return "rl:org:" + orgID }

// RefreshTokenKey stores one single-use refresh token, by hash (FR-A2).
func RefreshTokenKey(tokenHash string) string { return "rt:" + tokenHash }

// APIKeyKey caches API key hash → org (M5, NFR-P2).
func APIKeyKey(keyHash string) string { return "ak:" + keyHash }

// SubscriptionsKey caches consumer → subscribed endpoints (M5, NFR-P2).
func SubscriptionsKey(consumerID string) string { return "subs:" + consumerID }
