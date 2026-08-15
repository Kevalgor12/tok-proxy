// Package handlers holds the per-command output compressors. Each handler runs the real
// command, shrinks its output, and returns a Result the CLI records and prints.
package handlers

import (
	"strconv"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/run"
)

type Result struct {
	Filtered string
	Exit     int
	Raw      string
	CmdType  string
	ExecMs   int
}

// combined joins stdout and stderr the way the handlers treat their "raw" input.
func combined(r run.Result) string {
	if r.Stderr != "" {
		return r.Stdout + "\n" + r.Stderr
	}
	return r.Stdout
}

// finalize applies the shared fallback: an empty filtered output becomes "ok" on success,
// or the error text on failure.
func finalize(filtered string, r run.Result, raw, cmdType string) Result {
	if filtered == "" {
		if r.ExitCode == 0 {
			filtered = "ok"
		} else {
			filtered = strings.TrimSpace(firstNonEmpty(r.Stderr, raw))
		}
	}
	return Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: cmdType, ExecMs: r.ExecMs}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// limit returns at most the first n elements.
func limit[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// lastN returns at most the last n elements.
func lastN[T any](s []T, n int) []T {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
