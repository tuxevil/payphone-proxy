package httpapi

import (
	"fmt"
	"testing"
	"time"
)

func TestFixedWindowLimiterDoesNotEvictActiveKeysWhenFull(t *testing.T) {
	limiter := newFixedWindowLimiter(1, time.Hour)
	for i := 0; i < maxRateLimitEntries; i++ {
		if !limiter.allow(fmt.Sprintf("key-%d", i)) {
			t.Fatalf("initial key %d was rejected", i)
		}
	}
	if limiter.allow("overflow") {
		t.Fatal("overflow key was accepted despite a full active limiter")
	}
	if limiter.allow("key-0") {
		t.Fatal("active key quota was reset by an overflow key")
	}
}
