package handlers

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

func HandleGit(args []string, ultra bool) Result {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("git", args)
	raw := combined(r)
	filtered := ""

	switch sub {
	case "status", "st":
		filtered = formatGitStatus(r.Stdout, ultra)
	case "diff":
		filtered = compactDiff(r.Stdout, ultra)
	case "log":
		filtered = formatGitLog(r.Stdout, ultra)
	case "push":
		filtered = compactPush(raw)
	case "pull":
		filtered = compactPull(raw)
	case "add":
		filtered = "ok"
		if ultra {
			filtered = "✓"
		}
		if r.ExitCode != 0 {
			if s := strings.TrimSpace(firstNonEmpty(r.Stderr, raw)); s != "" {
				filtered = s
			}
		}
	case "commit":
		filtered = compactCommit(raw, ultra)
	case "branch":
		filtered = compactBranch(r.Stdout, ultra)
	case "fetch":
		if ultra {
			filtered = "✓ fetch"
		} else {
			filtered = "ok: fetched"
		}
	default:
		filtered = util.Truncate(strings.TrimSpace(util.StripAnsi(raw)), 50)
	}

	return finalize(filtered, r, raw, "git "+sub)
}

var (
	gitTreeCleanRe    = regexp.MustCompile(`(?i)working tree clean`)
	gitStatusChangeRe = regexp.MustCompile(`^\s*(modified|new file|deleted|renamed):\s+(.+)`)
	gitPorcelainRe    = regexp.MustCompile(`^[ MADRCU?][ MADRCU?]\s`)
	gitUntrackedRe    = regexp.MustCompile(`^\?\?\s+`)
	gitIndentedRe     = regexp.MustCompile(`^\s{2,}\S`)
)

func formatGitStatus(raw string, ultra bool) string {
	clean := util.StripAnsi(raw)
	if gitTreeCleanRe.MatchString(clean) || strings.TrimSpace(clean) == "" {
		if ultra {
			return "clean"
		}
		return "nothing to commit, working tree clean"
	}

	counts := map[string]int{"modified": 0, "new file": 0, "deleted": 0, "renamed": 0, "untracked": 0}
	for _, line := range strings.Split(clean, "\n") {
		if m := gitStatusChangeRe.FindStringSubmatch(line); m != nil {
			counts[m[1]]++
			continue
		}
		if gitUntrackedRe.MatchString(line) {
			counts["untracked"]++
		}
	}

	// Porcelain fallback (M/A/D/R/?? codes) when the long form matched nothing.
	var porcelain []string
	for _, l := range strings.Split(clean, "\n") {
		if gitPorcelainRe.MatchString(l) {
			porcelain = append(porcelain, l)
		}
	}
	if len(porcelain) > 0 && counts["modified"]+counts["new file"]+counts["deleted"]+counts["untracked"] == 0 {
		for _, line := range porcelain {
			code := line[:2]
			switch {
			case code == "??":
				counts["untracked"]++
			case strings.ContainsRune(code, 'M'):
				counts["modified"]++
			case strings.ContainsRune(code, 'A'):
				counts["new file"]++
			case strings.ContainsRune(code, 'D'):
				counts["deleted"]++
			case strings.ContainsRune(code, 'R'):
				counts["renamed"]++
			}
		}
	}

	// Count files listed under "Untracked files:". RE2 has no lookahead, so we slice by
	// index up to the next blank line / "Changes" header instead of a lookahead regex.
	if sec := untrackedSection(clean); sec != "" {
		n := 0
		for _, l := range strings.Split(sec, "\n") {
			if (strings.HasPrefix(l, "\t") || gitIndentedRe.MatchString(l)) && !strings.Contains(l, "(use ") {
				n++
			}
		}
		if n > counts["untracked"] {
			counts["untracked"] = n
		}
	}

	type part struct {
		n           int
		short, long string
	}
	order := []part{
		{counts["modified"], "M", "modified"},
		{counts["new file"], "N", "new file"},
		{counts["deleted"], "D", "deleted"},
		{counts["renamed"], "R", "renamed"},
		{counts["untracked"], "U", "untracked"},
	}
	var parts []string
	for _, p := range order {
		if p.n == 0 {
			continue
		}
		if ultra {
			parts = append(parts, fmt.Sprintf("%d%s", p.n, p.short))
		} else {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.long))
		}
	}
	if len(parts) == 0 {
		if ultra {
			return "clean"
		}
		return "nothing to commit, working tree clean"
	}
	if ultra {
		return strings.Join(parts, " ")
	}
	return strings.Join(parts, ", ")
}

func untrackedSection(clean string) string {
	idx := strings.Index(clean, "Untracked files:")
	if idx < 0 {
		return ""
	}
	rest := clean[idx:]
	end := len(rest)
	if i := strings.Index(rest, "\n\n"); i >= 0 && i < end {
		end = i
	}
	if i := strings.Index(rest, "\nChanges"); i >= 0 && i < end {
		end = i
	}
	return rest[:end]
}

func compactDiff(raw string, ultra bool) string {
	clean := util.StripAnsi(raw)
	files, added, removed := 0, 0, 0
	for _, line := range strings.Split(clean, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			files++
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			added++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			removed++
		}
	}
	if files == 0 && added == 0 && removed == 0 {
		if ultra {
			return "0"
		}
		return "no changes"
	}
	if ultra {
		return fmt.Sprintf("+%d-%d/%df", added, removed, files)
	}
	return fmt.Sprintf("%d %s: +%d/-%d", files, plural(files, "file", "files"), added, removed)
}

var (
	gitCommitRe      = regexp.MustCompile(`^commit\s+([0-9a-f]+)`)
	gitDateRe        = regexp.MustCompile(`^Date:\s+(.+)`)
	gitAuthorMergeRe = regexp.MustCompile(`^Author:|^Date:|^Merge:`)
	gitOnelineRe     = regexp.MustCompile(`^([0-9a-f]{7,40})\s+(.+)`)
)

func formatGitLog(raw string, ultra bool) string {
	clean := util.StripAnsi(raw)
	lines := strings.Split(clean, "\n")

	type entry struct{ hash, subject, date string }
	var cur entry
	var out []string
	flush := func() {
		if cur.hash != "" {
			out = append(out, formatLogEntry(cur.hash, cur.subject, cur.date, ultra))
		}
	}

	for _, line := range lines {
		if m := gitCommitRe.FindStringSubmatch(line); m != nil {
			flush()
			cur = entry{hash: shortHash7(m[1])}
			continue
		}
		if dm := gitDateRe.FindStringSubmatch(line); dm != nil {
			cur.date = strings.TrimSpace(dm[1])
			continue
		}
		if cur.hash != "" && cur.subject == "" {
			if subject := strings.TrimSpace(line); subject != "" && !gitAuthorMergeRe.MatchString(line) {
				cur.subject = subject
			}
		}
	}
	flush()

	if len(out) == 0 { // maybe --oneline
		for _, line := range lines {
			if m := gitOnelineRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				out = append(out, shortHash7(m[1])+" "+m[2])
			}
		}
	}
	return strings.Join(out, "\n")
}

func formatLogEntry(hash, subject, date string, ultra bool) string {
	if ultra {
		return hash + " " + subject
	}
	if date != "" {
		return hash + " " + subject + " (" + shortRelative(date) + ")"
	}
	return hash + " " + subject
}

func shortHash7(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// shortRelative renders a git date (or ISO date) as "3d ago". Git's default log date isn't
// RFC3339, so we try its layout first.
func shortRelative(s string) string {
	var t time.Time
	ok := false
	for _, layout := range []string{"Mon Jan _2 15:04:05 2006 -0700", time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, s); err == nil {
			t, ok = parsed, true
			break
		}
	}
	if !ok {
		return s
	}
	sec := int(time.Since(t).Seconds())
	switch {
	case sec < 60:
		return fmt.Sprintf("%ds ago", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm ago", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%dh ago", sec/3600)
	}
	d := sec / 86400
	if d < 30 {
		return fmt.Sprintf("%dd ago", d)
	}
	mo := d / 30
	if mo < 12 {
		return fmt.Sprintf("%dmo ago", mo)
	}
	return fmt.Sprintf("%dy ago", mo/12)
}

var (
	pushRefRe    = regexp.MustCompile(`\b([\w./-]+)\s*->\s*([\w./-]+)`)
	pushUpToDate = regexp.MustCompile(`(?i)up-to-date|Everything up-to-date`)
)

func compactPush(raw string) string {
	clean := util.StripAnsi(raw)
	if m := pushRefRe.FindStringSubmatch(clean); m != nil {
		return "ok " + m[1] + "→" + m[2]
	}
	if pushUpToDate.MatchString(clean) {
		return "ok: up-to-date"
	}
	return "ok"
}

var (
	pullUpToDateRe = regexp.MustCompile(`(?i)Already up.to.date`)
	pullStatRe     = regexp.MustCompile(`(\d+)\s+files?\s+changed.*?(\d+)\s+insertions?.*?(\d+)\s+deletions?`)
	pullCommitsRe  = regexp.MustCompile(`Fast-forward|Merging|(\d+)\s+commit`)
	pullCommitNRe  = regexp.MustCompile(`(\d+)\s+commit`)
)

func compactPull(raw string) string {
	clean := util.StripAnsi(raw)
	if pullUpToDateRe.MatchString(clean) {
		return "ok: up-to-date"
	}
	if stat := pullStatRe.FindStringSubmatch(clean); stat != nil {
		n := "?"
		if cm := pullCommitNRe.FindStringSubmatch(clean); cm != nil {
			n = cm[1]
		}
		return fmt.Sprintf("ok: %s commits +%s-%s", n, stat[2], stat[3])
	}
	if pullCommitsRe.MatchString(clean) {
		return "ok: pulled"
	}
	return "ok"
}

var (
	commitLineRe        = regexp.MustCompile(`\[\S+\s+([0-9a-f]+)\]\s*(.*)`)
	commitNothingRe     = regexp.MustCompile(`(?i)nothing to commit`)
	branchCurrentLineRe = regexp.MustCompile(`^\*\s+(.+)`)
)

func compactCommit(raw string, ultra bool) string {
	clean := util.StripAnsi(raw)
	if m := commitLineRe.FindStringSubmatch(clean); m != nil {
		if ultra {
			return "✓ " + shortHash7(m[1])
		}
		return "ok " + shortHash7(m[1]) + ": " + m[2]
	}
	if commitNothingRe.MatchString(clean) {
		if ultra {
			return "clean"
		}
		return "nothing to commit"
	}
	if ultra {
		return "✓"
	}
	return "ok"
}

func compactBranch(raw string, ultra bool) string {
	clean := util.StripAnsi(raw)
	var lines []string
	for _, l := range strings.Split(clean, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	current := ""
	for _, line := range lines {
		if m := branchCurrentLineRe.FindStringSubmatch(line); m != nil {
			current = strings.TrimSpace(m[1])
		}
	}
	if ultra {
		return fmt.Sprintf("*%s (%db)", current, len(lines))
	}
	return fmt.Sprintf("current: %s | %d branches total", current, len(lines))
}
