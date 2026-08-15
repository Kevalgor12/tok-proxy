package util

import (
	"fmt"
	"time"
)

// parseTime accepts the ISO strings tok writes (RFC3339, with or without fractional seconds).
func parseTime(s string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// WithinDays reports whether ts falls in the last `days` - a rolling 24h*days window
// (not calendar days), used by the analytics for "today / 7d / 30d".
func WithinDays(ts string, days int) bool {
	t, ok := parseTime(ts)
	if !ok {
		return false
	}
	return !t.Before(time.Now().Add(-time.Duration(days) * 24 * time.Hour))
}

func IsoDay(ts string) string {
	if t, ok := parseTime(ts); ok {
		return t.UTC().Format("2006-01-02")
	}
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

func IsoMonth(ts string) string {
	if t, ok := parseTime(ts); ok {
		return t.UTC().Format("2006-01")
	}
	if len(ts) >= 7 {
		return ts[:7]
	}
	return ts
}

// IsoWeek returns the ISO-8601 week label, e.g. "2026-W07".
func IsoWeek(ts string) string {
	t, ok := parseTime(ts)
	if !ok {
		return ts
	}
	y, w := t.UTC().ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// RelativeTime renders how long ago ts was, in the largest sensible unit ("3d ago").
func RelativeTime(ts string) string {
	t, ok := parseTime(ts)
	if !ok {
		return ts
	}
	sec := int(time.Since(t).Seconds())
	if sec < 60 {
		return fmt.Sprintf("%ds ago", sec)
	}
	min := sec / 60
	if min < 60 {
		return fmt.Sprintf("%dm ago", min)
	}
	hr := min / 60
	if hr < 24 {
		return fmt.Sprintf("%dh ago", hr)
	}
	day := hr / 24
	if day < 30 {
		return fmt.Sprintf("%dd ago", day)
	}
	mo := day / 30
	if mo < 12 {
		return fmt.Sprintf("%dmo ago", mo)
	}
	return fmt.Sprintf("%dy ago", mo/12)
}
