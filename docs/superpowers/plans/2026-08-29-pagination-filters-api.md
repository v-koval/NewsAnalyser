# Pagination & Filters API Implementation Plan (Phase 2 of 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** List endpoints return `{items, total}` envelopes with `page`/`per_page` pagination; `/api/runs` filters by `digest_id` and `status` and reports `materials_count`; a lightweight `/api/digests/options` feeds the filter dropdown.

**Architecture:** Repo gains paged query methods (`ListDigestsPage`, `ListRunsPage` with a `RunsFilter`, `ListDigestOptions`); the old unpaged `ListRuns` is removed (`ListDigests` stays — the scheduler iterates all digests). Handlers parse and validate query params (400 on garbage) and wrap results in a generic envelope. The current frontend gets a minimal bridge (`?per_page=100`, read `.items`) so the UI keeps working until the Phase 3 redesign.

**Tech Stack:** Go 1.22 (net/http 1.22 routing, pgx/v5), PostgreSQL 16. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-29-service-improvements-design.md` (section 1).

**Order:** Requires Phase 1 (`2026-08-29-reliability-hardening.md`) — mail columns must exist. Execute before Phase 3 (`2026-08-29-frontend-redesign.md`).

---

## File Structure

**Modify:**
- `internal/models/models.go` — `DigestRun.MaterialsCount`, new `DigestOption`
- `internal/repo/repo.go` — `ListDigestsPage`, `ListDigestOptions`, `RunsFilter` + `ListRunsPage`; remove `ListRuns`
- `internal/handlers/handlers.go` — param parsing, envelope, rewritten `listDigests`/`listRuns`, `digestOptions` route
- `internal/handlers/handlers_test.go` — param validation tests
- `internal/handlers/web/app.js` — bridge to the new response shape

---

## Task 1: Models, paged repo queries, handlers

Removing the old `ListRuns` breaks `handlers.listRuns`, so repo and handlers change within one task — the tree compiles and commits once, at the end of the task.

**Files:**
- Modify: `internal/models/models.go`, `internal/repo/repo.go`, `internal/handlers/handlers.go`, `internal/handlers/handlers_test.go`

- [ ] **Step 1: Add model fields**

In `internal/models/models.go`, add `MaterialsCount` to `DigestRun` after `MailError`:

```go
	MailStatus      string     `json:"mail_status"`
	MailError       string     `json:"mail_error,omitempty"`
	MaterialsCount  int        `json:"materials_count"`
```

And add the option type at the end of the file:

```go
// DigestOption is a lightweight digest reference for filter dropdowns.
type DigestOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
```

- [ ] **Step 2: Add paged digest queries**

In `internal/repo/repo.go`, after `ListDigests` (which stays — the scheduler needs the full list), add:

```go
func (r *Repo) ListDigestsPage(ctx context.Context, limit, offset int) ([]models.Digest, int, error) {
	var total int
	if err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM digests`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT `+digestCols+` FROM digests ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Digest{}
	for rows.Next() {
		d, err := scanDigest(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, nil
}

func (r *Repo) ListDigestOptions(ctx context.Context) ([]models.DigestOption, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id,name FROM digests ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.DigestOption{}
	for rows.Next() {
		var o models.DigestOption
		if err := rows.Scan(&o.ID, &o.Name); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}
```

- [ ] **Step 3: Replace `ListRuns` with a filtered paged query**

In `internal/repo/repo.go`, delete the whole `ListRuns` function and add instead (add `"fmt"` and `"strings"` to the repo import block):

```go
// RunsFilter narrows and pages the run listing. Empty string fields are
// ignored; Status values are validated by the handler.
type RunsFilter struct {
	DigestID string
	Status   string
	Limit    int
	Offset   int
}

func (r *Repo) ListRunsPage(ctx context.Context, f RunsFilter) ([]models.DigestRun, int, error) {
	where := " WHERE 1=1"
	args := []any{}
	if f.DigestID != "" {
		args = append(args, f.DigestID)
		where += fmt.Sprintf(" AND digest_id=$%d", len(args))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where += fmt.Sprintf(" AND status=$%d", len(args))
	}
	var total int
	if err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM digest_runs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	q := `SELECT id,digest_id,digest_name,analyzed_sources,processed_at,period_from,period_to,status,COALESCE(error,''),mail_status,mail_error,
	(SELECT COUNT(*) FROM digest_materials m WHERE m.run_id = digest_runs.id)
	FROM digest_runs` + where + fmt.Sprintf(` ORDER BY processed_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := r.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.DigestRun{}
	for rows.Next() {
		var run models.DigestRun
		var analyzed []byte
		if err := rows.Scan(&run.ID, &run.DigestID, &run.DigestName, &analyzed, &run.ProcessedAt, &run.PeriodFrom, &run.PeriodTo, &run.Status, &run.Error, &run.MailStatus, &run.MailError, &run.MaterialsCount); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(analyzed, &run.AnalyzedSources)
		out = append(out, run)
	}
	return out, total, nil
}
```

- [ ] **Step 4: Write the failing handler tests**

Append to `internal/handlers/handlers_test.go`:

```go
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
```

Run: `go test ./internal/handlers/ -run "TestPageParams|TestListRunsRejectsBadParams" -v`
Expected: FAIL — `pageParams` undefined (and the package does not compile yet: `listRuns` still calls the removed `ListRuns`).

- [ ] **Step 5: Implement parsing, envelope and handlers**

In `internal/handlers/handlers.go`, extend imports with `"errors"`, `"regexp"`, `"strconv"`, and `"newsanalyzer/internal/repo"` is already imported. Add to the helpers section:

```go
type listResponse[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

var errBadPageParams = errors.New("invalid page/per_page")

// pageParams parses ?page and ?per_page (defaults 1 and 20, per_page 1..100)
// into a LIMIT/OFFSET pair.
func pageParams(r *http.Request) (limit, offset int, err error) {
	page, per := 1, 20
	q := r.URL.Query()
	if v := q.Get("page"); v != "" {
		page, err = strconv.Atoi(v)
		if err != nil || page < 1 {
			return 0, 0, errBadPageParams
		}
	}
	if v := q.Get("per_page"); v != "" {
		per, err = strconv.Atoi(v)
		if err != nil || per < 1 || per > 100 {
			return 0, 0, errBadPageParams
		}
	}
	return per, (page - 1) * per, nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var runStatuses = map[string]bool{"ok": true, "error": true, "empty": true, "processing": true}
```

Replace `listDigests` and `listRuns`:

```go
func (h *Handlers) listDigests(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pageParams(r)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	items, total, err := h.Repo.ListDigestsPage(r.Context(), limit, offset)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, listResponse[models.Digest]{Items: items, Total: total})
}

func (h *Handlers) listRuns(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pageParams(r)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	f := repo.RunsFilter{Limit: limit, Offset: offset}
	q := r.URL.Query()
	if v := q.Get("digest_id"); v != "" {
		if !uuidRe.MatchString(v) {
			writeErr(w, 400, "invalid digest_id")
			return
		}
		f.DigestID = v
	}
	if v := q.Get("status"); v != "" {
		if !runStatuses[v] {
			writeErr(w, 400, "invalid status")
			return
		}
		f.Status = v
	}
	items, total, err := h.Repo.ListRunsPage(r.Context(), f)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, listResponse[models.DigestRun]{Items: items, Total: total})
}
```

Add the options handler:

```go
func (h *Handlers) digestOptions(w http.ResponseWriter, r *http.Request) {
	opts, err := h.Repo.ListDigestOptions(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, opts)
}
```

Register the route in `Mux()` **before** the `{id}` routes for readability (Go 1.22 picks the more specific literal pattern regardless of order):

```go
	protected.HandleFunc("GET /api/digests/options", h.digestOptions)
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/handlers/ -v && go build ./...`
Expected: all PASS, build ok.

- [ ] **Step 7: Commit**

```bash
git add internal/models/models.go internal/repo/repo.go internal/handlers/
git commit -m "feat(api): pagination envelope, run filters, digest options endpoint"
```

---

## Task 2: Frontend bridge to the new response shape

**Files:**
- Modify: `internal/handlers/web/app.js`

- [ ] **Step 1: Adapt the two list fetches**

The old UI has no pager yet; request the maximum page so behavior stays close to current (Phase 3 replaces this entirely).

In `renderDigests`, replace:

```js
    const list = await api('/api/digests');
```

with:

```js
    const list = (await api('/api/digests?per_page=100')).items;
```

In `renderRuns`, replace:

```js
    const list = await api('/api/runs');
```

with:

```js
    const list = (await api('/api/runs?per_page=100')).items;
```

- [ ] **Step 2: Verify build and behavior**

Run: `go build ./... && go test ./...`
Expected: ok.

Manual (requires Docker + Postgres): `go run ./cmd/server`, open http://localhost:8080 — «Дайджесты» and «История» render as before.

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/web/app.js
git commit -m "feat(ui): adapt list views to paginated API envelope"
```

---

## Task 3: Final verification

- [ ] **Step 1: Full check**

Run: `gofmt -l ./cmd ./internal` (no output), `go vet ./...` (clean), `go test ./...` (all PASS).

- [ ] **Step 2: API smoke (requires running server + auth token)**

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login -H 'Content-Type: application/json' -d '{"email":"admin@example.com","password":"admin"}' | python -c "import sys,json;print(json.load(sys.stdin)['access'])")
curl -s "http://localhost:8080/api/runs?page=1&per_page=5" -H "Authorization: Bearer $TOKEN"
curl -s "http://localhost:8080/api/runs?status=weird" -H "Authorization: Bearer $TOKEN"   # expect {"error":"invalid status"}
curl -s "http://localhost:8080/api/digests/options" -H "Authorization: Bearer $TOKEN"
```

Expected: first call returns `{"items":[...],"total":N}` with `materials_count` and `mail_status` on items; second returns the 400 error body; third returns `[{"id":"...","name":"..."}]`.

- [ ] **Step 3: Fix anything found, re-run tests, commit fixes if any**
