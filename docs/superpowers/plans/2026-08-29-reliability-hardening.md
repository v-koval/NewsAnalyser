# Reliability Hardening Implementation Plan (Phase 1 of 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make digest processing reliable and safe: async manual runs, honest trigger queue, stale-run recovery, visible mail failures with retries, SSRF-guarded and size-limited image downloads, signed view links, retention cleanup, and login rate limiting.

**Architecture:** All changes stay within the existing layering (handlers → repo, scheduler → processor). Migration 005 adds mail status columns, a retention setting, and run indexes. The scheduler gains async triggers and a daily cleanup pass; the processor records mail delivery via a small retry helper; `internal/images` gets a hardened HTTP client; view pages move to HMAC-signed short-lived URLs; auth gains an in-memory login limiter.

**Tech Stack:** Go 1.22 (net/http 1.22 routing, pgx/v5), PostgreSQL 16. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-29-service-improvements-design.md` (sections 2, 3).

**Order:** This plan is Phase 1. Execute before `2026-08-29-pagination-filters-api.md` (Phase 2) and `2026-08-29-frontend-redesign.md` (Phase 3).

---

## File Structure

**Create:**
- `internal/db/migrations/005_reliability.sql` — mail status, retention setting, indexes
- `internal/processor/retry.go` + `internal/processor/retry_test.go` — pure retry helper
- `internal/images/guard.go` + `internal/images/guard_test.go` — URL/IP validation, safe HTTP client
- `internal/images/fetch_test.go` — size/content-type limit tests
- `internal/handlers/viewlink.go` + `internal/handlers/viewlink_test.go` — HMAC view links
- `internal/auth/ratelimit.go` + `internal/auth/ratelimit_test.go` — login limiter
- `internal/scheduler/scheduler_test.go` — trigger queue test

**Modify:**
- `internal/models/models.go` — `Settings.KeepRunsDays`, `DigestRun.MailStatus/MailError`
- `internal/repo/repo.go` — settings columns, run mail columns, `SetRunMail`, `FailStaleProcessing`, cleanup queries
- `internal/scheduler/scheduler.go` — async trigger, bool `Trigger`, `ImagesDir`, daily cleanup
- `internal/processor/processor.go` — `sendMailAndRecord` replaces `sendMail`
- `internal/images/images.go` — safe client, limits in `Fetch`, scheme check in `ResolveArticleImage`
- `internal/handlers/handlers.go` — 503 on full queue, view-link route, settings validation, login limiter
- `internal/handlers/handlers_test.go` — new handler tests
- `internal/handlers/web/app.js` — open view via view-link; preserve `keep_runs_days` on settings save
- `cmd/server/main.go` — stale sweep on boot, new scheduler constructor arg

---

## Task 1: Migration 005, model fields, settings/run columns in repo

**Files:**
- Create: `internal/db/migrations/005_reliability.sql`
- Modify: `internal/models/models.go`, `internal/repo/repo.go`

- [ ] **Step 1: Create the migration**

Create `internal/db/migrations/005_reliability.sql`:

```sql
ALTER TABLE digest_runs
    ADD COLUMN IF NOT EXISTS mail_status TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS mail_error TEXT NOT NULL DEFAULT '';

ALTER TABLE settings
    ADD COLUMN IF NOT EXISTS keep_runs_days INT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_digest_runs_processed ON digest_runs(processed_at DESC);
CREATE INDEX IF NOT EXISTS idx_digest_runs_status ON digest_runs(status, processed_at DESC);
```

`IF NOT EXISTS` keeps the migration idempotent — migrations in this project run on every boot (`db.Migrate` executes every file each time), so idempotency is mandatory. `mail_status` values: `''` (legacy/not applicable), `sent`, `failed`, `skipped`. `keep_runs_days = 0` means keep forever.

- [ ] **Step 2: Add model fields**

In `internal/models/models.go`, add `KeepRunsDays` to `Settings` (after `ProcessingPaused`):

```go
type Settings struct {
	CursorAPIKey     string `json:"cursor_api_key"`
	CursorRepository string `json:"cursor_repository"`
	SMTPHost         string `json:"smtp_host"`
	SMTPPort         int    `json:"smtp_port"`
	SMTPUser         string `json:"smtp_user"`
	SMTPPassword     string `json:"smtp_password"`
	SMTPFrom         string `json:"smtp_from"`
	SMTPTLS          bool   `json:"smtp_tls"`
	ProcessingPaused bool   `json:"processing_paused"`
	KeepRunsDays     int    `json:"keep_runs_days"`
}
```

And add `MailStatus`/`MailError` to `DigestRun` (after `Error`):

```go
type DigestRun struct {
	ID              string     `json:"id"`
	DigestID        string     `json:"digest_id"`
	DigestName      string     `json:"digest_name"`
	AnalyzedSources []string   `json:"analyzed_sources"`
	ProcessedAt     time.Time  `json:"processed_at"`
	PeriodFrom      time.Time  `json:"period_from"`
	PeriodTo        time.Time  `json:"period_to"`
	HTML            string     `json:"html,omitempty"`
	Status          string     `json:"status"`
	Error           string     `json:"error,omitempty"`
	MailStatus      string     `json:"mail_status"`
	MailError       string     `json:"mail_error,omitempty"`
	Materials       []Material `json:"materials,omitempty"`
}
```

- [ ] **Step 3: Read/write the new columns in repo**

In `internal/repo/repo.go`, replace `GetSettings` and `UpdateSettings`:

```go
func (r *Repo) GetSettings(ctx context.Context) (models.Settings, error) {
	var s models.Settings
	err := r.Pool.QueryRow(ctx, `SELECT cursor_api_key,cursor_repository,smtp_host,smtp_port,smtp_user,smtp_password,smtp_from,smtp_tls,processing_paused,keep_runs_days FROM settings WHERE id=1`).
		Scan(&s.CursorAPIKey, &s.CursorRepository, &s.SMTPHost, &s.SMTPPort, &s.SMTPUser, &s.SMTPPassword, &s.SMTPFrom, &s.SMTPTLS, &s.ProcessingPaused, &s.KeepRunsDays)
	return s, err
}

func (r *Repo) UpdateSettings(ctx context.Context, s models.Settings) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE settings SET cursor_api_key=$1,cursor_repository=$2,smtp_host=$3,smtp_port=$4,smtp_user=$5,smtp_password=$6,smtp_from=$7,smtp_tls=$8,processing_paused=$9,keep_runs_days=$10 WHERE id=1`,
		s.CursorAPIKey, s.CursorRepository, s.SMTPHost, s.SMTPPort, s.SMTPUser, s.SMTPPassword, s.SMTPFrom, s.SMTPTLS, s.ProcessingPaused, s.KeepRunsDays)
	return err
}
```

In the same file, replace `ListRuns` and the head of `GetRun` so both scan the mail columns:

```go
func (r *Repo) ListRuns(ctx context.Context) ([]models.DigestRun, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id,digest_id,digest_name,analyzed_sources,processed_at,period_from,period_to,status,COALESCE(error,''),mail_status,mail_error FROM digest_runs ORDER BY processed_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.DigestRun{}
	for rows.Next() {
		var run models.DigestRun
		var analyzed []byte
		if err := rows.Scan(&run.ID, &run.DigestID, &run.DigestName, &analyzed, &run.ProcessedAt, &run.PeriodFrom, &run.PeriodTo, &run.Status, &run.Error, &run.MailStatus, &run.MailError); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(analyzed, &run.AnalyzedSources)
		out = append(out, run)
	}
	return out, nil
}
```

In `GetRun`, replace the first query and `Scan` with:

```go
	err := r.Pool.QueryRow(ctx,
		`SELECT id,digest_id,digest_name,analyzed_sources,processed_at,period_from,period_to,html,status,COALESCE(error,''),mail_status,mail_error FROM digest_runs WHERE id=$1`, id).
		Scan(&run.ID, &run.DigestID, &run.DigestName, &analyzed, &run.ProcessedAt, &run.PeriodFrom, &run.PeriodTo, &run.HTML, &run.Status, &run.Error, &run.MailStatus, &run.MailError)
```

(The rest of `GetRun` — materials loading — stays unchanged.)

- [ ] **Step 4: Verify compile and tests**

Run: `go build ./... && go test ./...`
Expected: build exits 0; existing tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/db/migrations/005_reliability.sql internal/models/models.go internal/repo/repo.go
git commit -m "feat(db): mail status columns, keep_runs_days setting, run indexes"
```

---

## Task 2: Async manual runs and honest trigger queue

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Modify: `cmd/server/main.go:58` (constructor call)
- Modify: `internal/handlers/handlers.go` (`triggerDigest`, `createDigest`)
- Create: `internal/scheduler/scheduler_test.go`
- Modify: `internal/handlers/handlers_test.go`

- [ ] **Step 1: Write the failing scheduler test**

Create `internal/scheduler/scheduler_test.go`:

```go
package scheduler

import "testing"

func TestTriggerReportsFullQueue(t *testing.T) {
	s := New(nil, nil, "")
	for i := 0; i < 32; i++ {
		if !s.Trigger("some-id") {
			t.Fatalf("trigger %d rejected before the queue is full", i)
		}
	}
	if s.Trigger("some-id") {
		t.Fatal("trigger accepted on a full queue")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/scheduler/ -run TestTriggerReportsFullQueue -v`
Expected: FAIL — compile error (`New` takes 2 args, `Trigger` returns nothing).

- [ ] **Step 3: Update the scheduler**

In `internal/scheduler/scheduler.go`, replace the struct, `New`, `Trigger`, and the `loop` trigger case:

```go
type Scheduler struct {
	Repo        *repo.Repo
	Processor   *processor.Processor
	ImagesDir   string
	trigger     chan string
	lastCleanup time.Time
}

func New(r *repo.Repo, p *processor.Processor, imagesDir string) *Scheduler {
	return &Scheduler{Repo: r, Processor: p, ImagesDir: imagesDir, trigger: make(chan string, 32)}
}

// Trigger queues a manual run. It reports false when the queue is full so the
// caller can tell the user instead of silently dropping the run.
func (s *Scheduler) Trigger(digestID string) bool {
	select {
	case s.trigger <- digestID:
		return true
	default:
		return false
	}
}
```

In `loop`, change the trigger case so manual runs no longer block the scheduler (a Cursor run takes up to 50 minutes):

```go
		case id := <-s.trigger:
			go s.runOne(ctx, id, false)
```

(`ImagesDir` and `lastCleanup` are used by Task 7; Go does not complain about unused struct fields.)

In `cmd/server/main.go`, update the constructor call:

```go
	sch := scheduler.New(r, p, imagesDir)
```

- [ ] **Step 4: Run the scheduler test to verify it passes**

Run: `go test ./internal/scheduler/ -run TestTriggerReportsFullQueue -v`
Expected: PASS

- [ ] **Step 5: Write the failing handler test**

Append to `internal/handlers/handlers_test.go` (extend the import block: `net/http/httptest`, `newsanalyzer/internal/scheduler`):

```go
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
```

Run: `go test ./internal/handlers/ -run TestTriggerDigestQueueFull -v`
Expected: FAIL — `triggerDigest` still returns 202.

- [ ] **Step 6: Update the handlers**

In `internal/handlers/handlers.go`, replace `triggerDigest`:

```go
func (h *Handlers) triggerDigest(w http.ResponseWriter, r *http.Request) {
	if !h.Sched.Trigger(r.PathValue("id")) {
		writeErr(w, 503, "очередь запусков переполнена, попробуйте позже")
		return
	}
	writeJSON(w, 202, map[string]string{"status": "queued"})
}
```

In `createDigest`, replace the post-create trigger block. Creation must not fail because of a full queue — a fresh digest has `next_run_at IS NULL`, so the next scheduler tick fires it anyway:

```go
	if created.Enabled && !h.Sched.Trigger(created.ID) {
		log.Printf("create digest %s: trigger queue full, scheduler tick will pick it up", created.ID)
	}
```

Add `"log"` to the handlers import block.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/... && go build ./...`
Expected: all PASS, build ok.

- [ ] **Step 8: Commit**

```bash
git add internal/scheduler/ internal/handlers/ cmd/server/main.go
git commit -m "fix(scheduler): async manual runs, honest full-queue reporting"
```

---

## Task 3: Mark stale `processing` runs as failed on startup

**Files:**
- Modify: `internal/repo/repo.go`, `cmd/server/main.go`

- [ ] **Step 1: Add the repo method**

In `internal/repo/repo.go` (Runs section):

```go
// FailStaleProcessing marks runs stuck in 'processing' (typically after a
// server restart mid-run) as failed. Returns the number of affected rows.
func (r *Repo) FailStaleProcessing(ctx context.Context) (int64, error) {
	tag, err := r.Pool.Exec(ctx,
		`UPDATE digest_runs SET status='error', error='прерван перезапуском сервера', processed_at=now() WHERE status='processing'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
```

- [ ] **Step 2: Call it on boot**

In `cmd/server/main.go`, right after `r := repo.New(database.Pool)`:

```go
	if n, err := r.FailStaleProcessing(ctx); err != nil {
		log.Printf("fail stale runs: %v", err)
	} else if n > 0 {
		log.Printf("marked %d stale processing runs as error", n)
	}
```

- [ ] **Step 3: Verify compile**

Run: `go build ./...`
Expected: exit 0. (This is plain SQL against a live DB; it is covered by the manual smoke in Task 9 — with Postgres running, insert a `processing` row via psql, start the server, confirm the log line and the `error` status.)

- [ ] **Step 4: Commit**

```bash
git add internal/repo/repo.go cmd/server/main.go
git commit -m "fix(processor): recover stale processing runs on startup"
```

---

## Task 4: Mail delivery status with retries

**Files:**
- Create: `internal/processor/retry.go`, `internal/processor/retry_test.go`
- Modify: `internal/repo/repo.go` (`SetRunMail`), `internal/processor/processor.go`

- [ ] **Step 1: Write the failing retry test**

Create `internal/processor/retry_test.go`:

```go
package processor

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetrySucceedsAfterFailures(t *testing.T) {
	calls := 0
	err := retry(context.Background(), []time.Duration{0, time.Millisecond, time.Millisecond}, func() error {
		calls++
		if calls < 3 {
			return errors.New("boom")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d, want nil and 3", err, calls)
	}
}

func TestRetryReturnsLastError(t *testing.T) {
	calls := 0
	err := retry(context.Background(), []time.Duration{0, time.Millisecond}, func() error {
		calls++
		return errors.New("always")
	})
	if err == nil || err.Error() != "always" || calls != 2 {
		t.Fatalf("err=%v calls=%d, want 'always' and 2", err, calls)
	}
}

func TestRetryStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := retry(ctx, []time.Duration{0, time.Hour}, func() error {
		calls++
		return errors.New("fail")
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want context.Canceled and 1", err, calls)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/processor/ -run TestRetry -v`
Expected: FAIL — `retry` undefined.

- [ ] **Step 3: Implement the helper**

Create `internal/processor/retry.go`:

```go
package processor

import (
	"context"
	"time"
)

// retry runs fn once per element of delays, waiting delays[i] before attempt
// i (delays[0] is usually 0). It returns nil on the first success, the last
// error otherwise, and ctx.Err() if the context is cancelled while waiting.
func retry(ctx context.Context, delays []time.Duration, fn func() error) error {
	var last error
	for _, d := range delays {
		if d > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		if last = fn(); last == nil {
			return nil
		}
	}
	return last
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/processor/ -run TestRetry -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Add `SetRunMail` to repo**

In `internal/repo/repo.go` (Runs section):

```go
func (r *Repo) SetRunMail(ctx context.Context, runID, status, errText string) error {
	_, err := r.Pool.Exec(ctx, `UPDATE digest_runs SET mail_status=$2, mail_error=$3 WHERE id=$1`, runID, status, errText)
	return err
}
```

- [ ] **Step 6: Replace `sendMail` with a recording method**

In `internal/processor/processor.go`, delete the package-level `sendMail` function and add this method:

```go
// sendMailAndRecord sends the digest email with retries and records the
// delivery outcome on the run so the UI can surface failures.
func (p *Processor) sendMailAndRecord(ctx context.Context, d models.Digest, run models.DigestRun, s models.Settings) {
	if len(d.Recipients) == 0 || s.SMTPHost == "" {
		if err := p.Repo.SetRunMail(ctx, run.ID, "skipped", ""); err != nil {
			log.Printf("set mail status: %v", err)
		}
		return
	}
	m := mailer.New(s)
	err := retry(ctx, []time.Duration{0, 5 * time.Second, 30 * time.Second}, func() error {
		err := m.Send(d.Recipients, d.Name, run.HTML)
		if err != nil {
			log.Printf("send mail (digest %s): %v", d.ID, err)
		}
		return err
	})
	status, errText := "sent", ""
	if err != nil {
		status, errText = "failed", err.Error()
	}
	if err := p.Repo.SetRunMail(ctx, run.ID, status, errText); err != nil {
		log.Printf("set mail status: %v", err)
	}
}
```

Replace both call sites in `Run` (`sendMail(d, run, settings)` — one in the empty-materials branch, one at the end) with:

```go
	p.sendMailAndRecord(ctx, d, run, settings)
```

- [ ] **Step 7: Run tests and build**

Run: `go test ./internal/... && go build ./...`
Expected: PASS, build ok.

- [ ] **Step 8: Commit**

```bash
git add internal/processor/ internal/repo/repo.go
git commit -m "feat(processor): record mail delivery status with retries"
```

---

## Task 5: Image download limits and SSRF guard

**Files:**
- Create: `internal/images/guard.go`, `internal/images/guard_test.go`, `internal/images/fetch_test.go`
- Modify: `internal/images/images.go`

- [ ] **Step 1: Write the failing guard tests**

Create `internal/images/guard_test.go`:

```go
package images

import (
	"net"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", false},
		{"10.1.2.3", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"169.254.10.10", false},
		{"224.0.0.1", false},
		{"0.0.0.0", false},
		{"::1", false},
		{"fe80::1", false},
		{"fc00::1", false},
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"2606:4700:4700::1111", true},
	}
	for _, c := range cases {
		if got := isPublicIP(net.ParseIP(c.ip)); got != c.want {
			t.Errorf("isPublicIP(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
	if isPublicIP(nil) {
		t.Error("isPublicIP(nil) = true, want false")
	}
}

func TestAllowedURL(t *testing.T) {
	for _, ok := range []string{"http://example.com/a.jpg", "https://example.com/a.jpg"} {
		if err := allowedURL(ok); err != nil {
			t.Errorf("allowedURL(%s) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"ftp://example.com/a", "file:///etc/passwd", "data:image/png;base64,x", "://bad"} {
		if err := allowedURL(bad); err == nil {
			t.Errorf("allowedURL(%s) = nil, want error", bad)
		}
	}
}
```

Run: `go test ./internal/images/ -run "TestIsPublicIP|TestAllowedURL" -v`
Expected: FAIL — `isPublicIP`, `allowedURL` undefined.

- [ ] **Step 2: Implement the guard**

Create `internal/images/guard.go`:

```go
package images

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// allowedURL permits only http/https URLs. Image URLs come from the agent
// response, so every other scheme (file:, data:, ftp:, ...) is rejected.
func allowedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("bad url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q is not allowed", u.Scheme)
	}
	return nil
}

// isPublicIP reports whether ip is a routable public address. Loopback,
// private, link-local, multicast and unspecified addresses are rejected to
// prevent SSRF via agent-supplied URLs.
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	return true
}

// newSafeClient returns an http.Client that resolves every host itself and
// refuses to connect to non-public addresses. The check runs per connection,
// so redirects are covered too.
func newSafeClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !isPublicIP(ip.IP) {
					return nil, fmt.Errorf("blocked non-public address %s for host %s", ip.IP, host)
				}
			}
			// Dial the address we just validated, not the hostname, so a
			// second DNS resolution cannot return a different IP.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}
```

Run: `go test ./internal/images/ -run "TestIsPublicIP|TestAllowedURL" -v`
Expected: PASS.

- [ ] **Step 3: Write the failing Fetch limit tests**

Create `internal/images/fetch_test.go`. These tests inject a plain HTTP client so the httptest server on 127.0.0.1 is reachable (the SSRF guard itself is covered by the unit tests above):

```go
package images

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchRejectsBlockedScheme(t *testing.T) {
	f := &Fetcher{Dir: t.TempDir(), HTTP: http.DefaultClient}
	_, _, err := f.Fetch(context.Background(), "run1", "file:///etc/passwd")
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v, want scheme error", err)
	}
}

func TestFetchRejectsNonImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()
	f := &Fetcher{Dir: t.TempDir(), HTTP: srv.Client()}
	_, _, err := f.Fetch(context.Background(), "run1", srv.URL+"/a.jpg")
	if err == nil || !strings.Contains(err.Error(), "content type") {
		t.Fatalf("err = %v, want content type error", err)
	}
}

func TestFetchRejectsOversized(t *testing.T) {
	big := bytes.Repeat([]byte{0}, maxImageBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(big)
	}))
	defer srv.Close()
	dir := t.TempDir()
	f := &Fetcher{Dir: dir, HTTP: srv.Client()}
	_, _, err := f.Fetch(context.Background(), "run1", srv.URL+"/big.png")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want size error", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "run1"))
	if len(entries) != 0 {
		t.Fatalf("oversized download left %d files on disk", len(entries))
	}
}
```

Run: `go test ./internal/images/ -run TestFetch -v`
Expected: FAIL — `maxImageBytes` undefined, no content-type/size checks yet.

- [ ] **Step 4: Harden `Fetch` and `ResolveArticleImage`**

In `internal/images/images.go`, change `New` to use the safe client:

```go
func New(dir, publicBase string) *Fetcher {
	return &Fetcher{Dir: dir, PublicBase: publicBase, HTTP: newSafeClient(30 * time.Second)}
}
```

Replace the whole `Fetch` function (note: the old `defer out.Close()` is gone — the file must be closed before `os.Remove` works on Windows):

```go
const maxImageBytes = 10 << 20 // 10 MB

// Fetch downloads the image URL into <Dir>/<runID>/<hash>.<ext>.
// Returns the local filesystem path and the public URL path (e.g. /images/<runID>/<hash>.<ext>).
func (f *Fetcher) Fetch(ctx context.Context, runID, url string) (string, string, error) {
	if url == "" {
		return "", "", nil
	}
	if err := allowedURL(url); err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("image %s: %d", url, resp.StatusCode)
	}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if !strings.HasPrefix(ct, "image/") {
		return "", "", fmt.Errorf("image %s: unexpected content type %q", url, ct)
	}
	ext := extFromContentType(ct)
	if ext == "" {
		ext = extFromURL(url)
	}
	if ext == "" {
		ext = ".jpg"
	}
	sum := sha1.Sum([]byte(url))
	name := hex.EncodeToString(sum[:]) + ext
	dir := filepath.Join(f.Dir, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	full := filepath.Join(dir, name)
	out, err := os.Create(full)
	if err != nil {
		return "", "", err
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, maxImageBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(full)
		return "", "", err
	}
	if n > maxImageBytes {
		_ = os.Remove(full)
		return "", "", fmt.Errorf("image %s exceeds %d bytes", url, maxImageBytes)
	}
	public := strings.TrimRight(f.PublicBase, "/") + "/images/" + runID + "/" + name
	return full, public, nil
}
```

(`extFromContentType(ct)` now receives the already-normalized `ct` string — its own normalization is harmless.)

In `ResolveArticleImage`, add the scheme check right after the empty check:

```go
	if articleURL == "" {
		return "", nil
	}
	if err := allowedURL(articleURL); err != nil {
		return "", err
	}
```

- [ ] **Step 5: Run all images tests**

Run: `go test ./internal/images/ -v`
Expected: PASS (guard + fetch tests).

- [ ] **Step 6: Commit**

```bash
git add internal/images/
git commit -m "feat(images): SSRF guard, 10MB size limit, content-type check"
```

---

## Task 6: Signed short-lived view links

**Files:**
- Create: `internal/handlers/viewlink.go`, `internal/handlers/viewlink_test.go`
- Modify: `internal/handlers/handlers.go` (route, `viewRun`), `internal/handlers/web/app.js` (open button)

- [ ] **Step 1: Write the failing tests**

Create `internal/handlers/viewlink_test.go`:

```go
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
```

Run: `go test ./internal/handlers/ -run "TestVerifyViewLink|TestRunViewLinkRoundTrip" -v`
Expected: FAIL — functions undefined.

- [ ] **Step 2: Implement view links**

Create `internal/handlers/viewlink.go`:

```go
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const viewLinkTTL = 15 * time.Minute

func viewLinkSig(secret []byte, runID string, exp int64) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%s|%d", runID, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyViewLink(secret []byte, runID, expStr, sig string, now time.Time) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || now.Unix() > exp {
		return false
	}
	want := viewLinkSig(secret, runID, exp)
	return hmac.Equal([]byte(want), []byte(sig))
}

// runViewLink issues a short-lived signed URL for /runs/{id}/view. The run's
// existence is not checked: signing an unknown id is harmless, /view 404s.
func (h *Handlers) runViewLink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	exp := time.Now().Add(viewLinkTTL).Unix()
	writeJSON(w, 200, map[string]string{
		"url": fmt.Sprintf("/runs/%s/view?exp=%d&sig=%s", id, exp, viewLinkSig(h.Auth.Secret, id, exp)),
	})
}
```

In `internal/handlers/handlers.go`:

Register the route in `Mux()` next to the other run routes:

```go
	protected.HandleFunc("GET /api/runs/{id}/view-link", h.runViewLink)
```

Guard `viewRun` (the route stays outside the auth middleware — the browser tab cannot send a Bearer header):

```go
func (h *Handlers) viewRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	if !verifyViewLink(h.Auth.Secret, id, q.Get("exp"), q.Get("sig"), time.Now()) {
		http.Error(w, "not found", 404)
		return
	}
	run, err := h.Repo.GetRun(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(run.HTML))
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/handlers/ -v`
Expected: PASS.

- [ ] **Step 4: Keep the current UI working**

The old frontend links to `/runs/{id}/view` directly, which is now signed-only. In `internal/handlers/web/app.js` (`renderRuns`), replace the actions line:

```js
        <div class="actions"><a target="_blank" href="/runs/${r.id}/view"><button class="secondary">Открыть</button></a></div>`;
```

with:

```js
        <div class="actions"><button class="secondary" data-act="open">Открыть</button></div>`;
```

and add the handler right after `el.innerHTML = ...` (before `$('#rlist').appendChild(el);`). The tab opens synchronously in the click handler so popup blockers do not interfere:

```js
      el.querySelector('[data-act=open]').onclick = async () => {
        const win = window.open('about:blank', '_blank');
        try {
          const j = await api('/api/runs/' + r.id + '/view-link');
          win.location = j.url;
        } catch (e) {
          if (win) win.close();
          alert(e.message);
        }
      };
```

- [ ] **Step 5: Build and commit**

Run: `go build ./... && go test ./...`
Expected: ok.

```bash
git add internal/handlers/
git commit -m "feat(api): signed short-lived links for run view"
```

---

## Task 7: Daily retention cleanup and token pruning

**Files:**
- Modify: `internal/repo/repo.go`, `internal/scheduler/scheduler.go`, `internal/handlers/handlers.go`, `internal/handlers/web/app.js`

- [ ] **Step 1: Add cleanup queries to repo**

In `internal/repo/repo.go`, add a Cleanup section before `orEmpty`:

```go
// -------- Cleanup --------

func (r *Repo) ListOldRunIDs(ctx context.Context, before time.Time) ([]string, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id FROM digest_runs WHERE processed_at < $1`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (r *Repo) DeleteRunsByID(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.Pool.Exec(ctx, `DELETE FROM digest_runs WHERE id = ANY($1)`, ids)
	return err
}

func (r *Repo) DeleteExpiredRefresh(ctx context.Context) (int64, error) {
	tag, err := r.Pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
```

(Materials are removed by the existing `ON DELETE CASCADE`.)

- [ ] **Step 2: Add the daily cleanup to the scheduler**

In `internal/scheduler/scheduler.go`, add `"os"` and `"path/filepath"` to imports. In `tick`, insert the cleanup between `GetSettings` and the `ProcessingPaused` check (retention is a time-based policy and must run even while processing is paused):

```go
	if time.Since(s.lastCleanup) >= 24*time.Hour {
		s.cleanup(ctx, settings.KeepRunsDays)
		s.lastCleanup = time.Now()
	}
```

Add the method:

```go
// cleanup prunes expired refresh tokens and, when retention is configured,
// deletes runs older than keepDays together with their image directories.
func (s *Scheduler) cleanup(ctx context.Context, keepDays int) {
	if n, err := s.Repo.DeleteExpiredRefresh(ctx); err != nil {
		log.Printf("cleanup refresh tokens: %v", err)
	} else if n > 0 {
		log.Printf("cleanup: deleted %d expired refresh tokens", n)
	}
	if keepDays <= 0 {
		return
	}
	before := time.Now().UTC().AddDate(0, 0, -keepDays)
	ids, err := s.Repo.ListOldRunIDs(ctx, before)
	if err != nil {
		log.Printf("cleanup list old runs: %v", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	if err := s.Repo.DeleteRunsByID(ctx, ids); err != nil {
		log.Printf("cleanup delete runs: %v", err)
		return
	}
	for _, id := range ids {
		if err := os.RemoveAll(filepath.Join(s.ImagesDir, id)); err != nil {
			log.Printf("cleanup images %s: %v", id, err)
		}
	}
	log.Printf("cleanup: deleted %d runs older than %d days", len(ids), keepDays)
}
```

Note: `s.lastCleanup` starts at the zero value, so the first tick after boot always runs a cleanup.

- [ ] **Step 3: Validate the setting in handlers**

In `internal/handlers/handlers.go` `updateSettings`, after the decode and mask-handling blocks, add:

```go
	if s.KeepRunsDays < 0 {
		writeErr(w, 400, "keep_runs_days must be >= 0")
		return
	}
```

- [ ] **Step 4: Preserve the value in the old frontend**

The settings form in `internal/handlers/web/app.js` PUTs a hand-built payload; without this line every save would reset retention to 0. In `renderSettings`, `form.onsubmit`, add to the `payload` object:

```js
      keep_runs_days: s.keep_runs_days || 0,
```

(The pause-toggle button already round-trips the full settings object and needs no change. The editable UI field arrives with the Phase 3 redesign.)

- [ ] **Step 5: Build, test, commit**

Run: `go build ./... && go test ./...`
Expected: ok.

```bash
git add internal/repo/repo.go internal/scheduler/scheduler.go internal/handlers/
git commit -m "feat(scheduler): daily retention cleanup and refresh token pruning"
```

---

## Task 8: Login rate limiting

**Files:**
- Create: `internal/auth/ratelimit.go`, `internal/auth/ratelimit_test.go`
- Modify: `internal/handlers/handlers.go`, `internal/handlers/handlers_test.go`

- [ ] **Step 1: Write the failing limiter test**

Create `internal/auth/ratelimit_test.go`:

```go
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
```

Run: `go test ./internal/auth/ -run TestLoginLimiter -v`
Expected: FAIL — `NewLoginLimiter` undefined.

- [ ] **Step 2: Implement the limiter**

Create `internal/auth/ratelimit.go`:

```go
package auth

import (
	"sync"
	"time"
)

const (
	limiterThreshold  = 5
	limiterBaseBlock  = time.Minute
	limiterMaxBlock   = 15 * time.Minute
	limiterStaleAfter = 30 * time.Minute
)

type limiterEntry struct {
	fails        int
	blockDur     time.Duration
	blockedUntil time.Time
	lastFail     time.Time
}

// LoginLimiter is an in-memory brute-force limiter keyed by "IP|email".
// After limiterThreshold consecutive failures the key is blocked for
// limiterBaseBlock; every further failure doubles the block up to
// limiterMaxBlock. A successful login resets the key.
type LoginLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	now     func() time.Time
}

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{entries: map[string]*limiterEntry{}, now: time.Now}
}

// Allowed reports whether an attempt may proceed; when blocked it also
// returns the remaining block duration.
func (l *LoginLimiter) Allowed(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gc()
	e := l.entries[key]
	if e == nil {
		return true, 0
	}
	if now := l.now(); now.Before(e.blockedUntil) {
		return false, e.blockedUntil.Sub(now)
	}
	return true, 0
}

func (l *LoginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil {
		e = &limiterEntry{}
		l.entries[key] = e
	}
	e.fails++
	e.lastFail = l.now()
	if e.fails < limiterThreshold {
		return
	}
	if e.blockDur == 0 {
		e.blockDur = limiterBaseBlock
	} else {
		e.blockDur *= 2
		if e.blockDur > limiterMaxBlock {
			e.blockDur = limiterMaxBlock
		}
	}
	e.blockedUntil = l.now().Add(e.blockDur)
}

func (l *LoginLimiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// gc drops entries that are unblocked and stale. Caller must hold l.mu.
func (l *LoginLimiter) gc() {
	now := l.now()
	for k, e := range l.entries {
		if now.After(e.blockedUntil) && now.Sub(e.lastFail) > limiterStaleAfter {
			delete(l.entries, k)
		}
	}
}
```

Run: `go test ./internal/auth/ -v`
Expected: PASS.

- [ ] **Step 3: Write the failing handler test**

Append to `internal/handlers/handlers_test.go` (extend imports: `strings`, `newsanalyzer/internal/auth`):

```go
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
```

Run: `go test ./internal/handlers/ -run TestLoginRateLimited -v`
Expected: FAIL — `Handlers.Limiter` undefined.

- [ ] **Step 4: Wire the limiter into login**

In `internal/handlers/handlers.go`:

Add the field and initialize it in `New` (add `"net"` and `"fmt"` to imports if missing):

```go
type Handlers struct {
	Repo      *repo.Repo
	Auth      *auth.Auth
	Sched     *scheduler.Scheduler
	Processor *processor.Processor
	StorageFS http.Handler
	Limiter   *auth.LoginLimiter
}

func New(r *repo.Repo, a *auth.Auth, s *scheduler.Scheduler, p *processor.Processor, imagesDir string) *Handlers {
	return &Handlers{
		Repo: r, Auth: a, Sched: s, Processor: p,
		StorageFS: http.StripPrefix("/images/", http.FileServer(http.Dir(imagesDir))),
		Limiter:   auth.NewLoginLimiter(),
	}
}
```

Add the helper (X-Forwarded-For is deliberately not trusted; behind a reverse proxy the email half of the key keeps per-account limiting meaningful):

```go
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
```

Replace `login`:

```go
func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	var in loginReq
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	key := clientIP(r) + "|" + email
	if ok, wait := h.Limiter.Allowed(key); !ok {
		writeErr(w, 429, fmt.Sprintf("слишком много попыток входа, повторите через %d с", int(wait.Seconds())+1))
		return
	}
	u, err := h.Repo.GetUserByEmail(r.Context(), email)
	if err != nil || !auth.CheckPassword(u.PasswordHash, in.Password) {
		h.Limiter.Fail(key)
		writeErr(w, 401, "invalid credentials")
		return
	}
	h.Limiter.Success(key)
	access, _ := h.Auth.SignAccess(u.ID)
	refresh, err := h.Auth.NewRefresh(r.Context(), u.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"access": access, "refresh": refresh, "user": u})
}
```

- [ ] **Step 5: Run tests and commit**

Run: `go test ./... && go build ./...`
Expected: PASS.

```bash
git add internal/auth/ internal/handlers/
git commit -m "feat(auth): in-memory login rate limiting"
```

---

## Task 9: Final verification

- [ ] **Step 1: Full check**

Run: `gofmt -l ./cmd ./internal` (expected: no output), `go vet ./...` (expected: no findings), `go test ./...` (expected: all PASS).

- [ ] **Step 2: Manual smoke (requires Docker)**

```bash
docker compose up -d
go run ./cmd/server
```

Checklist:
1. Boot log shows migrations applied without error (new columns/indexes are idempotent on re-run).
2. Insert a fake stuck run, restart the server, confirm recovery:
   `docker compose exec -T db psql -U postgres -d newsanalyzer -c "INSERT INTO digest_runs(digest_id,digest_name,period_from,period_to,html,status) SELECT id,name,now(),now(),'','processing' FROM digests LIMIT 1;"`
   (requires at least one digest; adjust user/db names to docker-compose.yml). Restart `go run ./cmd/server` → log line `marked 1 stale processing runs as error`.
3. In the UI: «Запустить» on a digest → «Поставлено в очередь»; «Открыть» in История opens the run view in a new tab (signed URL with `exp`/`sig` in the address bar); editing the URL's `sig` yields «not found».
4. 6 failed logins in a row → the 6th returns «слишком много попыток входа…».

- [ ] **Step 3: Fix anything found, re-run tests, commit fixes**

```bash
git add -A && git commit -m "fix: phase 1 smoke findings"
```

(Skip the commit if nothing was found.)
