package util

import "testing"

func TestFormatNumber(t *testing.T) {
	for in, want := range map[int]string{0: "0", 42: "42", 1000: "1,000", 1234567: "1,234,567", -1500: "-1,500"} {
		if got := FormatNumber(in); got != want {
			t.Errorf("FormatNumber(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestDollar(t *testing.T) {
	for in, want := range map[float64]string{0: "$0.00", 0.001: "$0.0010", 2.5: "$2.50"} {
		if got := Dollar(in); got != want {
			t.Errorf("Dollar(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	for in, want := range map[int]string{512: "512 B", 2048: "2.0 KB", 5242880: "5.0 MB"} {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("a\nb\nc\nd", 2); got != "a\nb\n[+2 more lines]" {
		t.Errorf("Truncate over limit = %q", got)
	}
	if got := Truncate("a\nb", 5); got != "a\nb" {
		t.Errorf("Truncate under limit = %q", got)
	}
}

func TestWithinDays(t *testing.T) {
	if !WithinDays(NowIso(), 1) {
		t.Error("now should be within 1 day")
	}
	if WithinDays("2000-01-01T00:00:00.000Z", 7) {
		t.Error("year 2000 should not be within 7 days")
	}
	if WithinDays("not-a-date", 7) {
		t.Error("unparseable timestamp should be false")
	}
}

func TestStripAnsi(t *testing.T) {
	if got := StripAnsi("\x1b[31mred\x1b[0m"); got != "red" {
		t.Errorf("StripAnsi = %q, want %q", got, "red")
	}
}
