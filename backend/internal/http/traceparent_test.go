package http

import (
	"strings"
	"testing"
)

func TestValidTraceparent(t *testing.T) {
	t.Parallel()
	good := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	tests := []struct {
		in   string
		want bool
	}{
		{good, true},
		{"", false},
		{strings.Repeat("a", 100000), false},
		{"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-0", false},
		{"00-0AF7651916CD43DD8448EB211C80319C-b7ad6b7169203331-01", false}, // uppercase is not allowed
		{"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01\xff", false},
		{"00-00000000000000000000000000000000-b7ad6b7169203331-01", false}, // all-zero trace id
		{"00-0af7651916cd43dd8448eb211c80319c-0000000000000000-01", false}, // all-zero span id
		{"ff-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", false}, // version ff is invalid
	}
	for _, tt := range tests {
		if got := validTraceparent(tt.in); got != tt.want {
			t.Errorf("validTraceparent(%.60q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
