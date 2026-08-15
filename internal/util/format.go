package util

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ansiCodes  = regexp.MustCompile(`\x1b\[[0-9;]*[mGKHF]`)
	carriageRe = regexp.MustCompile(`\r[^\n]*`)
	spinnerRe  = regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]`)
)

// StripAnsi removes color/cursor escape codes, carriage-return progress redraws, and braille
// spinner frames - the noise that inflates command output without adding meaning.
func StripAnsi(raw string) string {
	raw = ansiCodes.ReplaceAllString(raw, "")
	raw = carriageRe.ReplaceAllString(raw, "")
	return spinnerRe.ReplaceAllString(raw, "")
}

// Truncate keeps the first maxLines lines and notes how many were dropped.
func Truncate(text string, maxLines int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	return strings.Join(lines[:maxLines], "\n") + fmt.Sprintf("\n[+%d more lines]", len(lines)-maxLines)
}

// EstimateTokens is a rough token count for a string (~4 chars per token).
func EstimateTokens(text string) int { return len(text) / 4 }

// BytesToTokens converts a byte count to estimated tokens without allocating a filler string.
func BytesToTokens(bytes int) int {
	if bytes < 0 {
		bytes = 0
	}
	return bytes / 4
}

func FormatBytes(bytes int) string {
	const kb, mb, gb = 1024, 1024 * 1024, 1024 * 1024 * 1024
	switch {
	case bytes < kb:
		return fmt.Sprintf("%d B", bytes)
	case bytes < mb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/kb)
	case bytes < gb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/mb)
	default:
		return fmt.Sprintf("%.2f GB", float64(bytes)/gb)
	}
}

// FormatNumber renders an int with thousands separators: 1234567 -> "1,234,567".
func FormatNumber(n int) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	digits := strconv.Itoa(n)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return sign + b.String()
}

func Dollar(n float64) string {
	switch {
	case n == 0:
		return "$0.00"
	case math.Abs(n) < 0.01:
		return fmt.Sprintf("$%.4f", n)
	default:
		return fmt.Sprintf("$%.2f", n)
	}
}

func Percent(n float64, digits int) string {
	return fmt.Sprintf("%.*f%%", digits, n)
}

// Pad left- or right-justifies s to width columns, measured in runes so box-drawing and
// other multi-byte glyphs still line up.
func Pad(s string, width int, alignRight bool) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	spaces := strings.Repeat(" ", width-n)
	if alignRight {
		return spaces + s
	}
	return s + spaces
}

// EscapeCsv quotes a field only when it contains a comma, quote, or newline.
func EscapeCsv(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}
