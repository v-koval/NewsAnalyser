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
	// d.Sources is intentionally not forwarded: facts are collected from
	// encyclopedic references the agent picks itself, not from curated
	// news source lists.
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
