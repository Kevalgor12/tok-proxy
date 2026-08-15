package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// GitHub CLI. gh output is dense human tables; we collapse them to counts plus the
// leading identifier for each row so the model can still act on them.
func HandleGh(args []string, ultra bool) Result {
	sub, verb := "", ""
	if len(args) > 0 {
		sub = args[0]
	}
	if len(args) > 1 {
		verb = args[1]
	}
	r := run.Run("gh", args)
	raw := combined(r)
	var filtered string

	switch {
	case sub == "pr" && verb == "list":
		filtered = formatPrList(r.Stdout, ultra)
	case sub == "pr" && verb == "view":
		filtered = formatPrView(r.Stdout, ultra)
	case sub == "pr" && verb == "checks":
		filtered = formatChecks(r.Stdout, ultra)
	case sub == "issue" && verb == "list":
		filtered = formatIssueList(r.Stdout, ultra)
	case sub == "run" && verb == "list":
		filtered = formatRunList(r.Stdout, ultra)
	default:
		filtered = strings.TrimSpace(util.StripAnsi(raw))
	}

	cmdType := strings.TrimSpace("gh " + sub)
	if verb != "" {
		cmdType += " " + verb
	}
	if filtered == "" {
		if r.ExitCode == 0 {
			filtered = "ok"
		} else {
			filtered = strings.TrimSpace(firstNonEmpty(r.Stderr, raw))
		}
	}
	return Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: cmdType, ExecMs: r.ExecMs}
}

var (
	ghShowingRe  = regexp.MustCompile(`(?i)^Showing\b`)
	ghRunOkRe    = regexp.MustCompile(`(?i)\b(completed\s+success|✓|success)\b`)
	ghRunFailRe  = regexp.MustCompile(`(?i)\b(failure|✗|X|cancelled|timed_out)\b`)
	ghRunStatRe  = regexp.MustCompile(`(?i)success|failure|cancelled|in_progress|queued`)
	ghDigitsRe   = regexp.MustCompile(`^\d+$`)
	ghPassRe     = regexp.MustCompile(`(?i)\bpass\b|✓`)
	ghFailRe     = regexp.MustCompile(`(?i)\bfail\b|✗|X`)
	ghPendingRe  = regexp.MustCompile(`(?i)\bpending\b|in_progress`)
	ghTitleRe    = regexp.MustCompile(`(?im)^title:\s*(.+)$`)
	ghStateRe    = regexp.MustCompile(`(?im)^state:\s*(.+)$`)
	ghReviewerRe = regexp.MustCompile(`(?im)^reviewers:\s*(.+)$`)
)

func tableRows(raw string) []string {
	var out []string
	for _, l := range strings.Split(util.StripAnsi(raw), "\n") {
		l = strings.TrimSuffix(l, "\r")
		if strings.TrimSpace(l) != "" && !ghShowingRe.MatchString(l) {
			out = append(out, l)
		}
	}
	return out
}

func formatPrList(raw string, ultra bool) string {
	rows := tableRows(raw)
	if len(rows) == 0 {
		if ultra {
			return "0 PRs"
		}
		return "no open pull requests"
	}
	if ultra {
		return fmt.Sprintf("%d PRs", len(rows))
	}
	out := []string{fmt.Sprintf("%d open %s:", len(rows), plural(len(rows), "PR", "PRs"))}
	for _, row := range limit(rows, 20) {
		cols := strings.Split(row, "\t")
		num := strings.TrimPrefix(strings.TrimSpace(col(cols, 0)), "#")
		title := clip(strings.TrimSpace(col(cols, 1)), 60)
		branch := strings.TrimSpace(col(cols, 2))
		line := fmt.Sprintf("  #%s %s", num, title)
		if branch != "" {
			line += " [" + branch + "]"
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func formatIssueList(raw string, ultra bool) string {
	rows := tableRows(raw)
	if len(rows) == 0 {
		if ultra {
			return "0 issues"
		}
		return "no open issues"
	}
	if ultra {
		return fmt.Sprintf("%d issues", len(rows))
	}
	out := []string{fmt.Sprintf("%d open %s:", len(rows), plural(len(rows), "issue", "issues"))}
	for _, row := range limit(rows, 20) {
		cols := strings.Split(row, "\t")
		num := strings.TrimPrefix(strings.TrimSpace(col(cols, 0)), "#")
		title := clip(strings.TrimSpace(firstNonEmpty(col(cols, 2), col(cols, 1))), 60)
		out = append(out, fmt.Sprintf("  #%s %s", num, title))
	}
	return strings.Join(out, "\n")
}

func formatRunList(raw string, ultra bool) string {
	rows := tableRows(raw)
	if len(rows) == 0 {
		if ultra {
			return "0 runs"
		}
		return "no workflow runs"
	}
	ok, fail, other := 0, 0, 0
	for _, r := range rows {
		switch {
		case ghRunOkRe.MatchString(r):
			ok++
		case ghRunFailRe.MatchString(r):
			fail++
		default:
			other++
		}
	}
	if ultra {
		s := fmt.Sprintf("%d✓/%d✗", ok, fail)
		if other != 0 {
			s += fmt.Sprintf("/%d?", other)
		}
		return s
	}
	head := fmt.Sprintf("%d %s: %d passed, %d failed", len(rows), plural(len(rows), "run", "runs"), ok, fail)
	if other != 0 {
		head += fmt.Sprintf(", %d in-progress", other)
	}
	out := []string{head}
	for _, row := range limit(rows, 10) {
		cols := strings.Split(row, "\t")
		for i, c := range cols {
			cols[i] = strings.TrimSpace(c)
		}
		status := ""
		for _, c := range cols {
			if ghRunStatRe.MatchString(c) {
				status = c
				break
			}
		}
		if status == "" {
			status = col(cols, 0)
		}
		name := ""
		for _, c := range cols {
			if c != "" && c != status && !ghDigitsRe.MatchString(c) {
				name = c
				break
			}
		}
		out = append(out, clip(fmt.Sprintf("  %s %s", status, name), 70))
	}
	return strings.Join(out, "\n")
}

func formatPrView(raw string, ultra bool) string {
	clean := util.StripAnsi(raw)
	title := submatch(ghTitleRe, clean)
	state := submatch(ghStateRe, clean)
	reviewers := submatch(ghReviewerRe, clean)
	if ultra {
		return clip(fmt.Sprintf("%s: %s", firstNonEmpty(state, "?"), title), 60)
	}
	var out []string
	if title != "" {
		out = append(out, title)
	}
	if state != "" {
		out = append(out, "state: "+state)
	}
	if reviewers != "" {
		out = append(out, "reviewers: "+reviewers)
	}
	if i := strings.Index(clean, "--\n"); i >= 0 {
		body := strings.TrimSpace(clean[i+3:])
		if body != "" {
			out = append(out, "", util.Truncate(body, 15))
		}
	}
	if len(out) == 0 {
		return util.Truncate(clean, 20)
	}
	return strings.Join(out, "\n")
}

func formatChecks(raw string, ultra bool) string {
	rows := tableRows(raw)
	pass, fail, pending := 0, 0, 0
	for _, r := range rows {
		switch {
		case ghPassRe.MatchString(r):
			pass++
		case ghFailRe.MatchString(r):
			fail++
		case ghPendingRe.MatchString(r):
			pending++
		}
	}
	if ultra {
		s := fmt.Sprintf("%d✓/%d✗", pass, fail)
		if pending != 0 {
			s += fmt.Sprintf("/%d⋯", pending)
		}
		return s
	}
	head := fmt.Sprintf("checks: %d passing, %d failing", pass, fail)
	if pending != 0 {
		head += fmt.Sprintf(", %d pending", pending)
	}
	out := []string{head}
	if fail > 0 {
		var failing []string
		for _, r := range rows {
			if ghFailRe.MatchString(r) {
				failing = append(failing, r)
			}
		}
		for _, r := range limit(failing, 10) {
			out = append(out, "  ✗ "+strings.TrimSpace(strings.Split(r, "\t")[0]))
		}
	}
	return strings.Join(out, "\n")
}

func col(cols []string, i int) string {
	if i < len(cols) {
		return cols[i]
	}
	return ""
}

func submatch(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}
