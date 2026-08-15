package handlers

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// JS/TS linters: eslint, biome, prettier. Their per-file violation lists collapse to
// counts grouped by rule and by file, keeping the worst offenders visible.

type violation struct {
	file    string
	line    int
	rule    string
	message string
}

func HandleLint(linter string, args []string, ultra bool) Result {
	r := run.Run(linter, args)
	raw := combined(r)
	filtered := groupViolations(raw, ultra)
	if filtered == "" {
		if r.ExitCode == 0 {
			if ultra {
				filtered = "✓"
			} else {
				filtered = "✓ no issues"
			}
		} else {
			filtered = raw
		}
	}
	return Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: linter, ExecMs: r.ExecMs}
}

var (
	lintFileRe    = regexp.MustCompile(`^(/|[A-Za-z]:\\|\./|\.\./|[\w./-]+\.[\w]+)$`)
	lintFileExtRe = regexp.MustCompile(`(?i)\.[a-z]+$`)
	lintColRe     = regexp.MustCompile(`:\d+:\d+`)
	lintCompactRe = regexp.MustCompile(`^\s*(\d+):(\d+)\s+(?:error|warning)\s+(.+?)\s+([@\w/-]+)\s*$`)
	lintInlineRe  = regexp.MustCompile(`^(.+?):(\d+):(\d+)\s+(?:error|warning)\s+(.+?)\s+([@\w/-]+)\s*$`)
	lintStyleRe   = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s+(.+?)\s+\[([@\w/-]+)\]\s*$`)
)

func parseViolations(raw string) []violation {
	lines := strings.Split(util.StripAnsi(raw), "\n")
	var out []violation
	currentFile := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Header: a bare file path.
		if lintFileRe.MatchString(trimmed) && lintFileExtRe.MatchString(trimmed) && !lintColRe.MatchString(line) {
			currentFile = trimmed
			continue
		}
		// ESLint compact format: "  line:col  error  message  rule"
		if m := lintCompactRe.FindStringSubmatch(line); m != nil && currentFile != "" {
			out = append(out, violation{file: currentFile, line: atoi(m[1]), rule: m[4], message: strings.TrimSpace(m[3])})
			continue
		}
		// Inline: "file:line:col error/warning message rule"
		if m := lintInlineRe.FindStringSubmatch(line); m != nil {
			out = append(out, violation{file: m[1], line: atoi(m[2]), rule: m[5], message: strings.TrimSpace(m[4])})
			continue
		}
		// Stylish-ish: "file:line:col: message [rule]"
		if m := lintStyleRe.FindStringSubmatch(line); m != nil {
			out = append(out, violation{file: m[1], line: atoi(m[2]), rule: m[5], message: m[4]})
		}
	}
	return out
}

func groupViolations(raw string, ultra bool) string {
	violations := parseViolations(raw)
	total := len(violations)
	if total == 0 {
		if ultra {
			return "✓"
		}
		return "✓ no issues"
	}

	byRule, ruleOrder := groupBy(violations, func(v violation) string { return v.rule })
	byFile, fileOrder := groupBy(violations, func(v violation) string { return v.file })
	sortedRules := sortByCountDesc(ruleOrder, byRule)
	sortedFiles := sortByCountDesc(fileOrder, byFile)

	if ultra {
		var topRules []string
		for _, rule := range limit(sortedRules, 3) {
			topRules = append(topRules, fmt.Sprintf("%s:%d", shortRule(rule), len(byRule[rule])))
		}
		return fmt.Sprintf("%dV %dF | %s", total, len(byFile), strings.Join(topRules, " "))
	}

	out := []string{
		fmt.Sprintf("%d %s in %d %s",
			total, plural(total, "violation", "violations"), len(byFile), plural(len(byFile), "file", "files")),
		"",
		"By rule:",
	}
	for _, rule := range limit(sortedRules, 10) {
		n := len(byRule[rule])
		pct := float64(n) / float64(total) * 100
		out = append(out, fmt.Sprintf("  %s: %d (%.1f%%)", rule, n, pct))
	}
	out = append(out, "", "Top files:")
	for _, file := range limit(sortedFiles, 5) {
		n := len(byFile[file])
		out = append(out, fmt.Sprintf("  %s: %d %s", file, n, plural(n, "violation", "violations")))
	}
	return strings.Join(out, "\n")
}

// groupBy buckets violations by a key, returning the buckets and the first-seen key order.
func groupBy(vs []violation, key func(violation) string) (map[string][]violation, []string) {
	m := map[string][]violation{}
	var order []string
	for _, v := range vs {
		k := key(v)
		if _, ok := m[k]; !ok {
			order = append(order, k)
		}
		m[k] = append(m[k], v)
	}
	return m, order
}

// sortByCountDesc stably orders keys by bucket size, largest first (ties keep first-seen order).
func sortByCountDesc(order []string, m map[string][]violation) []string {
	sorted := append([]string(nil), order...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(m[sorted[i]]) > len(m[sorted[j]]) })
	return sorted
}

func shortRule(rule string) string {
	parts := strings.Split(rule, "/")
	return parts[len(parts)-1]
}
