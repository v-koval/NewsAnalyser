# Stable Digest Schedule Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the per-cycle drift in digest scheduling so two consecutive runs of a `frequency_hours = N` digest are exactly `N` hours apart, regardless of processing duration or tick timing.

**Architecture:** Introduce a new `next_run_at TIMESTAMPTZ` column on `digests`. The scheduler decides whether to run based on `next_run_at` (not `last_run_at`). After each scheduled run, `next_run_at` is advanced by adding exactly `frequency` to its previous *planned* value (not to `now()`). Manual "run now" triggers do not modify `next_run_at`. Missed slots (gap ≥ frequency) are dropped — `next_run_at` jumps forward to the next future grid point without firing a run. `last_run_at` is preserved with its original "actual start time" semantics for the UI.

**Tech Stack:** Go 1.x, pgx/v5, PostgreSQL, vanilla JS frontend.

**Spec:** [docs/superpowers/specs/2026-04-27-stable-digest-schedule-design.md](../specs/2026-04-27-stable-digest-schedule-design.md)

**Note on testing:** This Go codebase has no automated tests today (`grep -r "_test.go" internal/` returns nothing). To stay consistent with the existing project conventions, this plan does not introduce a new test framework. Each task ends with a compile check (`go build ./...`, `go vet ./...`) and the final task is an end-to-end manual verification against a running database.

---

## File Structure

| File | Action | Responsibility |
| --- | --- | --- |
| `internal/db/migrations/003_add_next_run_at.sql` | create | Adds `next_run_at` column and back-fills existing rows. |
| `internal/models/models.go` | modify | Adds `NextRunAt *time.Time` to `Digest`. |
| `internal/repo/repo.go` | modify | Updates `digestCols` / `scanDigest`; adds `SetDigestNextRun`. |
| `internal/processor/processor.go` | modify | Adds `advanceSchedule` parameter to `Run`; introduces a helper that updates both `last_run_at` (always) and `next_run_at` (only when scheduled). |
| `internal/scheduler/scheduler.go` | modify | Replaces the `last_run_at`-based gate with the new `next_run_at` algorithm; passes the `advanceSchedule` flag to the processor. |
| `internal/handlers/web/app.js` | modify | Renders "Следующий запуск" in the digest list row. |

---

### Task 1: Add `next_run_at` migration

**Files:**
- Create: `internal/db/migrations/003_add_next_run_at.sql`

- [ ] **Step 1: Create the migration file**

`internal/db/migrations/003_add_next_run_at.sql`:

```sql
ALTER TABLE digests
    ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ;

UPDATE digests
SET next_run_at = COALESCE(last_run_at, now()) + (frequency_hours * interval '1 hour')
WHERE next_run_at IS NULL;
```

The `IF NOT EXISTS` and `WHERE next_run_at IS NULL` clauses make this migration idempotent — re-running it on an already-migrated database is a no-op.

- [ ] **Step 2: Verify the file is picked up by the migration runner**

Run: `grep -rn "migrations" internal/db/`

Expected: an embed/loader directive (e.g. `//go:embed migrations/*.sql`) or a sorted-by-filename loader. Confirm visually that a new `003_…` file will be applied automatically — no extra registration is needed. If a registration call is required, update it.

- [ ] **Step 3: Apply the migration locally and inspect**

Run (against a local dev DB):

```bash
psql "$DATABASE_URL" -c '\d digests'
```

Restart the application so it runs migrations, then re-run the same `\d digests` command.

Expected on the second run:

```
 next_run_at | timestamp with time zone |
```

And:

```bash
psql "$DATABASE_URL" -c 'SELECT id, last_run_at, next_run_at, frequency_hours FROM digests;'
```

Expected: every row has `next_run_at = COALESCE(last_run_at, now()) + frequency_hours * 1h`.

- [ ] **Step 4: Commit**

```bash
git add internal/db/migrations/003_add_next_run_at.sql
git commit -m "feat(db): add next_run_at column to digests"
```

---

### Task 2: Expose `NextRunAt` on the `Digest` model

**Files:**
- Modify: `internal/models/models.go` (struct `Digest`, around lines 24-38)

- [ ] **Step 1: Add the field**

Open `internal/models/models.go`. In the `Digest` struct, after `LastRunAt`, insert:

```go
	NextRunAt      *time.Time `json:"next_run_at"`
```

The full struct after editing:

```go
type Digest struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Topic          string     `json:"topic"`
	Sources        []string   `json:"sources"`
	IgnoredSources []string   `json:"ignored_sources"`
	FrequencyHours int        `json:"frequency_hours"`
	Recipients     []string   `json:"recipients"`
	Language       string     `json:"language"`
	Enabled        bool       `json:"enabled"`
	LastRunAt      *time.Time `json:"last_run_at"`
	NextRunAt      *time.Time `json:"next_run_at"`
	AutoSources    []string   `json:"auto_sources"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
```

- [ ] **Step 2: Compile**

Run: `go build ./...`

Expected: build fails inside `internal/repo` with a scan-arity mismatch (because `digestCols` and `scanDigest` haven't been updated yet). That's expected — Task 3 fixes it.

- [ ] **Step 3: Do not commit yet**

Tasks 2 and 3 form one logically coherent change (the model field + its persistence) and should be committed together at the end of Task 3.

---

### Task 3: Repo support for `next_run_at`

**Files:**
- Modify: `internal/repo/repo.go` (`digestCols` near line 138, `scanDigest` near line 124, and add a new method)

- [ ] **Step 1: Update `digestCols`**

Replace the existing constant:

```go
const digestCols = `id,name,topic,sources,ignored_sources,frequency_hours,recipients,language,enabled,last_run_at,auto_sources,created_at,updated_at`
```

With:

```go
const digestCols = `id,name,topic,sources,ignored_sources,frequency_hours,recipients,language,enabled,last_run_at,next_run_at,auto_sources,created_at,updated_at`
```

- [ ] **Step 2: Update `scanDigest`**

Replace the existing scan call:

```go
err := row.Scan(&d.ID, &d.Name, &d.Topic, &sources, &ignored, &d.FrequencyHours, &recipients, &d.Language, &d.Enabled, &d.LastRunAt, &auto, &d.CreatedAt, &d.UpdatedAt)
```

With:

```go
err := row.Scan(&d.ID, &d.Name, &d.Topic, &sources, &ignored, &d.FrequencyHours, &recipients, &d.Language, &d.Enabled, &d.LastRunAt, &d.NextRunAt, &auto, &d.CreatedAt, &d.UpdatedAt)
```

`CreateDigest` and `UpdateDigest` use `RETURNING ` + `digestCols`, so they automatically pick up the new column — no changes to their `INSERT` / `UPDATE` column lists are needed (we want `next_run_at` to remain NULL on insert, which is the default).

- [ ] **Step 3: Add `SetDigestNextRun`**

Right below the existing `SetDigestLastRun` (around line 208), add:

```go
func (r *Repo) SetDigestNextRun(ctx context.Context, id string, t time.Time) error {
	_, err := r.Pool.Exec(ctx, `UPDATE digests SET next_run_at=$2 WHERE id=$1`, id, t)
	return err
}
```

Leave `SetDigestLastRun` unchanged.

- [ ] **Step 4: Compile**

Run: `go build ./...`

Expected: PASS. (The processor and scheduler still use the old API; they don't yet call the new method, but nothing they call has changed signature, so the build should succeed.)

- [ ] **Step 5: Vet**

Run: `go vet ./...`

Expected: no warnings.

- [ ] **Step 6: Commit (Tasks 2 + 3 together)**

```bash
git add internal/models/models.go internal/repo/repo.go
git commit -m "feat(repo): persist and expose next_run_at on digests"
```

---

### Task 4: Processor advances the schedule from the planned time

**Files:**
- Modify: `internal/processor/processor.go` (`Run` signature near line 64, the four `SetDigestLastRun` call sites at lines 118, 151, 162, 206)

- [ ] **Step 1: Add the `advanceSchedule` helper**

In `internal/processor/processor.go`, just above `func (p *Processor) Run(...)` (around line 64), add:

```go
// advanceSchedule writes the actual run start time to last_run_at, and — if
// scheduled is true — advances next_run_at by exactly one frequency from its
// previous *planned* value. Manual ("run now") triggers pass scheduled=false
// so the regular schedule is not shifted by user actions.
func (p *Processor) advanceSchedule(ctx context.Context, d models.Digest, to time.Time, scheduled bool) {
	if err := p.Repo.SetDigestLastRun(ctx, d.ID, to); err != nil {
		log.Printf("set last_run_at: %v", err)
	}
	if !scheduled {
		return
	}
	freq := time.Duration(d.FrequencyHours) * time.Hour
	var next time.Time
	if d.NextRunAt != nil {
		next = d.NextRunAt.Add(freq)
	} else {
		next = to.Add(freq)
	}
	if err := p.Repo.SetDigestNextRun(ctx, d.ID, next); err != nil {
		log.Printf("set next_run_at: %v", err)
	}
}
```

- [ ] **Step 2: Change the `Run` signature**

Find the existing function header:

```go
func (p *Processor) Run(ctx context.Context, digestID string) error {
```

Replace with:

```go
func (p *Processor) Run(ctx context.Context, digestID string, scheduled bool) error {
```

- [ ] **Step 3: Replace all four `SetDigestLastRun` call sites**

Inside `Run`, there are exactly four calls to `p.Repo.SetDigestLastRun(ctx, d.ID, to)` — at the cursor-error path, the parse-error path, the empty-results path, and the success path (current lines 118, 151, 162, 206).

Replace **each** of them with:

```go
p.advanceSchedule(ctx, d, to, scheduled)
```

After the replacement, no calls to `p.Repo.SetDigestLastRun` should remain inside `Run`. Verify with:

```bash
grep -n "SetDigestLastRun" internal/processor/processor.go
```

Expected: empty output (the only remaining reference is inside the helper itself, which is above `Run`).

- [ ] **Step 4: Compile**

Run: `go build ./...`

Expected: build fails in `internal/scheduler/scheduler.go` because `s.Processor.Run(runCtx, id)` no longer matches the new 3-arg signature. That's expected — Task 5 fixes it.

- [ ] **Step 5: Do not commit yet**

Tasks 4 and 5 must compile together. Commit after Task 5.

---

### Task 5: Scheduler uses `next_run_at` and passes the `scheduled` flag

**Files:**
- Modify: `internal/scheduler/scheduler.go` (entire file)

- [ ] **Step 1: Update `runOne` to accept the `scheduled` flag**

Replace the existing `runOne`:

```go
func (s *Scheduler) runOne(ctx context.Context, id string) {
	log.Printf("runOne: starting digest %s", id)
	settings, err := s.Repo.GetSettings(ctx)
	if err != nil {
		log.Printf("runOne: get settings: %v", err)
		return
	}
	if settings.ProcessingPaused {
		log.Printf("runOne: processing paused, skipping")
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 50*time.Minute)
	defer cancel()
	if err := s.Processor.Run(runCtx, id); err != nil {
		log.Printf("run digest %s: %v", id, err)
	}
}
```

With:

```go
func (s *Scheduler) runOne(ctx context.Context, id string, scheduled bool) {
	log.Printf("runOne: starting digest %s (scheduled=%v)", id, scheduled)
	settings, err := s.Repo.GetSettings(ctx)
	if err != nil {
		log.Printf("runOne: get settings: %v", err)
		return
	}
	if settings.ProcessingPaused {
		log.Printf("runOne: processing paused, skipping")
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 50*time.Minute)
	defer cancel()
	if err := s.Processor.Run(runCtx, id, scheduled); err != nil {
		log.Printf("run digest %s: %v", id, err)
	}
}
```

- [ ] **Step 2: Update the manual-trigger call site in `loop`**

In `func (s *Scheduler) loop`, replace:

```go
		case id := <-s.trigger:
			s.runOne(ctx, id)
```

With:

```go
		case id := <-s.trigger:
			s.runOne(ctx, id, false)
```

- [ ] **Step 3: Replace the gating logic in `tick`**

Replace the entire body of the `for _, d := range digests` loop in `tick`. The current body is:

```go
		if !d.Enabled {
			continue
		}
		if d.LastRunAt != nil && now.Sub(d.LastRunAt.UTC()) < time.Duration(d.FrequencyHours)*time.Hour {
			continue
		}
		go s.runOne(ctx, d.ID)
```

Replace with:

```go
		if !d.Enabled {
			continue
		}
		freq := time.Duration(d.FrequencyHours) * time.Hour

		// First-ever run: next_run_at is NULL, fire immediately.
		if d.NextRunAt == nil {
			go s.runOne(ctx, d.ID, true)
			continue
		}

		next := d.NextRunAt.UTC()

		// Slot still in the future — wait.
		if now.Before(next) {
			continue
		}

		// Slot is more than one full frequency in the past: missed slots are
		// dropped. Jump next_run_at forward to the nearest future grid point
		// without running.
		if now.Sub(next) >= freq {
			for !next.After(now) {
				next = next.Add(freq)
			}
			if err := s.Repo.SetDigestNextRun(ctx, d.ID, next); err != nil {
				log.Printf("scheduler: jump next_run_at for digest %s: %v", d.ID, err)
			}
			continue
		}

		// Normal slot: next_run_at <= now < next_run_at + freq.
		go s.runOne(ctx, d.ID, true)
```

- [ ] **Step 4: Compile**

Run: `go build ./...`

Expected: PASS.

- [ ] **Step 5: Vet**

Run: `go vet ./...`

Expected: no warnings.

- [ ] **Step 6: Commit (Tasks 4 + 5 together)**

```bash
git add internal/processor/processor.go internal/scheduler/scheduler.go
git commit -m "feat(scheduler): drive runs from next_run_at to remove drift"
```

---

### Task 6: UI shows "Следующий запуск"

**Files:**
- Modify: `internal/handlers/web/app.js` (around line 118)

- [ ] **Step 1: Add the new line**

Find the existing line:

```javascript
          <div class="meta">Последний запуск: ${d.last_run_at ? new Date(d.last_run_at).toLocaleString() : '—'}</div>
```

Replace with the same line plus a new sibling line directly below:

```javascript
          <div class="meta">Последний запуск: ${d.last_run_at ? new Date(d.last_run_at).toLocaleString() : '—'}</div>
          <div class="meta">Следующий запуск: ${d.next_run_at ? new Date(d.next_run_at).toLocaleString() : '—'}</div>
```

- [ ] **Step 2: Manually verify in the browser**

Restart the app, open the digest list page, hard-refresh (Ctrl+F5).

Expected: each digest row shows two lines, "Последний запуск: …" and "Следующий запуск: …". For an existing digest just migrated, the next-run value equals the last-run value plus `frequency_hours`.

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/web/app.js
git commit -m "feat(ui): show next_run_at in digest list"
```

---

### Task 7: End-to-end verification

**Files:** none (manual verification with logs and DB queries)

This task validates the behaviors locked in by the spec. Use a digest with `frequency_hours = 1` so cycles complete in minutes rather than days.

- [ ] **Step 1: Steady-state — no drift across cycles**

Create a digest with `frequency_hours = 1` (or pick an existing one and temporarily set it to 1). Note its `next_run_at`:

```bash
psql "$DATABASE_URL" -c "SELECT id, last_run_at, next_run_at FROM digests WHERE id='<id>';"
```

Wait through 2-3 hourly cycles. After each cycle, re-run the query.

Expected: each new `next_run_at` is **exactly** one hour after the previous `next_run_at` (no creep, even by a second). `last_run_at` may differ from `next_run_at` by a few seconds — that is expected and is the actual processing start time.

- [ ] **Step 2: First run of a brand-new digest**

Create a fresh digest via the UI. Immediately query:

```bash
psql "$DATABASE_URL" -c "SELECT next_run_at, last_run_at FROM digests WHERE id='<new id>';"
```

Expected: `next_run_at IS NULL`, `last_run_at IS NULL`.

Wait for the next hourly tick (or restart the app to force `tick()` immediately).

Expected after the run completes: `last_run_at` ≈ run start time, `next_run_at` = `last_run_at` + `frequency_hours`.

- [ ] **Step 3: Missed slots are dropped**

With a digest at `frequency_hours = 1`, stop the app for **at least 2 hours** past its `next_run_at`. Restart.

Expected: on the first post-restart tick, the scheduler logs no run for this digest, but `next_run_at` is updated to a future time on the original grid (an exact multiple of 1h ahead of the old `next_run_at`). No catch-up run fires. Verify with:

```bash
psql "$DATABASE_URL" -c "SELECT now(), next_run_at, last_run_at FROM digests WHERE id='<id>';"
```

Expected: `next_run_at > now()`, `last_run_at` unchanged from before the outage.

- [ ] **Step 4: Manual trigger does not shift the schedule**

Pick a digest whose `next_run_at` is, say, 30 minutes in the future. Note that value.

Click "Запустить" in the UI.

After processing finishes, query:

```bash
psql "$DATABASE_URL" -c "SELECT next_run_at, last_run_at FROM digests WHERE id='<id>';"
```

Expected: `next_run_at` is **unchanged**. `last_run_at` updates to the manual run's start time.

- [ ] **Step 5: `frequency_hours` change does not move the current slot**

Pick a digest with `frequency_hours = 24` and a `next_run_at` set for, e.g., tomorrow at 12:00. Edit the digest in the UI and change `frequency_hours` to 6. Save.

Query `next_run_at` again.

Expected: `next_run_at` is **unchanged** (still tomorrow at 12:00). The 6-hour cadence kicks in only on the cycle *after* that slot — i.e. the run after tomorrow's 12:00 will set `next_run_at` to 18:00.

- [ ] **Step 6: UI displays both timestamps**

Open the digest list in a browser.

Expected: every row displays both "Последний запуск: …" and "Следующий запуск: …" with timestamps that match the database values (rendered in local time).

- [ ] **Step 7: No commit**

Verification only — nothing to commit.

---

## Self-Review Notes

Reviewed against the spec on completion:

- **Spec § "New column" + Migration** → Task 1.
- **Spec § "Models"** → Task 2.
- **Spec § "Repo changes"** → Task 3.
- **Spec § "Processor: advancing the schedule"** → Task 4.
- **Spec § "Scheduler tick algorithm"** → Task 5 Step 3.
- **Spec § "Manual trigger"** → Task 5 Step 2 (passes `scheduled=false` from the manual path) and Task 4 Step 1 (helper short-circuits `next_run_at` write when `!scheduled`). Verified by Task 7 Step 4.
- **Spec § "UI"** → Task 6.
- **Spec § "Behavior summary" rows** → Task 7 Steps 1-6 (one verification per row).
- **Spec § "Verification"** → Task 7 (1:1 mapping with the listed manual checks).

No placeholder steps. All function names used in later tasks (`advanceSchedule`, `SetDigestNextRun`, `runOne(ctx, id, scheduled)`, `Run(ctx, id, scheduled)`) are defined the first time they appear and are used consistently afterward.
