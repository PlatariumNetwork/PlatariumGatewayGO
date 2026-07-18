package handlers

import "testing"

func TestIsPermanentMempoolInvalid(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"invalid nonce: expected 0, got 1", false}, // future — keep
		{"Invalid nonce: expected 2, got 5", false},
		{"State error: Invalid nonce: expected 1, got 3", false},
		{"invalid nonce: expected 2, got 1", true}, // stale — drop
		{"invalid nonce: expected 1, got 0", true},
		{"insufficient PLP balance: need 10, available 0", true},
		{"signature verification failed", true},
		{"", true},
	}
	for _, tc := range cases {
		got := isPermanentMempoolInvalid(tc.msg)
		if got != tc.want {
			t.Fatalf("msg=%q got=%v want=%v", tc.msg, got, tc.want)
		}
	}
}
