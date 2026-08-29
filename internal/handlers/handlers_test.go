package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"newsanalyzer/internal/auth"
	"newsanalyzer/internal/scheduler"
)

func TestNormalizeDigestKind(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"", "news", true},
		{"news", "news", true},
		{"facts", "facts", true},
		{"News", "", false},
		{"NEWS", "", false},
		{"unknown", "", false},
		{" news ", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeDigestKind(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("normalizeDigestKind(%q) = (%q, %v), want (%q, %v)",
				c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestTriggerDigestQueueFull(t *testing.T) {
	h := &Handlers{Sched: scheduler.New(nil, nil, "")}
	for i := 0; i < 32; i++ {
		h.Sched.Trigger("x")
	}
	req := httptest.NewRequest("POST", "/api/digests/abc/run", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()
	h.triggerDigest(rec, req)
	if rec.Code != 503 {
		t.Fatalf("triggerDigest on full queue = %d, want 503", rec.Code)
	}
}

func TestLoginRateLimited(t *testing.T) {
	h := &Handlers{Limiter: auth.NewLoginLimiter()}
	// httptest.NewRequest always sets RemoteAddr to 192.0.2.1:1234.
	key := "192.0.2.1|user@example.com"
	for i := 0; i < 5; i++ {
		h.Limiter.Fail(key)
	}
	req := httptest.NewRequest("POST", "/api/auth/login",
		strings.NewReader(`{"email":"user@example.com","password":"x"}`))
	rec := httptest.NewRecorder()
	h.login(rec, req)
	if rec.Code != 429 {
		t.Fatalf("login while blocked = %d, want 429", rec.Code)
	}
}
