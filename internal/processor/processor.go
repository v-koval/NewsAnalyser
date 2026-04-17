package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"strings"
	"sync"
	"time"

	"newsanalisator/internal/cursor"
	"newsanalisator/internal/images"
	"newsanalisator/internal/mailer"
	"newsanalisator/internal/models"
	"newsanalisator/internal/repo"
)

type Processor struct {
	Repo    *repo.Repo
	Images  *images.Fetcher
	mu      sync.Mutex
	running map[string]bool
}

func New(r *repo.Repo, imgs *images.Fetcher) *Processor {
	return &Processor{Repo: r, Images: imgs, running: map[string]bool{}}
}

type agentMaterial struct {
	URL          string   `json:"url"`
	Title        string   `json:"title"`
	ImageURL     string   `json:"image_url"`
	SummaryTitle string   `json:"summary_title"`
	SummaryText  string   `json:"summary_text"`
	FullText     string   `json:"full_text"`
	SourceHost   string   `json:"source"`
	Tags         []string `json:"tags"`
}

type agentResponse struct {
	Materials          []agentMaterial `json:"materials"`
	DiscoveredSources  []string        `json:"discovered_sources"`
	AnalyzedSources    []string        `json:"analyzed_sources"`
}

func (p *Processor) tryLock(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running[id] {
		return false
	}
	p.running[id] = true
	return true
}

func (p *Processor) unlock(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.running, id)
}

func (p *Processor) Run(ctx context.Context, digestID string) error {
	if !p.tryLock(digestID) {
		return fmt.Errorf("digest %s already running", digestID)
	}
	defer p.unlock(digestID)

	d, err := p.Repo.GetDigest(ctx, digestID)
	if err != nil {
		return err
	}
	settings, err := p.Repo.GetSettings(ctx)
	if err != nil {
		return err
	}
	if settings.CursorAPIKey == "" {
		return fmt.Errorf("cursor api key is not set")
	}

	to := time.Now().UTC()
	var from time.Time
	if d.LastRunAt != nil {
		from = d.LastRunAt.UTC()
	} else {
		from = to.Add(-7 * 24 * time.Hour)
	}

	prompt := buildPrompt(d, from, to)

	cc := cursor.New(settings.CursorAPIKey)
	text, aerr := cc.RunPrompt(ctx, prompt, settings.CursorRepository)
	run := models.DigestRun{
		DigestID:   d.ID,
		DigestName: d.Name,
		PeriodFrom: from,
		PeriodTo:   to,
		Status:     "ok",
	}
	if aerr != nil {
		run.Status = "error"
		run.Error = aerr.Error()
		run.HTML = "<p>Ошибка обработки: " + html.EscapeString(aerr.Error()) + "</p>"
		if _, err := p.Repo.CreateRun(ctx, run); err != nil {
			log.Printf("save failed run: %v", err)
		}
		_ = p.Repo.SetDigestLastRun(ctx, d.ID, to)
		return aerr
	}

	raw := cursor.ExtractJSON(text)
	var ar agentResponse
	if err := json.Unmarshal([]byte(raw), &ar); err != nil {
		run.Status = "error"
		run.Error = "bad agent response: " + err.Error()
		run.HTML = "<p>Не удалось разобрать ответ агента.</p><pre>" + html.EscapeString(text) + "</pre>"
		if _, err := p.Repo.CreateRun(ctx, run); err != nil {
			log.Printf("save failed run: %v", err)
		}
		_ = p.Repo.SetDigestLastRun(ctx, d.ID, to)
		return fmt.Errorf("parse agent json: %w", err)
	}

	run.AnalyzedSources = mergeStrings(d.Sources, ar.AnalyzedSources)
	if len(ar.Materials) == 0 {
		run.Status = "empty"
		run.HTML = buildHTML(d, from, to, nil, "")
		saved, err := p.Repo.CreateRun(ctx, run)
		if err != nil {
			return err
		}
		_ = p.Repo.SetDigestLastRun(ctx, d.ID, to)
		if len(ar.DiscoveredSources) > 0 && len(d.Sources) == 0 {
			_ = p.Repo.AppendAutoSources(ctx, d.ID, ar.DiscoveredSources)
		}
		sendMail(d, saved, settings)
		return nil
	}

	saved, err := p.Repo.CreateRun(ctx, run)
	if err != nil {
		return err
	}
	for i, m := range ar.Materials {
		mat := models.Material{
			URL:          m.URL,
			Title:        m.Title,
			SummaryTitle: strOr(m.SummaryTitle, m.Title),
			SummaryText:  m.SummaryText,
			FullText:     m.FullText,
			ImageURL:     m.ImageURL,
			Position:     i,
		}
		if m.ImageURL != "" {
			if _, pub, err := p.Images.Fetch(ctx, saved.ID, m.ImageURL); err == nil {
				mat.LocalImage = pub
			} else {
				log.Printf("image fetch %s: %v", m.ImageURL, err)
			}
		}
		if err := p.Repo.AddMaterial(ctx, saved.ID, mat); err != nil {
			log.Printf("save material: %v", err)
		}
		saved.Materials = append(saved.Materials, mat)
	}
	saved.HTML = buildHTML(d, from, to, saved.Materials, "")
	if _, err := p.Repo.Pool.Exec(ctx, `UPDATE digest_runs SET html=$2 WHERE id=$1`, saved.ID, saved.HTML); err != nil {
		log.Printf("update html: %v", err)
	}
	_ = p.Repo.SetDigestLastRun(ctx, d.ID, to)
	if len(ar.DiscoveredSources) > 0 {
		_ = p.Repo.AppendAutoSources(ctx, d.ID, ar.DiscoveredSources)
	}
	sendMail(d, saved, settings)
	return nil
}

func sendMail(d models.Digest, run models.DigestRun, s models.Settings) {
	if len(d.Recipients) == 0 || s.SMTPHost == "" {
		return
	}
	m := mailer.New(s)
	if err := m.Send(d.Recipients, d.Name, run.HTML); err != nil {
		log.Printf("send mail: %v", err)
	}
}

func buildPrompt(d models.Digest, from, to time.Time) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "Ты — ассистент, формирующий новостной дайджест.\n\n")
	fmt.Fprintf(b, "Тематика дайджеста: %s\n", d.Topic)
	fmt.Fprintf(b, "Период для анализа: с %s по %s (UTC).\n",
		from.Format(time.RFC3339), to.Format(time.RFC3339))
	if len(d.Sources) > 0 {
		fmt.Fprintf(b, "Используй следующие источники в качестве основных: %s.\n", strings.Join(d.Sources, ", "))
	} else {
		fmt.Fprintf(b, "Список источников не задан — подбери релевантные источники самостоятельно исходя из тематики.\n")
	}
	if len(d.IgnoredSources) > 0 {
		fmt.Fprintf(b, "ПОЛНОСТЬЮ игнорируй эти источники: %s.\n", strings.Join(d.IgnoredSources, ", "))
	}
	fmt.Fprintf(b, "Язык дайджеста: %s. Если оригинал написан на другом языке — переведи заголовок, краткое содержание и полный текст на выбранный язык.\n", d.Language)
	b.WriteString(`
Найди наиболее важные и релевантные новости, события и аналитические статьи.
Для каждого материала собери:
- исходный URL (обязательно),
- оригинальный заголовок,
- URL основной иллюстрации, если есть,
- краткий заголовок (summary_title, 1 строка, основная мысль),
- короткий пересказ (summary_text, 3-5 предложений: основная мысль и выводы),
- полный текст материала (full_text) на выбранном языке, сохраняя смысл и структуру оригинала.

Если ты самостоятельно подбирал источники — верни их в discovered_sources.
В analyzed_sources верни итоговый список проанализированных сайтов (доменов).

Верни СТРОГО JSON в таком формате, без markdown и без пояснений:
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
  "discovered_sources": ["example.com"],
  "analyzed_sources": ["example.com"]
}
`)
	return b.String()
}

func buildHTML(d models.Digest, from, to time.Time, materials []models.Material, note string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>`)
	b.WriteString(html.EscapeString(d.Name))
	b.WriteString(`</title><style>
body{font-family:-apple-system,Segoe UI,Roboto,Arial,sans-serif;max-width:820px;margin:0 auto;padding:24px;color:#222;line-height:1.5}
h1{font-size:24px;margin:0 0 4px 0}
.period{color:#666;margin-bottom:24px}
.toc{background:#f5f7fa;padding:16px 20px;border-radius:8px;margin-bottom:32px}
.toc h2{font-size:16px;margin:0 0 12px 0;text-transform:uppercase;letter-spacing:.5px;color:#555}
.toc-item{margin-bottom:12px}
.toc-item a{color:#1a56db;text-decoration:none;font-weight:600}
.toc-item p{margin:4px 0 0;color:#444;font-size:14px}
article{border-top:1px solid #eee;padding-top:24px;margin-top:32px}
article h2{font-size:20px;margin:0 0 8px 0}
article img{max-width:100%;height:auto;border-radius:6px;margin:12px 0}
.src{color:#666;font-size:13px}
.src a{color:#1a56db}
</style></head><body>`)
	fmt.Fprintf(&b, "<h1>%s</h1>", html.EscapeString(d.Name))
	fmt.Fprintf(&b, `<div class="period">Период: %s — %s</div>`,
		from.Format("2006-01-02 15:04"), to.Format("2006-01-02 15:04"))
	if note != "" {
		fmt.Fprintf(&b, "<p>%s</p>", html.EscapeString(note))
	}
	if len(materials) == 0 {
		b.WriteString(`<p>Новых материалов не найдено.</p>`)
	} else {
		b.WriteString(`<div class="toc"><h2>Краткое содержание</h2>`)
		for i, m := range materials {
			fmt.Fprintf(&b, `<div class="toc-item"><a href="#m%d">%s</a><p>%s</p></div>`,
				i, html.EscapeString(m.SummaryTitle), html.EscapeString(m.SummaryText))
		}
		b.WriteString(`</div>`)
		for i, m := range materials {
			fmt.Fprintf(&b, `<article id="m%d"><h2>%s</h2>`, i, html.EscapeString(m.Title))
			img := m.LocalImage
			if img == "" {
				img = m.ImageURL
			}
			if img != "" {
				fmt.Fprintf(&b, `<img src="%s" alt="">`, html.EscapeString(img))
			}
			b.WriteString(`<div>`)
			b.WriteString(paragraphs(m.FullText))
			b.WriteString(`</div>`)
			fmt.Fprintf(&b, `<p class="src">Источник: <a href="%s">%s</a></p></article>`,
				html.EscapeString(m.URL), html.EscapeString(m.URL))
		}
	}
	b.WriteString(`</body></html>`)
	return b.String()
}

func paragraphs(t string) string {
	parts := strings.Split(strings.TrimSpace(t), "\n\n")
	var b strings.Builder
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		b.WriteString("<p>")
		b.WriteString(html.EscapeString(p))
		b.WriteString("</p>")
	}
	return b.String()
}

func strOr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func mergeStrings(a, b []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range a {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range b {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
