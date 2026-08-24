package websocket

import "testing"

func TestAllowGroupProtocolSendRateLimit(t *testing.T) {
	s := &Server{groupProtocolHits: map[string][]int64{}}
	from := "PxTestSender"
	now := int64(1_000_000)
	for i := 0; i < groupProtocolRateLimit; i++ {
		if !s.allowGroupProtocolSend(from, now) {
			t.Fatalf("expected allow at hit %d", i+1)
		}
	}
	if s.allowGroupProtocolSend(from, now) {
		t.Fatal("expected rate limit at cap")
	}
	if !s.allowGroupProtocolSend(from, now+groupProtocolRateWindowSecs+1) {
		t.Fatal("expected window expiry to allow again")
	}
}
