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

	"newsanalyzer/internal/cursor"
	"newsanalyzer/internal/images"
	"newsanalyzer/internal/mailer"
	"newsanalyzer/internal/models"
	"newsanalyzer/internal/repo"
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
	last, err := p.Repo.LastRunPeriodTo(ctx, d.ID)
	if err != nil {
		return fmt.Errorf("last run period_to: %w", err)
	}
	if last != nil {
		from = last.UTC()
	} else {
		from = to.Add(-7 * 24 * time.Hour)
	}

	prompt := buildPrompt(d, from, to)

	run := models.DigestRun{
		DigestID:   d.ID,
		DigestName: d.Name,
		PeriodFrom: from,
		PeriodTo:   to,
	}
	started, err := p.Repo.StartRun(ctx, run)
	if err != nil {
		return fmt.Errorf("start run: %w", err)
	}
	run = started
	run.Status = "ok"

	cc := cursor.New(settings.CursorAPIKey)
	text, aerr := cc.RunPrompt(ctx, prompt, settings.CursorRepository)
	if aerr != nil {
		run.Status = "error"
		run.Error = aerr.Error()
		run.HTML = "<p>Ошибка обработки: " + html.EscapeString(aerr.Error()) + "</p>"
		if err := p.Repo.FinishRun(ctx, run); err != nil {
			log.Printf("finish failed run: %v", err)
		}
		_ = p.Repo.SetDigestLastRun(ctx, d.ID, to)
		return aerr
	}

	log.Printf("agent response length: %d chars", len(text))
	if len(text) > 500 {
		log.Printf("agent response start: %s", text[:500])
	} else {
		log.Printf("agent response full: %s", text)
	}
	raw := cursor.ExtractJSON(text)
	log.Printf("extracted JSON length: %d chars", len(raw))
	var ar agentResponse
	parseErr := json.Unmarshal([]byte(raw), &ar)
	if parseErr != nil {
		repaired := cursor.RepairJSON(raw)
		if repaired != raw {
			log.Printf("parse agent json failed (%v), retrying with repaired payload", parseErr)
			if err := json.Unmarshal([]byte(repaired), &ar); err == nil {
				parseErr = nil
			} else {
				log.Printf("repaired parse also failed: %v", err)
				parseErr = err
			}
		}
	}
	if parseErr != nil {
		run.Status = "error"
		run.Error = "bad agent response: " + parseErr.Error()
		run.HTML = "<p>Не удалось разобрать ответ агента.</p><pre>" + html.EscapeString(text) + "</pre>"
		if err := p.Repo.FinishRun(ctx, run); err != nil {
			log.Printf("finish failed run: %v", err)
		}
		_ = p.Repo.SetDigestLastRun(ctx, d.ID, to)
		return fmt.Errorf("parse agent json: %w", parseErr)
	}

	run.AnalyzedSources = mergeStrings(d.Sources, ar.AnalyzedSources)
	if len(ar.Materials) == 0 {
		run.Status = "empty"
		run.HTML = buildHTML(d, from, to, nil, "")
		if err := p.Repo.FinishRun(ctx, run); err != nil {
			return err
		}
		_ = p.Repo.SetDigestLastRun(ctx, d.ID, to)
		if len(ar.DiscoveredSources) > 0 && len(d.Sources) == 0 {
			_ = p.Repo.AppendAutoSources(ctx, d.ID, ar.DiscoveredSources)
		}
		sendMail(d, run, settings)
		return nil
	}

	for i, m := range ar.Materials {
		mat := models.Material{
			URL:          m.URL,
			Title:        m.Title,
			SummaryTitle: strOr(m.SummaryTitle, m.Title),
			SummaryText:  m.SummaryText,
			FullText:     m.FullText,
			Position:     i,
		}
		candidates := []string{}
		if resolved, err := p.Images.ResolveArticleImage(ctx, m.URL); err != nil {
			log.Printf("resolve article image %s: %v", m.URL, err)
		} else if resolved != "" {
			candidates = append(candidates, resolved)
		}
		if m.ImageURL != "" {
			candidates = append(candidates, m.ImageURL)
		}
		for _, imgURL := range candidates {
			if _, pub, err := p.Images.Fetch(ctx, run.ID, imgURL); err == nil {
				mat.ImageURL = imgURL
				mat.LocalImage = pub
				break
			} else {
				log.Printf("image fetch %s: %v", imgURL, err)
			}
		}
		if err := p.Repo.AddMaterial(ctx, run.ID, mat); err != nil {
			log.Printf("save material: %v", err)
		}
		run.Materials = append(run.Materials, mat)
	}
	run.HTML = buildHTML(d, from, to, run.Materials, "")
	if err := p.Repo.FinishRun(ctx, run); err != nil {
		log.Printf("finish run: %v", err)
	}
	_ = p.Repo.SetDigestLastRun(ctx, d.ID, to)
	if len(ar.DiscoveredSources) > 0 {
		_ = p.Repo.AppendAutoSources(ctx, d.ID, ar.DiscoveredSources)
	}
	sendMail(d, run, settings)
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
- URL основной иллюстрации (image_url) — только если это РЕАЛЬНАЯ прямая ссылка на картинку с сайта-источника, которую ты сам видел в материале. НЕ УГАДЫВАЙ и не конструируй URL по шаблону. Если не уверен — оставь image_url пустой строкой, мы сами извлечём обложку из Open Graph-меток страницы,
- краткий заголовок (summary_title, 1 строка, основная мысль),
- короткий пересказ (summary_text, 3-5 предложений: основная мысль и выводы),
- полный текст материала (full_text) на выбранном языке, сохраняя смысл и структуру оригинала.

Если ты самостоятельно подбирал источники — верни их в discovered_sources.
В analyzed_sources верни итоговый список проанализированных сайтов (доменов).

ВАЖНО: НЕ СОЗДАВАЙ НИКАКИХ ФАЙЛОВ. Не сохраняй результат в файл. Верни JSON ПРЯМО В ТЕКСТЕ ОТВЕТА.
Не пиши никаких пояснений, комментариев или описаний — только чистый JSON.

ТРЕБОВАНИЯ К JSON:
- Строго валидный JSON по RFC 8259, UTF-8.
- Внутри строковых значений запрещены неэкранированные двойные кавычки. Если нужно процитировать что-то, используй «ёлочки», „лапки" или одиночные кавычки, либо экранируй двойные как \".
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
