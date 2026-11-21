package redis

import "testing"

func TestKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		got, want string
	}{
		{OrgRateLimitKey("o1"), "rl:org:o1"},
		{RefreshTokenKey("h1"), "rt:h1"},
		{APIKeyKey("h2"), "ak:h2"},
		{SubscriptionsKey("c1"), "subs:c1"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("key = %q, want %q", tt.got, tt.want)
		}
	}
}
