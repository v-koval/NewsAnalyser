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
