package handlers

import (
	"net/http/httptest"
	"testing"

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
