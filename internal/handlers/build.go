package handlers

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// Compilers and non-JS linters. They all emit diagnostics of the shape
// "error[...]" / "warning: ..." / "path:line:col: message". We count them, group by
// code/rule, and surface the first handful of real errors.

func HandleRuff(args []string, ultra bool) Result {
	return diagnosticHandler("ruff", "ruff", args, ultra)
}
func HandleGolangciLint(args []string, ultra bool) Result {
	return diagnosticHandler("golangci-lint", "golangci-lint", args, ultra)
}
func HandleRubocop(args []string, ultra bool) Result {
	return diagnosticHandler("rubocop", "rubocop", args, ultra)
}
func HandleNext(args []string, ultra bool) Result {
	return diagnosticHandler("next", "next", args, ultra)
}

func diagnosticHandler(bin, cmdType string, args []string, ultra bool) Result {
	r := run.Run(bin, args)
	raw := combined(r)
	filtered := summarizeDiagnostics(raw, cmdType, ultra, r.ExitCode)
	return Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: cmdType, ExecMs: r.ExecMs}
}

type diag struct {
	code    string
	file    string
	message string
}

var (
	diagErrWarnRe = regexp.MustCompile(`^(error|warning)(?:\[([A-Z]\d+)\])?:\s+(.+)`)
	diagPathRe    = regexp.MustCompile(`^(.+?):(\d+):(\d+):?\s+(?:([A-Z]\d+|[A-Z]/\w+)\s+)?(.+)`)
	diagFileExtRe = regexp.MustCompile(`\.\w+$`)
	diagWarnRe    = regexp.MustCompile(`(?i)\bwarn`)
	diagOkRe      = regexp.MustCompile(`(?i)(compiled successfully|no issues|0 offenses|Finished|Found 0 errors|test result: ok)`)
)

// summarizeDiagnostics is the shared diagnostic folder, reused by the Go/Rust handlers.
func summarizeDiagnostics(raw, tool string, ultra bool, exitCode int) string {
	clean := util.StripAnsi(raw)
	lines := strings.Split(clean, "\n")
	var errors []diag
	warnings := 0

	for _, line := range lines {
		// rustc / cargo: "error[E0308]: mismatched types" or "error: ..."
		if m := diagErrWarnRe.FindStringSubmatch(line); m != nil {
			if m[1] == "warning" {
				warnings++
				continue
			}
			errors = append(errors, diag{code: firstNonEmpty(m[2], "error"), message: strings.TrimSpace(m[3])})
			continue
		}
		// path:line:col: message  (ruff / golangci-lint / go build / rubocop)
		if m := diagPathRe.FindStringSubmatch(line); m != nil && diagFileExtRe.MatchString(m[1]) {
			if diagWarnRe.MatchString(m[5]) {
				warnings++
				continue
			}
			errors = append(errors, diag{code: firstNonEmpty(m[4], "lint"), file: m[1], message: clip(strings.TrimSpace(m[5]), 100)})
		}
	}

	// Fall back to the tool's own summary line if we parsed nothing structured.
	if len(errors) == 0 && warnings == 0 {
		if exitCode == 0 || diagOkRe.MatchString(clean) {
			if ultra {
				return "✓"
			}
			return "✓ " + tool + ": clean"
		}
		tail := lastN(nonEmptyLines(clean), 8)
		if ultra {
			return "✗ " + tool
		}
		if len(tail) > 0 {
			return strings.Join(tail, "\n")
		}
		return "✗ " + tool + " failed"
	}

	if ultra {
		if warnings > 0 {
			return fmt.Sprintf("%dE/%dW", len(errors), warnings)
		}
		return fmt.Sprintf("%dE", len(errors))
	}

	var codeOrder []string
	byCode := map[string]int{}
	for _, e := range errors {
		if _, ok := byCode[e.code]; !ok {
			codeOrder = append(codeOrder, e.code)
		}
		byCode[e.code]++
	}
	type codeCount struct {
		code string
		n    int
	}
	grouped := make([]codeCount, len(codeOrder))
	for i, c := range codeOrder {
		grouped[i] = codeCount{c, byCode[c]}
	}
	sort.SliceStable(grouped, func(i, j int) bool { return grouped[i].n > grouped[j].n })

	head := fmt.Sprintf("%d %s", len(errors), plural(len(errors), "error", "errors"))
	if warnings > 0 {
		head += fmt.Sprintf(", %d %s", warnings, plural(warnings, "warning", "warnings"))
	}
	head += fmt.Sprintf(" (%s):", tool)

	out := []string{head}
	if len(grouped) > 1 {
		out = append(out, "", "By code:")
		for _, g := range limit(grouped, 8) {
			out = append(out, fmt.Sprintf("  %s: %d", g.code, g.n))
		}
	}
	out = append(out, "")
	for _, e := range limit(errors, 12) {
		prefix := ""
		if e.file != "" {
			prefix = e.file + ": "
		}
		out = append(out, fmt.Sprintf("  %s[%s] %s", prefix, e.code, e.message))
	}
	if len(errors) > 12 {
		out = append(out, fmt.Sprintf("  … +%d more", len(errors)-12))
	}
	return strings.Join(out, "\n")
}
