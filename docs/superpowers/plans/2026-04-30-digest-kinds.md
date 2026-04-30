# Digest Kinds Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a digest type selector with two kinds — `news` (existing behavior) and `facts` (interesting facts collection where the agent picks date-anchored items by day+month plus topical items).

**Architecture:** Add a `kind` column to `digests` (default `'news'` covers existing rows). Reuse the entire materials/runs/HTML/email pipeline as-is. The processor branches on `kind` to pick one of two prompts; the facts prompt receives a precomputed list of (day, month) pairs covering the run period in human-readable form. The HTML template skips the "Source" line when a material's `url` is empty (facts may have none).

**Tech Stack:** Go 1.22, PostgreSQL 16 (pgx/v5), vanilla JS frontend.

---

## File Structure

**Create:**
- `internal/db/migrations/004_add_digest_kind.sql` — schema migration
- `internal/processor/calendar.go` — pure helper: build (day, month) range string for the prompt
- `internal/processor/calendar_test.go` — unit tests for the helper
- `internal/processor/prompt_facts.go` — `buildFactsPrompt(d, from, to)` (split out to keep `processor.go` readable)
- `internal/processor/prompt_facts_test.go` — unit tests for the facts prompt builder

**Modify:**
- `internal/models/models.go` — add `Kind` to `Digest`
- `internal/repo/repo.go` — include `kind` in scans / inserts / updates
- `internal/processor/processor.go` — branch on `d.Kind` in `Run`; wrap the "Source" HTML block in `if m.URL != ""`
- `internal/handlers/handlers.go` — normalize / validate `kind` in `createDigest` and `updateDigest`
- `internal/handlers/web/app.js` — add `<select>` to the modal, send `kind` in payload, render kind badge in the list

---

## Task 1: Migration and model field

**Files:**
- Create: `internal/db/migrations/004_add_digest_kind.sql`
- Modify: `internal/models/models.go`

- [ ] **Step 1: Create the migration file**

Create `internal/db/migrations/004_add_digest_kind.sql`:

```sql
ALTER TABLE digests
    ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'news';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE table_name = 'digests' AND constraint_name = 'digests_kind_check'
    ) THEN
        ALTER TABLE digests
            ADD CONSTRAINT digests_kind_check CHECK (kind IN ('news', 'facts'));
    END IF;
END$$;
```

The `IF NOT EXISTS` clauses make the migration idempotent (consistent with `003_add_next_run_at.sql`). The default `'news'` automatically backfills every existing row.

- [ ] **Step 2: Add the `Kind` field to the model**

In `internal/models/models.go`, add `Kind` to `Digest` after `Language`:

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
	Kind           string     `json:"kind"`
	Enabled        bool       `json:"enabled"`
	LastRunAt      *time.Time `json:"last_run_at"`
	NextRunAt      *time.Time `json:"next_run_at"`
	AutoSources    []string   `json:"auto_sources"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
```

- [ ] **Step 3: Verify compile**

Run: `go build ./...`
Expected: exits 0 with no output. (`Kind` is unused in repo yet, but Go doesn't warn on unused struct fields.)

- [ ] **Step 4: Commit**

```bash
git add internal/db/migrations/004_add_digest_kind.sql internal/models/models.go
git commit -m "feat(db): add digest kind column and model field"
```

---

## Task 2: Repository support for `kind`

**Files:**
- Modify: `internal/repo/repo.go:124-161` (the digest scan/columns/CRUD block)

- [ ] **Step 1: Update `digestCols` and `scanDigest`**

In `internal/repo/repo.go`, replace the `scanDigest` function and `digestCols` constant:

```go
func scanDigest(row pgx.Row) (models.Digest, error) {
	var d models.Digest
	var sources, ignored, recipients, auto []byte
	err := row.Scan(&d.ID, &d.Name, &d.Topic, &sources, &ignored, &d.FrequencyHours, &recipients, &d.Language, &d.Kind, &d.Enabled, &d.LastRunAt, &d.NextRunAt, &auto, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	_ = json.Unmarshal(sources, &d.Sources)
	_ = json.Unmarshal(ignored, &d.IgnoredSources)
	_ = json.Unmarshal(recipients, &d.Recipients)
	_ = json.Unmarshal(auto, &d.AutoSources)
	return d, nil
}

const digestCols = `id,name,topic,sources,ignored_sources,frequency_hours,recipients,language,kind,enabled,last_run_at,next_run_at,auto_sources,created_at,updated_at`
```

Note the order: `kind` is inserted between `language` and `enabled`. This must match the order in `scanDigest`'s `Scan` call.

- [ ] **Step 2: Update `CreateDigest`**

Replace `CreateDigest` with:

```go
func (r *Repo) CreateDigest(ctx context.Context, d models.Digest) (models.Digest, error) {
	src, _ := json.Marshal(orEmpty(d.Sources))
	ign, _ := json.Marshal(orEmpty(d.IgnoredSources))
	rec, _ := json.Marshal(orEmpty(d.Recipients))
	auto, _ := json.Marshal(orEmpty(d.AutoSources))
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO digests(name,topic,sources,ignored_sources,frequency_hours,recipients,language,kind,enabled,auto_sources)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+digestCols,
		d.Name, d.Topic, src, ign, d.FrequencyHours, rec, d.Language, d.Kind, d.Enabled, auto)
	return scanDigest(row)
}
```

- [ ] **Step 3: Update `UpdateDigest`**

Replace `UpdateDigest` with:

```go
func (r *Repo) UpdateDigest(ctx context.Context, d models.Digest) (models.Digest, error) {
	src, _ := json.Marshal(orEmpty(d.Sources))
	ign, _ := json.Marshal(orEmpty(d.IgnoredSources))
	rec, _ := json.Marshal(orEmpty(d.Recipients))
	row := r.Pool.QueryRow(ctx,
		`UPDATE digests SET name=$2,topic=$3,sources=$4,ignored_sources=$5,frequency_hours=$6,recipients=$7,language=$8,kind=$9,enabled=$10,updated_at=now()
		 WHERE id=$1 RETURNING `+digestCols,
		d.ID, d.Name, d.Topic, src, ign, d.FrequencyHours, rec, d.Language, d.Kind, d.Enabled)
	return scanDigest(row)
}
```

- [ ] **Step 4: Verify compile**

Run: `go build ./...`
Expected: exits 0 with no output.

- [ ] **Step 5: Commit**

```bash
git add internal/repo/repo.go
git commit -m "feat(repo): persist and read digest kind"
```

---

## Task 3: Calendar helper (TDD)

**Files:**
- Create: `internal/processor/calendar.go`
- Test: `internal/processor/calendar_test.go`

The helper builds the human-readable `(day, month)` list passed to the facts prompt. Pure function, easy to test.

- [ ] **Step 1: Write the failing test**

Create `internal/processor/calendar_test.go`:

```go
package processor

import (
	"strings"
	"testing"
	"time"
)

func TestCalendarRangeDescription_SingleDay(t *testing.T) {
	from := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 1, 23, 59, 0, 0, time.UTC)
	got := calendarRangeDescription(from, to)
	if got != "1 мая" {
		t.Fatalf("single day: got %q, want %q", got, "1 мая")
	}
}

func TestCalendarRangeDescription_WeekWithinMonth(t *testing.T) {
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 7, 23, 59, 0, 0, time.UTC)
	got := calendarRangeDescription(from, to)
	want := "1 мая, 2 мая, 3 мая, 4 мая, 5 мая, 6 мая, 7 мая"
	if got != want {
		t.Fatalf("week: got %q, want %q", got, want)
	}
}

func TestCalendarRangeDescription_AcrossMonthBoundary(t *testing.T) {
	from := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	got := calendarRangeDescription(from, to)
	want := "30 апреля, 1 мая, 2 мая"
	if got != want {
		t.Fatalf("month boundary: got %q, want %q", got, want)
	}
}

func TestCalendarRangeDescription_AcrossYearBoundary(t *testing.T) {
	from := time.Date(2026, 12, 30, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC)
	got := calendarRangeDescription(from, to)
	want := "30 декабря, 31 декабря, 1 января, 2 января"
	if got != want {
		t.Fatalf("year boundary: got %q, want %q", got, want)
	}
}

func TestCalendarRangeDescription_FullYear(t *testing.T) {
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	got := calendarRangeDescription(from, to)
	if !strings.Contains(got, "весь год") {
		t.Fatalf("full year: expected substring %q in %q", "весь год", got)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/processor -run TestCalendarRangeDescription -v`
Expected: build error — `undefined: calendarRangeDescription`.

- [ ] **Step 3: Implement the helper**

Create `internal/processor/calendar.go`:

```go
package processor

import (
	"fmt"
	"strings"
	"time"
)

var ruMonthsGenitive = [...]string{
	"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

// calendarRangeDescription returns a human-readable list of calendar dates
// covered by [from, to] in UTC, formatted like "1 мая, 2 мая". It ignores
// time-of-day and spans year boundaries naturally. If the period covers
// 365 days or more, it returns "весь год" to keep the prompt short.
func calendarRangeDescription(from, to time.Time) string {
	from = from.UTC().Truncate(24 * time.Hour)
	to = to.UTC().Truncate(24 * time.Hour)
	if to.Before(from) {
		from, to = to, from
	}
	days := int(to.Sub(from).Hours()/24) + 1
	if days >= 365 {
		return "весь год"
	}
	parts := make([]string, 0, days)
	for d := from; !d.After(to); d = d.Add(24 * time.Hour) {
		parts = append(parts, fmt.Sprintf("%d %s", d.Day(), ruMonthsGenitive[int(d.Month())-1]))
	}
	return strings.Join(parts, ", ")
}
```

- [ ] **Step 4: Run the tests, verify they pass**

Run: `go test ./internal/processor -run TestCalendarRangeDescription -v`
Expected: all five subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/processor/calendar.go internal/processor/calendar_test.go
git commit -m "feat(processor): calendar range description helper"
```

---

## Task 4: Facts prompt builder (TDD)

**Files:**
- Create: `internal/processor/prompt_facts.go`
- Test: `internal/processor/prompt_facts_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/processor/prompt_facts_test.go`:

```go
package processor

import (
	"strings"
	"testing"
	"time"

	"newsanalyzer/internal/models"
)

func TestBuildFactsPrompt_ContainsKeyElements(t *testing.T) {
	d := models.Digest{
		Topic:          "Русская литература XIX века",
		Language:       "ru",
		IgnoredSources: []string{"badsource.example"},
	}
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 3, 23, 59, 0, 0, time.UTC)

	got := buildFactsPrompt(d, from, to)

	for _, want := range []string{
		"Русская литература XIX века",
		"1 мая, 2 мая, 3 мая",
		"день рождения",
		"БЕЗ ПРИВЯЗКИ К ДАТЕ",
		`"materials"`,
		`"discovered_sources"`,
		`"analyzed_sources"`,
		"badsource.example",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected prompt to contain %q\nfull prompt:\n%s", want, got)
		}
	}
}

func TestBuildFactsPrompt_FullYearWindow(t *testing.T) {
	d := models.Digest{Topic: "T", Language: "ru"}
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got := buildFactsPrompt(d, from, to)
	if !strings.Contains(got, "весь год") {
		t.Fatalf("expected 'весь год' marker in long-window prompt:\n%s", got)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/processor -run TestBuildFactsPrompt -v`
Expected: build error — `undefined: buildFactsPrompt`.

- [ ] **Step 3: Implement the prompt builder**

Create `internal/processor/prompt_facts.go`:

```go
package processor

import (
	"fmt"
	"strings"
	"time"

	"newsanalyzer/internal/models"
)

// buildFactsPrompt constructs the agent prompt for digests of kind "facts".
// The agent is asked to combine date-anchored facts (where the calendar date,
// day+month only, falls inside [from, to]) with topical facts that have no
// date binding. The response shape matches the news prompt so the rest of
// the pipeline stays unchanged.
func buildFactsPrompt(d models.Digest, from, to time.Time) string {
	dates := calendarRangeDescription(from, to)
	b := &strings.Builder{}
	fmt.Fprintf(b, "Ты — ассистент, формирующий подборку интересных фактов по заданной теме.\n\n")
	fmt.Fprintf(b, "Тематика подборки: %s\n", d.Topic)
	fmt.Fprintf(b, "Календарные даты, на которые ориентируемся (только число и месяц, год не важен): %s.\n", dates)
	fmt.Fprintf(b, "Язык подборки: %s. Если оригинал материала на другом языке — переведи заголовок, краткое содержание и полный текст на выбранный язык.\n", d.Language)
	if len(d.IgnoredSources) > 0 {
		fmt.Fprintf(b, "ПОЛНОСТЬЮ игнорируй эти источники: %s.\n", strings.Join(d.IgnoredSources, ", "))
	}
	b.WriteString(`
Подбери НЕСКОЛЬКО фактов двух категорий — пропорцию выбираешь сам:

1. ФАКТЫ ПО ДАТАМ. Для каждой из перечисленных календарных дат проверь,
   произошло ли в эту дату (в любом году) что-то яркое и связанное с темой:
   день рождения или день смерти известного деятеля по теме, важное событие,
   открытие, публикация, премьера и т.п. Если для какой-то даты подходящего
   факта нет — пропусти её. Не выдумывай.

2. ФАКТЫ ПО ТЕМЕ БЕЗ ПРИВЯЗКИ К ДАТЕ. Несколько любопытных фактов, наблюдений
   или историй по теме, которые не привязаны к конкретной календарной дате,
   но интересны читателю.

Для каждого факта верни:
- url — ссылка на справочный источник (например, статью Wikipedia). Если уместного
  источника нет — пустая строка "".
- title — заголовок на выбранном языке.
- image_url — РЕАЛЬНАЯ прямая ссылка на иллюстрацию с источника, которую ты сам видел.
  НЕ УГАДЫВАЙ и не конструируй URL по шаблону. Если не уверен — пустая строка "".
- summary_title — короткий заголовок (1 строка, основная мысль).
- summary_text — короткий пересказ (3-5 предложений: суть факта и почему он интересен).
- full_text — развёрнутый текст факта на выбранном языке (контекст, детали, значение).

В discovered_sources верни справочные домены, которые ты использовал
самостоятельно. В analyzed_sources — итоговый список использованных доменов.

ВАЖНО: НЕ СОЗДАВАЙ НИКАКИХ ФАЙЛОВ. Не сохраняй результат в файл. Верни JSON ПРЯМО В ТЕКСТЕ ОТВЕТА.
Не пиши никаких пояснений, комментариев или описаний — только чистый JSON.

ТРЕБОВАНИЯ К JSON:
- Строго валидный JSON по RFC 8259, UTF-8.
- Внутри строковых значений запрещены неэкранированные двойные кавычки. Используй
  «ёлочки», „лапки" или одиночные кавычки, либо экранируй двойные как \".
- Все переводы строк внутри строк должны быть записаны как \n.

Формат ответа:
{
  "materials": [
    {
      "url": "...",
      "title": "...",
      "image_url": "...",
      "summary_title": "...",
      "summary_text": "...",
      "full_text": "..."
    }
  ],
  "discovered_sources": ["wikipedia.org"],
  "analyzed_sources": ["wikipedia.org"]
}
`)
	return b.String()
}
```

- [ ] **Step 4: Run the tests, verify they pass**

Run: `go test ./internal/processor -run TestBuildFactsPrompt -v`
Expected: both subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/processor/prompt_facts.go internal/processor/prompt_facts_test.go
git commit -m "feat(processor): facts prompt builder"
```

---

## Task 5: Processor branching and HTML fix

**Files:**
- Modify: `internal/processor/processor.go:122` (prompt selection in `Run`)
- Modify: `internal/processor/processor.go:352-354` (source link in `buildHTML`)

- [ ] **Step 1: Branch on `d.Kind` in `Run`**

In `internal/processor/processor.go`, replace the line:

```go
	prompt := buildPrompt(d, from, to)
```

with:

```go
	var prompt string
	switch d.Kind {
	case "facts":
		prompt = buildFactsPrompt(d, from, to)
	default:
		prompt = buildPrompt(d, from, to)
	}
```

`default` covers both `"news"` and the empty string, which keeps behavior stable for any record that somehow lacks a kind.

- [ ] **Step 2: Skip the source block in `buildHTML` when URL is empty**

In `buildHTML`, replace this block:

```go
			fmt.Fprintf(&b, `<p class="src">Источник: <a href="%s">%s</a></p></article>`,
				html.EscapeString(m.URL), html.EscapeString(m.URL))
```

with:

```go
			if m.URL != "" {
				fmt.Fprintf(&b, `<p class="src">Источник: <a href="%s">%s</a></p>`,
					html.EscapeString(m.URL), html.EscapeString(m.URL))
			}
			b.WriteString(`</article>`)
```

The `</article>` close was previously concatenated to the source line; now it always emits regardless of URL presence.

- [ ] **Step 3: Verify compile**

Run: `go build ./...`
Expected: exits 0 with no output.

- [ ] **Step 4: Run all processor tests**

Run: `go test ./internal/processor -v`
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/processor/processor.go
git commit -m "feat(processor): branch on digest kind, render source only when URL present"
```

---

## Task 6: Handler normalization for `kind`

**Files:**
- Modify: `internal/handlers/handlers.go:164-185` (`createDigest`) and `:196-212` (`updateDigest`)

- [ ] **Step 1: Add a normalization helper**

In `internal/handlers/handlers.go`, near the other helpers (just below `decode`), add:

```go
// normalizeDigestKind validates and normalizes the digest kind.
// Returns the canonical value and true if valid; empty string and false otherwise.
// An empty input is treated as "news" for backward compatibility with old clients.
func normalizeDigestKind(k string) (string, bool) {
	switch k {
	case "":
		return "news", true
	case "news", "facts":
		return k, true
	default:
		return "", false
	}
}
```

- [ ] **Step 2: Use the helper in `createDigest`**

Replace `createDigest` with:

```go
func (h *Handlers) createDigest(w http.ResponseWriter, r *http.Request) {
	var d models.Digest
	if err := decode(r, &d); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	if d.FrequencyHours <= 0 {
		d.FrequencyHours = 24
	}
	if d.Language == "" {
		d.Language = "ru"
	}
	kind, ok := normalizeDigestKind(d.Kind)
	if !ok {
		writeErr(w, 400, "invalid kind")
		return
	}
	d.Kind = kind
	created, err := h.Repo.CreateDigest(r.Context(), d)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if created.Enabled {
		h.Sched.Trigger(created.ID)
	}
	writeJSON(w, 200, created)
}
```

- [ ] **Step 3: Use the helper in `updateDigest`**

Replace `updateDigest` with:

```go
func (h *Handlers) updateDigest(w http.ResponseWriter, r *http.Request) {
	var d models.Digest
	if err := decode(r, &d); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	d.ID = r.PathValue("id")
	if d.FrequencyHours <= 0 {
		d.FrequencyHours = 24
	}
	kind, ok := normalizeDigestKind(d.Kind)
	if !ok {
		writeErr(w, 400, "invalid kind")
		return
	}
	d.Kind = kind
	updated, err := h.Repo.UpdateDigest(r.Context(), d)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, updated)
}
```

- [ ] **Step 4: Verify compile**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/handlers.go
git commit -m "feat(api): accept and validate digest kind"
```

---

## Task 7: Frontend — modal select and list badge

**Files:**
- Modify: `internal/handlers/web/app.js`

- [ ] **Step 1: Add the `kind` select to the modal**

In `internal/handlers/web/app.js`, locate `openDigestModal`. Update the default object to include `kind`:

```js
  d = d || {name:'',topic:'',sources:[],ignored_sources:[],frequency_hours:24,recipients:[],language:'ru',kind:'news',enabled:true};
```

In the modal's `innerHTML`, insert the kind select between the `Название` label and the `Тематика` label:

```html
    <label>Название<input name="name" required></label>
    <label>Тип дайджеста
      <select name="kind">
        <option value="news">Новостной дайджест</option>
        <option value="facts">Подборка интересных фактов</option>
      </select>
    </label>
    <label>Тематика (описание для анализа)<textarea name="topic"></textarea></label>
```

- [ ] **Step 2: Read and submit `kind`**

After the existing `form.enabled.checked = !!d.enabled;` line, add:

```js
  form.kind.value = d.kind || 'news';
```

In the `payload` object inside `form.onsubmit`, add `kind`:

```js
    const payload = {
      name: form.name.value.trim(),
      topic: form.topic.value.trim(),
      kind: form.kind.value,
      frequency_hours: parseInt(form.frequency_hours.value, 10),
      language: form.language.value.trim(),
      enabled: form.enabled.checked,
      sources: ts.get(), ignored_sources: ti.get(), recipients: tr.get(),
    };
```

- [ ] **Step 3: Render the kind badge in the list**

In `renderDigests`, locate the badges row inside the digest item template and add a kind badge after the language badge:

```js
            <span class="badge ${d.enabled?'':'off'}">${d.enabled?'включен':'выключен'}</span>
            <span class="badge off">каждые ${d.frequency_hours} ч</span>
            <span class="badge off">${esc(d.language)}</span>
            <span class="badge off">${(d.kind || 'news') === 'facts' ? 'Факты' : 'Новости'}</span>
```

- [ ] **Step 4: Manual smoke check (optional dev sanity)**

Run: `go run ./cmd/server` (with Postgres up via `docker compose up -d`).
Open http://localhost:8080, log in, open the Дайджесты screen, click «+ Новый дайджест». Confirm the form has the new «Тип дайджеста» select with two options. Cancel out — no DB write needed for this step.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/web/app.js
git commit -m "feat(ui): digest kind selector and badge"
```

---

## Task 8: End-to-end smoke verification

**Files:** none — runtime check.

- [ ] **Step 1: Start the stack**

Run: `docker compose up -d` then `go run ./cmd/server`.
Expected: server starts on `:8080`. Migrations 001..004 apply cleanly (confirm by inspecting logs — no migration error).

- [ ] **Step 2: Verify the migration backfilled existing digests**

In a separate shell, connect to Postgres and run:

```sql
SELECT id, name, kind FROM digests;
```

Expected: every existing row has `kind = 'news'`. The `digests_kind_check` constraint exists:

```sql
SELECT conname FROM pg_constraint WHERE conrelid = 'digests'::regclass AND conname = 'digests_kind_check';
```

Expected: one row.

- [ ] **Step 3: Create a facts digest via UI**

Open the app, create a new digest:
- Name: «Тестовая подборка фактов»
- Тип: Подборка интересных фактов
- Тематика: «Русская литература XIX века»
- Регулярность: 24
- Язык: ru
- Enabled: on
- Recipients: optional
- Sources: empty (let agent pick)

Save. Expected: the new digest appears in the list with the **Факты** badge. The badge for old digests reads **Новости**.

- [ ] **Step 4: Trigger a run and inspect output**

From the digest list, click «Запустить» on the facts digest. Wait for the run to finish (check History tab). Open the run's HTML view.

Expected:
- Page renders without errors.
- The materials look like facts (anniversary-style or topical), not news.
- For materials whose `url` is empty, the «Источник:» line is absent — only title and text.
- For materials with a `url`, the «Источник:» line still appears.

If the agent returns empty materials or fails, the existing error path renders a placeholder page — this is acceptable for the smoke check; rerun until you see at least one successful facts run.

- [ ] **Step 5: Edit existing digest, change kind**

Open one of the pre-existing news digests and switch its kind to «Подборка интересных фактов». Save. Expected: PUT succeeds, badge updates to **Факты**, past runs in History remain unchanged. Switch back to **Новости** to leave the digest in its original state.

- [ ] **Step 6: Negative test — invalid kind via API**

Run:

```bash
curl -X POST http://localhost:8080/api/digests \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <access_token>" \
  -d '{"name":"x","topic":"y","kind":"bogus","frequency_hours":24,"language":"ru"}'
```

Expected: HTTP 400 with `{"error":"invalid kind"}`.

- [ ] **Step 7: No commit needed**

Smoke verification only — nothing to stage. If a defect surfaces, fix it inside the relevant earlier task and re-run from this task.

---

## Self-Review Notes

Coverage check against the spec:

- ✅ Migration with default `'news'` for existing rows → Task 1.
- ✅ `Kind` model field, repo CRUD, normalization in handlers → Tasks 1, 2, 6.
- ✅ Processor branching by kind → Task 5.
- ✅ Facts prompt: topic, language, calendar dates list, two categories, ignored sources, JSON contract → Tasks 3, 4.
- ✅ HTML template skips source line on empty URL → Task 5.
- ✅ Frontend select + payload + badge → Task 7.
- ✅ Edge cases: empty kind → news, invalid kind → 400, kind change on existing digest, full-year window → Tasks 3, 4, 6, 8.
