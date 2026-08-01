package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := New(2, time.Minute)
	if !l.Allow("a") || !l.Allow("a") {
		t.Fatal("first two should pass")
	}
	if l.Allow("a") {
		t.Fatal("third should fail")
	}
	if !l.Allow("b") {
		t.Fatal("other key ok")
	}
}
