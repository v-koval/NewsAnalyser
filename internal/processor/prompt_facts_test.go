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
