package handlers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// jestJSON is the subset of jest/vitest --json output we read for a pass/fail summary.
type jestJSON struct {
	NumTotalTests  int `json:"numTotalTests"`
	NumPassedTests int `json:"numPassedTests"`
	NumFailedTests int `json:"numFailedTests"`
	TestResults    []struct {
		TestFilePath     string `json:"testFilePath"`
		Name             string `json:"name"`
		AssertionResults []struct {
			AncestorTitles  []string `json:"ancestorTitles"`
			Title           string   `json:"title"`
			FullName        string   `json:"fullName"`
			Status          string   `json:"status"`
			FailureMessages []string `json:"failureMessages"`
		} `json:"assertionResults"`
	} `json:"testResults"`
}

// HandleTestRunner compresses jest / vitest / mocha output, preferring their JSON reporter
// (asking for it when absent) and falling back to regex parsing of the human output.
func HandleTestRunner(runner string, args []string, ultra bool) Result {
	cmdArgs := append([]string(nil), args...)
	if runner == "jest" && !contains(cmdArgs, "--json") {
		cmdArgs = append([]string{"--json"}, cmdArgs...)
	} else if runner == "vitest" && !contains(cmdArgs, "--reporter=json") {
		rest := make([]string, 0, len(cmdArgs))
		for _, a := range cmdArgs {
			if a != "run" {
				rest = append(rest, a)
			}
		}
		cmdArgs = append([]string{"run", "--reporter=json"}, rest...)
	}

	r := run.Run(runner, cmdArgs)
	raw := combined(r)
	filtered := ""
	if parsed := tryParseJestJSON(r.Stdout); parsed != nil {
		filtered = formatFromJSON(parsed, ultra, r.ExecMs)
	} else {
		filtered = formatFromRegex(raw, ultra, r.ExecMs)
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
	return Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: runner, ExecMs: r.ExecMs}
}

func tryParseJestJSON(stdout string) *jestJSON {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return nil
	}
	// Some runners prefix log lines before the JSON; start at the first "{".
	start := strings.IndexByte(trimmed, '{')
	if start < 0 {
		return nil
	}
	var j jestJSON
	if json.Unmarshal([]byte(trimmed[start:]), &j) != nil {
		return nil
	}
	return &j
}

func formatFromJSON(j *jestJSON, ultra bool, execMs int) string {
	total, passed, failed := j.NumTotalTests, j.NumPassedTests, j.NumFailedTests
	if failed == 0 && total > 0 {
		if ultra {
			return fmt.Sprintf("✓%d", total)
		}
		return fmt.Sprintf("✓ All %d tests passed (%dms)", total, execMs)
	}
	if ultra {
		return fmt.Sprintf("✗%d/%d", failed, total)
	}

	var failures []string
	for _, tr := range j.TestResults {
		file := firstNonEmpty(tr.TestFilePath, firstNonEmpty(tr.Name, "unknown"))
		for _, a := range tr.AssertionResults {
			if a.Status != "failed" {
				continue
			}
			ancestors := strings.Join(a.AncestorTitles, " > ")
			title := firstNonEmpty(a.Title, a.FullName)
			path := title
			if ancestors != "" {
				path = ancestors + " > " + title
			}
			msg := strings.Join(limit(strings.Split(strings.Join(a.FailureMessages, "\n"), "\n"), 4), "\n")
			failures = append(failures, fmt.Sprintf("%s > %s\n  %s", file, path, strings.ReplaceAll(msg, "\n", "\n  ")))
		}
	}

	out := []string{fmt.Sprintf("%d failed %s:", failed, plural(failed, "test", "tests")), ""}
	for _, f := range limit(failures, 20) {
		out = append(out, f, "")
	}
	out = append(out, fmt.Sprintf("Summary: %d passed, %d failed (%d total)", passed, failed, total))
	return strings.TrimSpace(strings.Join(out, "\n"))
}

var (
	trTotalRe = regexp.MustCompile(`(?i)Tests?:\s*(?:(\d+)\s+failed,\s*)?(\d+)\s+passed,\s*(\d+)\s+total`)
	trFailRe  = regexp.MustCompile(`(?m)^(FAIL|×|✗)\s+(.+)$`)
)

func formatFromRegex(raw string, ultra bool, execMs int) string {
	clean := util.StripAnsi(raw)
	failed, passed, total := 0, 0, 0
	if m := trTotalRe.FindStringSubmatch(clean); m != nil {
		failed, passed, total = atoi(m[1]), atoi(m[2]), atoi(m[3])
	}
	failBlocks := trFailRe.FindAllString(clean, -1)

	if failed == 0 && len(failBlocks) == 0 {
		if total > 0 {
			if ultra {
				return fmt.Sprintf("✓%d", total)
			}
			return fmt.Sprintf("✓ All %d tests passed (%dms)", total, execMs)
		}
		if ultra {
			return "✓"
		}
		return "✓ tests passed"
	}

	failedCount := failed
	if failedCount == 0 {
		failedCount = len(failBlocks)
	}
	if ultra {
		totalStr := "?"
		if total > 0 {
			totalStr = fmt.Sprintf("%d", total)
		}
		return fmt.Sprintf("✗%d/%s", failedCount, totalStr)
	}

	out := []string{fmt.Sprintf("%d failed %s:", failedCount, plural(failed, "test", "tests")), ""}
	out = append(out, limit(failBlocks, 10)...)
	out = append(out, "", fmt.Sprintf("Summary: %d passed, %d failed (%d total)", passed, failedCount, total))
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
