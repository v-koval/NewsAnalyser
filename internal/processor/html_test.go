package processor

import (
	"strings"
	"testing"
	"time"

	"newsanalyzer/internal/models"
)

func TestBuildHTML_SourceBlockOnlyWhenURLPresent(t *testing.T) {
	d := models.Digest{Name: "Тест"}
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)

	materials := []models.Material{
		{
			Title:        "С источником",
			SummaryTitle: "С источником",
			SummaryText:  "—",
			FullText:     "Полный текст 1.",
			URL:          "https://example.com/article",
		},
		{
			Title:        "Без источника",
			SummaryTitle: "Без источника",
			SummaryText:  "—",
			FullText:     "Полный текст 2.",
			URL:          "",
		},
	}

	got := buildHTML(d, from, to, materials, "")

	// Article 0: with URL — source line and example.com link must appear.
	if !strings.Contains(got, `href="https://example.com/article"`) {
		t.Errorf("expected source link for material with URL\nHTML:\n%s", got)
	}
	if strings.Count(got, "Источник:") != 1 {
		t.Errorf("expected exactly one «Источник:» line for the material with URL, got %d\nHTML:\n%s",
			strings.Count(got, "Источник:"), got)
	}

	// Each material must produce exactly one </article> close, regardless of URL.
	if got, want := strings.Count(got, "</article>"), len(materials); got != want {
		t.Errorf("expected %d </article> closes, got %d", want, got)
	}

	// Each material must produce exactly one <article opening tag.
	if got, want := strings.Count(got, "<article"), len(materials); got != want {
		t.Errorf("expected %d <article opens, got %d", want, got)
	}
}

func TestBuildHTML_NoMaterials(t *testing.T) {
	d := models.Digest{Name: "Пусто"}
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)

	got := buildHTML(d, from, to, nil, "")
	if strings.Contains(got, "<article") {
		t.Errorf("expected no <article> elements when materials is empty\nHTML:\n%s", got)
	}
}
