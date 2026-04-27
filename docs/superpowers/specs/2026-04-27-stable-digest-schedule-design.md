# Stable digest schedule (no drift)

## Problem

Digest processing currently uses `digests.last_run_at` as the only scheduling signal. The scheduler ticks every hour and runs a digest when `now - last_run_at >= frequency_hours`. The processor records `last_run_at = to` where `to = time.Now().UTC()` is captured at the start of each run.

Two interacting effects cause the next-run time to drift forward by ~1 hour on every cycle:

1. `to` is captured a few milliseconds after the tick reads `now`, so on the matching tick of the next period the inequality `now - last_run_at >= frequency` is just barely false. The tick is skipped, and the next tick (one hour later) ends up running.
2. The hourly ticker has no anchor to a planned schedule — once a slot is missed, drift accumulates.

The user-visible symptom: a digest scheduled for "every 24h" runs ~1 hour later each day.

## Goal

The scheduled time of digest processing must not depend on processing duration or on the exact moment a tick fires. Two consecutive runs of a `frequency_hours = N` digest must be planned exactly `N` hours apart.

## Design

### New column

Add `next_run_at TIMESTAMPTZ` (nullable) to the `digests` table. Semantics:

- `next_run_at` — the **planned** time of the next run. Authoritative for scheduling.
- `last_run_at` — kept as-is, the **actual** start time of the most recent run. Used by the UI only.

`NULL` for `next_run_at` means "never run, run on the next tick" — same trigger as today's `last_run_at IS NULL`.

### Migration `003_add_next_run_at.sql`

```sql
ALTER TABLE digests
    ADD COLUMN next_run_at TIMESTAMPTZ;

UPDATE digests
SET next_run_at = COALESCE(last_run_at, now()) + (frequency_hours * interval '1 hour');
```

Existing digests preserve their schedule: the next planned slot equals their previous last run plus their period. Digests that have never run get `now() + frequency`, which keeps the current "run on next tick" feel without an immediate burst at deploy time.

### Scheduler tick algorithm

`internal/scheduler/scheduler.go`, in `tick`, replace the `last_run_at`-based gate with:

```go
freq := time.Duration(d.FrequencyHours) * time.Hour

// 1) Never run: trigger immediately.
if d.NextRunAt == nil {
    go s.runOne(ctx, d.ID)
    continue
}

// 2) Slot is in the future: wait.
if now.Before(*d.NextRunAt) {
    continue
}

// 3) Slot is more than one frequency in the past: missed slots are dropped.
//    Advance next_run_at to the nearest future slot on the grid; do NOT run.
if now.Sub(*d.NextRunAt) >= freq {
    next := *d.NextRunAt
    for !next.After(now) {
        next = next.Add(freq)
    }
    _ = s.Repo.SetDigestNextRun(ctx, d.ID, next)
    continue
}

// 4) Normal slot: next_run_at <= now < next_run_at + freq → run.
go s.runOne(ctx, d.ID)
```

Tick interval (1 hour) and the `s.trigger` channel are unchanged.

### Processor: advancing the schedule

In `internal/processor/processor.go`, all four current call sites of `SetDigestLastRun(ctx, d.ID, to)` are replaced by a single helper:

```go
func (p *Processor) advanceSchedule(ctx context.Context, d models.Digest, to time.Time, scheduled bool) {
    p.Repo.SetDigestLastRun(ctx, d.ID, to)
    // Manual triggers preserve the regular schedule — except for the first
    // run of a brand-new digest, where next_run_at is still NULL and must be
    // initialized so the next tick does not fire the digest again.
    if !scheduled && d.NextRunAt != nil {
        return
    }
    freq := time.Duration(d.FrequencyHours) * time.Hour
    var next time.Time
    if d.NextRunAt != nil {
        next = d.NextRunAt.Add(freq)  // shift relative to PLANNED time, not now()
    } else {
        next = to.Add(freq)            // first run anchors the schedule
    }
    p.Repo.SetDigestNextRun(ctx, d.ID, next)
}
```

(Errors are elided in the pseudocode for clarity; the implementation logs them.)

The crucial property is `next = d.NextRunAt + freq`, **not** `now + freq`. This eliminates the drift: tick timing and processing duration no longer leak into the schedule.

If `NextRunAt` is `nil` (first run of a freshly created digest), `next = to + freq` — the schedule anchors at the moment of the first actual run. This initialization applies to both scheduled and manual first runs, so that a brand-new digest manually triggered does not get re-fired by the next scheduler tick.

### Manual trigger

`Scheduler.Trigger(digestID)` already exists for the UI "run now" button. It must **not** modify `next_run_at` — manual runs do not shift the regular schedule.

Implementation: `Processor.Run` accepts an additional boolean parameter `scheduled`. The scheduled path (`Scheduler.tick` → `Scheduler.runOne`) passes `true`; the manual path (`Scheduler.Trigger` → `Scheduler.runOne`) passes `false`. Inside `Processor.Run`, the `advanceSchedule` helper writes `last_run_at` unconditionally; it then writes `next_run_at` if the run was scheduled, OR if `next_run_at` is still `NULL` (first-run initialization for newly created digests).

### Repo changes

`internal/repo/repo.go`:

- `digestCols` const — append `next_run_at`.
- `scanDigest` — scan into `&d.NextRunAt`.
- `CreateDigest` / `UpdateDigest` — `RETURNING` the new column set; do not write `next_run_at` on insert (left `NULL`).
- New method:

  ```go
  func (r *Repo) SetDigestNextRun(ctx context.Context, id string, t time.Time) error {
      _, err := r.Pool.Exec(ctx, `UPDATE digests SET next_run_at=$2 WHERE id=$1`, id, t)
      return err
  }
  ```

`SetDigestLastRun` is unchanged.

### Models

`internal/models/models.go` — add to `Digest`:

```go
NextRunAt *time.Time `json:"next_run_at"`
```

### UI

`internal/handlers/web/app.js` — in the digest list row, render an additional line next to "Последний запуск":

```
Следующий запуск: <localized next_run_at, or "—" if null>
```

## Behavior summary

| Situation                                        | Behavior                                                                                                            |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------- |
| New digest created                               | `next_run_at = NULL` → runs on the next hourly tick. After first run, `next_run_at = to + frequency`.               |
| Steady-state run                                 | `next_run_at` advances by exactly `frequency_hours` each cycle. No drift.                                           |
| Tick fires late (typical case)                   | Processed as a normal slot if delay < `frequency`. Schedule keeps the original grid.                                |
| App was offline through one or more slots        | First post-restart tick: scheduler advances `next_run_at` to the nearest future slot **without running**.            |
| `frequency_hours` changed via UI                 | `next_run_at` is **not** recalculated. The current planned slot stands; subsequent slots use the new period.         |
| Manual "run now" trigger                         | Runs immediately. `last_run_at` updates. `next_run_at` is **not** modified — except on the very first run of a newly created digest, where `next_run_at` is initialized to `last_run_at + frequency`. Otherwise the regular schedule is preserved. |

## Verification

The project has no automated test suite for this area, so verification is manual:

1. Apply migration on a database with existing digests; confirm `next_run_at` is populated as `last_run_at + frequency` (or `now() + frequency` for never-run digests).
2. Create a digest with `frequency_hours = 1`. Watch 3–4 cycles. Confirm `next_run_at` increments by exactly one hour each time, regardless of how long processing took.
3. Create a fresh digest. Confirm it runs on the next tick and `next_run_at` is then set to `last_run_at + frequency`.
4. Stop the app for more than `2 * frequency`, restart. Confirm no catch-up run fires and `next_run_at` is jumped to a future slot on the grid.
5. Click "run now" in the UI. Confirm `last_run_at` updates and `next_run_at` is unchanged.
6. UI: confirm the digest list shows both "Последний запуск" and "Следующий запуск" with sensible values.

## Out of scope

- Sub-hour scheduling precision (the tick remains hourly).
- Cron-style expressions or per-digest custom schedules beyond a fixed `frequency_hours` interval.
- Automatic recalculation of `next_run_at` on `frequency_hours` change.
- Catching up on missed slots.
