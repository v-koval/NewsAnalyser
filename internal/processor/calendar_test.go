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
