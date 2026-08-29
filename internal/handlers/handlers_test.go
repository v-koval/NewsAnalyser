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

func TestPageParams(t *testing.T) {
	cases := []struct {
		query   string
		limit   int
		offset  int
		wantErr bool
	}{
		{"", 20, 0, false},
		{"?page=1&per_page=20", 20, 0, false},
		{"?page=3&per_page=50", 50, 100, false},
		{"?per_page=100", 100, 0, false},
		{"?page=0", 0, 0, true},
		{"?page=-1", 0, 0, true},
		{"?page=abc", 0, 0, true},
		{"?per_page=0", 0, 0, true},
		{"?per_page=101", 0, 0, true},
		{"?per_page=abc", 0, 0, true},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/api/runs"+c.query, nil)
		limit, offset, err := pageParams(req)
		if (err != nil) != c.wantErr || limit != c.limit || offset != c.offset {
			t.Errorf("pageParams(%q) = (%d, %d, %v), want (%d, %d, err=%v)",
				c.query, limit, offset, err, c.limit, c.offset, c.wantErr)
		}
	}
}

func TestListRunsRejectsBadParams(t *testing.T) {
	h := &Handlers{} // params are rejected before the repo is touched
	for _, u := range []string{
		"/api/runs?page=0",
		"/api/runs?per_page=101",
		"/api/runs?page=abc",
		"/api/runs?digest_id=not-a-uuid",
		"/api/runs?status=weird",
	} {
		req := httptest.NewRequest("GET", u, nil)
		rec := httptest.NewRecorder()
		h.listRuns(rec, req)
		if rec.Code != 400 {
			t.Errorf("%s: got %d, want 400", u, rec.Code)
		}
	}
}
