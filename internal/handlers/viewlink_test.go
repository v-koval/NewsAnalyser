package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"newsanalyzer/internal/auth"
)

func TestVerifyViewLink(t *testing.T) {
	secret := []byte("s3cret")
	now := time.Now()
	exp := now.Add(15 * time.Minute).Unix()
	expStr := strconv.FormatInt(exp, 10)
	sig := viewLinkSig(secret, "run-1", exp)

	if !verifyViewLink(secret, "run-1", expStr, sig, now) {
		t.Error("valid link rejected")
	}
	if verifyViewLink(secret, "run-1", expStr, sig, now.Add(16*time.Minute)) {
		t.Error("expired link accepted")
	}
	if verifyViewLink(secret, "run-2", expStr, sig, now) {
		t.Error("signature accepted for a different run id")
	}
	if verifyViewLink(secret, "run-1", expStr, sig+"00", now) {
		t.Error("tampered signature accepted")
	}
	if verifyViewLink(secret, "run-1", "abc", sig, now) {
		t.Error("garbage exp accepted")
	}
}

func TestRunViewLinkRoundTrip(t *testing.T) {
	h := &Handlers{Auth: auth.New(nil, "s3cret", 15, 720)}
	req := httptest.NewRequest("GET", "/api/runs/run-1/view-link", nil)
	req.SetPathValue("id", "run-1")
	rec := httptest.NewRecorder()
	h.runViewLink(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(out["url"])
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if !verifyViewLink(h.Auth.Secret, "run-1", q.Get("exp"), q.Get("sig"), time.Now()) {
		t.Errorf("issued link does not verify: %s", out["url"])
	}
}

func TestViewRunRejectsUnsigned(t *testing.T) {
	h := &Handlers{Auth: auth.New(nil, "s3cret", 15, 720)}
	cases := []string{
		"/runs/run-1/view",
		"/runs/run-1/view?exp=abc&sig=zz",
		"/runs/run-1/view?exp=1&sig=" + viewLinkSig(h.Auth.Secret, "run-1", 1), // expired
	}
	for _, u := range cases {
		req := httptest.NewRequest("GET", u, nil)
		req.SetPathValue("id", "run-1")
		rec := httptest.NewRecorder()
		h.viewRun(rec, req)
		if rec.Code != 404 {
			t.Errorf("%s: got %d, want 404", u, rec.Code)
		}
		if rec.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: Referrer-Policy = %q, want %q", u, rec.Header().Get("Referrer-Policy"), "no-referrer")
		}
	}
}
