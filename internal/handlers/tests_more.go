package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// Test runners that emit human-readable output (no stable JSON): pytest, rspec,
// minitest (rake test), playwright. Each is collapsed to a pass/fail summary plus
// the failing test names - the only part an agent needs to act on.

func HandleMoreTests(runner string, args []string, ultra bool) Result {
	bin := runner
	if runner == "rake" {
		bin = "rake"
	}
	r := run.Run(bin, args)
	raw := combined(r)
	var filtered string
	switch runner {
	case "pytest":
		filtered = summarizePytest(raw, ultra, r.ExecMs)
	case "rspec":
		filtered = summarizeRspec(raw, ultra, r.ExecMs)
	case "rake":
		filtered = summarizeMinitest(raw, ultra, r.ExecMs)
	default:
		filtered = summarizePlaywright(raw, ultra, r.ExecMs)
	}
	if filtered == "" {
		if r.ExitCode == 0 {
			if ultra {
				filtered = "✓"
			} else {
				filtered = "✓ tests passed"
			}
		} else {
			filtered = raw
		}
	}
	cmdType := runner
	if runner == "rake" {
		cmdType = "rake test"
	}
	return Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: cmdType, ExecMs: r.ExecMs}
}

var (
	tmFailedRe   = regexp.MustCompile(`(\d+)\s+failed`)
	tmPassedRe   = regexp.MustCompile(`(\d+)\s+passed`)
	tmErrorRe    = regexp.MustCompile(`(\d+)\s+error`)
	tmFlakyRe    = regexp.MustCompile(`(\d+)\s+flaky`)
	pyFailNameRe = regexp.MustCompile(`(?m)^FAILED\s+(\S+)`)
	rspecCntRe   = regexp.MustCompile(`(\d+)\s+examples?,\s+(\d+)\s+failures?`)
	rspecNameRe  = regexp.MustCompile(`(?m)^rspec\s+(\S+)\s+#\s+(.+)$`)
	miniCntRe    = regexp.MustCompile(`(\d+)\s+runs?,\s+\d+\s+assertions?,\s+(\d+)\s+failures?,\s+(\d+)\s+errors?`)
	miniNameRe   = regexp.MustCompile(`(?m)^\s*\d+\)\s+(Failure|Error):\s*\n\s*(.+?)(?:\s|$)`)
	pwNameRe     = regexp.MustCompile(`(?m)^\s*[✘✗×]\s+(?:\d+\s+)?(.+?)(?:\s+\([\d.]+m?s\))?$`)
)

func passLine(passed int, ultra bool, execMs int) string {
	if ultra {
		return fmt.Sprintf("✓%d", passed)
	}
	return fmt.Sprintf("✓ All %d tests passed (%dms)", passed, execMs)
}

func failBlock(failed, passed, total int, names []string, ultra bool) string {
	if ultra {
		return fmt.Sprintf("✗%d/%d", failed, nonZero(total, passed+failed))
	}
	out := []string{fmt.Sprintf("%d failed %s:", failed, plural(failed, "test", "tests")), ""}
	for _, n := range limit(names, 20) {
		out = append(out, "  ✗ "+n)
	}
	summary := fmt.Sprintf("Summary: %d passed, %d failed", passed, failed)
	if total != 0 {
		summary += fmt.Sprintf(" (%d total)", total)
	}
	out = append(out, "", summary)
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func summarizePytest(raw string, ultra bool, execMs int) string {
	clean := util.StripAnsi(raw)
	failed := firstGroupInt(tmFailedRe, clean)
	passed := firstGroupInt(tmPassedRe, clean)
	errors := firstGroupInt(tmErrorRe, clean)
	total := failed + passed + errors
	if failed == 0 && errors == 0 {
		return passLine(nonZero(passed, total), ultra, execMs)
	}
	var names []string
	for _, m := range pyFailNameRe.FindAllStringSubmatch(clean, -1) {
		names = append(names, m[1])
	}
	return failBlock(failed+errors, passed, total, names, ultra)
}

func summarizeRspec(raw string, ultra bool, execMs int) string {
	clean := util.StripAnsi(raw)
	examples, failures := 0, 0
	if m := rspecCntRe.FindStringSubmatch(clean); m != nil {
		examples, failures = atoi(m[1]), atoi(m[2])
	}
	if failures == 0 {
		return passLine(examples, ultra, execMs)
	}
	var names []string
	for _, m := range rspecNameRe.FindAllStringSubmatch(clean, -1) {
		names = append(names, fmt.Sprintf("%s (%s)", strings.TrimSpace(m[2]), m[1]))
	}
	return failBlock(failures, examples-failures, examples, names, ultra)
}

func summarizeMinitest(raw string, ultra bool, execMs int) string {
	clean := util.StripAnsi(raw)
	runs, failures, errors := 0, 0, 0
	if m := miniCntRe.FindStringSubmatch(clean); m != nil {
		runs, failures, errors = atoi(m[1]), atoi(m[2]), atoi(m[3])
	}
	if failures == 0 && errors == 0 {
		return passLine(runs, ultra, execMs)
	}
	var names []string
	for _, m := range miniNameRe.FindAllStringSubmatch(clean, -1) {
		names = append(names, strings.TrimSpace(m[2]))
	}
	return failBlock(failures+errors, runs-failures-errors, runs, names, ultra)
}

func summarizePlaywright(raw string, ultra bool, execMs int) string {
	clean := util.StripAnsi(raw)
	failed := firstGroupInt(tmFailedRe, clean)
	passed := firstGroupInt(tmPassedRe, clean)
	flaky := firstGroupInt(tmFlakyRe, clean)
	total := failed + passed + flaky
	if failed == 0 {
		if ultra {
			if flaky != 0 {
				return fmt.Sprintf("✓%d~%d", passed, flaky)
			}
			return fmt.Sprintf("✓%d", passed)
		}
		s := fmt.Sprintf("✓ %d passed", passed)
		if flaky != 0 {
			s += fmt.Sprintf(", %d flaky", flaky)
		}
		return s + fmt.Sprintf(" (%dms)", execMs)
	}
	var names []string
	for _, m := range pwNameRe.FindAllStringSubmatch(clean, -1) {
		names = append(names, strings.TrimSpace(m[1]))
	}
	return failBlock(failed, passed, total, names, ultra)
}

func nonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
