package domain

import "testing"

func TestJSONStorageIssue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		bad  bool
	}{
		{"plain", `{"a":1}`, false},
		{"accents", `{"a":"é"}`, false},
		{"escaped accent", `{"a":"é"}`, false},
		{"valid surrogate pair", `{"a":"😀"}`, false},
		{"escaped backslash then text u0000", `{"a":"\\u0000"}`, false},
		{"nul escape", `{"a":"\u0000"}`, true},
		{"nul escape in a key", `{"k\u0000":1}`, true},
		{"lone high surrogate", `{"a":"\ud800"}`, true},
		{"lone low surrogate", `{"a":"\udc00"}`, true},
		{"high surrogate then text", `{"a":"\ud83dx"}`, true},
		{"high surrogate then non-low escape", `{"a":"\ud83dA"}`, true},
		{"invalid utf-8", "{\"a\":\"\xff\"}", true},
		{"truncated escape", `{"a":"\u00"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := JSONStorageIssue([]byte(tt.in))
			if (got != "") != tt.bad {
				t.Errorf("JSONStorageIssue(%q) = %q, want bad=%v", tt.in, got, tt.bad)
			}
		})
	}
}
