package auth

import (
	"testing"
	"time"
)

func TestLoginLimiter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := NewLoginLimiter()
	l.now = func() time.Time { return now }
	key := "1.2.3.4|user@example.com"

	for i := 0; i < 4; i++ {
		l.Fail(key)
		if ok, _ := l.Allowed(key); !ok {
			t.Fatalf("blocked after only %d failures", i+1)
		}
	}
	l.Fail(key) // 5th failure blocks
	ok, wait := l.Allowed(key)
	if ok || wait <= 0 || wait > time.Minute {
		t.Fatalf("after 5th failure: ok=%v wait=%v, want blocked for <=1m", ok, wait)
	}

	now = now.Add(61 * time.Second)
	if ok, _ := l.Allowed(key); !ok {
		t.Fatal("still blocked after base block expired")
	}

	l.Fail(key) // 6th failure doubles the block
	_, wait = l.Allowed(key)
	if wait < 119*time.Second || wait > 2*time.Minute {
		t.Fatalf("after 6th failure wait=%v, want ~2m", wait)
	}

	// Blocks are capped at 15 minutes.
	for i := 0; i < 10; i++ {
		now = now.Add(16 * time.Minute)
		l.Fail(key)
	}
	if _, wait = l.Allowed(key); wait > 15*time.Minute {
		t.Fatalf("wait=%v exceeds 15m cap", wait)
	}

	l.Success(key)
	if ok, _ := l.Allowed(key); !ok {
		t.Fatal("success must reset the key")
	}
}

func TestLoginLimiterGC(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := NewLoginLimiter()
	l.now = func() time.Time { return now }
	l.Fail("stale-key")
	now = now.Add(31 * time.Minute)
	l.Allowed("other-key") // triggers gc
	if _, exists := l.entries["stale-key"]; exists {
		t.Fatal("stale entry not collected")
	}
}
