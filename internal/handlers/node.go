package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// HandleNode compresses npm / pnpm / yarn output.
func HandleNode(pm string, args []string, ultra bool) Result {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run(pm, args)
	raw := combined(r)
	filtered := ""

	switch {
	case sub == "install" || sub == "i" || sub == "add" || (pm == "yarn" && sub == ""):
		filtered = filterInstall(raw, ultra)
	case sub == "list" || sub == "ls":
		filtered = filterList(raw, ultra)
	case sub == "outdated":
		filtered = filterOutdated(raw, ultra)
	case sub == "run" || sub == "run-script" || (pm == "yarn" && sub != "install"):
		filtered = filterRun(raw, r.ExitCode, ultra)
	default:
		filtered = util.Truncate(strings.TrimSpace(util.StripAnsi(raw)), 30)
	}

	cmdType := pm + " " + sub
	if sub == "" {
		cmdType = pm + " cmd"
	}
	return finalize(filtered, r, raw, cmdType)
}

var (
	addedPkgsRe = regexp.MustCompile(`added\s+(\d+)\s+package`)
	pkgsInstRe  = regexp.MustCompile(`(\d+)\s+packages? installed`)
	doneInRe    = regexp.MustCompile(`Done in [\d.]+s`)
	plusPkgRe   = regexp.MustCompile(`\+\s+(\S+)@(\S+)`)
	addedLineRe = regexp.MustCompile(`^added\s+(\S+)@(\S+)`)
	npmErrRe    = regexp.MustCompile(`(?i)^npm ERR!|^pnpm ERR|^error `)
	npmWarnRe   = regexp.MustCompile(`(?i)^npm warn|^pnpm WARN|^warning `)
	majorVerRe  = regexp.MustCompile(`@(\d+)\.\d+\.\d+`)
	nonEmptyRe  = regexp.MustCompile(`\S`)
	listTopRe   = regexp.MustCompile(`^[├└]|^\S+@`)
	listNestRe  = regexp.MustCompile(`[│ ]{2}`)
	pkgHeaderRe = regexp.MustCompile(`(?i)^Package`)
)

func filterInstall(raw string, ultra bool) string {
	clean := util.StripAnsi(raw)
	lines := strings.Split(clean, "\n")

	total := 0
	if m := addedPkgsRe.FindStringSubmatch(clean); m != nil {
		total = atoi(m[1])
	} else if m := pkgsInstRe.FindStringSubmatch(clean); m != nil {
		total = atoi(m[1])
	}

	var direct []string
	for _, m := range plusPkgRe.FindAllStringSubmatch(clean, -1) {
		direct = append(direct, m[1]+"@"+m[2])
	}
	for _, line := range lines {
		if a := addedLineRe.FindStringSubmatch(strings.TrimSpace(line)); a != nil {
			direct = append(direct, a[1]+"@"+a[2])
		}
	}

	var errs, warns []string
	for _, line := range lines {
		if npmErrRe.MatchString(line) {
			errs = append(errs, strings.TrimSpace(line))
		}
		if npmWarnRe.MatchString(line) {
			warns = append(warns, strings.TrimSpace(line))
		}
	}
	uniqueWarnings := limit(util.Unique(warns), 5)
	uniqueDirect := limit(util.Unique(direct), 10)

	if len(errs) > 0 {
		if ultra {
			return fmt.Sprintf("✗%derr", len(errs))
		}
		return "✗ Install failed\n" + strings.Join(limit(errs, 5), "\n")
	}

	if ultra {
		tag := "✓ok"
		if total > 0 {
			tag = fmt.Sprintf("✓%d", total)
		}
		if len(uniqueDirect) > 0 {
			var compact []string
			for _, d := range limit(uniqueDirect, 3) {
				compact = append(compact, majorVerRe.ReplaceAllString(d, "@$1"))
			}
			return tag + " " + strings.Join(compact, " ")
		}
		return tag
	}

	var out []string
	switch {
	case total > 0:
		out = append(out, fmt.Sprintf("✓ Installed %d packages", total))
	case doneInRe.MatchString(clean):
		out = append(out, "✓ Done")
	default:
		out = append(out, "✓ ok")
	}
	if len(uniqueDirect) > 0 {
		out = append(out, "", "New: "+strings.Join(uniqueDirect, ", "))
	}
	if len(uniqueWarnings) > 0 {
		out = append(out, "", fmt.Sprintf("Warnings (%d):", len(uniqueWarnings)))
		for _, w := range uniqueWarnings {
			out = append(out, "  "+w)
		}
	}
	return strings.Join(out, "\n")
}

func filterList(raw string, ultra bool) string {
	clean := util.StripAnsi(raw)
	var top []string
	for _, line := range strings.Split(clean, "\n") {
		if !nonEmptyRe.MatchString(line) {
			continue
		}
		if listTopRe.MatchString(line) && !listNestRe.MatchString(line) {
			top = append(top, strings.TrimSpace(line))
		}
	}
	if ultra {
		return fmt.Sprintf("%d pkgs", len(top))
	}
	if len(top) == 0 {
		return util.Truncate(clean, 30)
	}
	return fmt.Sprintf("Top-level dependencies (%d):\n%s", len(top), strings.Join(limit(top, 30), "\n"))
}

func filterOutdated(raw string, ultra bool) string {
	clean := util.StripAnsi(raw)
	var lines []string
	for _, l := range strings.Split(clean, "\n") {
		if nonEmptyRe.MatchString(l) {
			lines = append(lines, l)
		}
	}
	var dataLines []string
	for i, l := range lines {
		if i == 0 || pkgHeaderRe.MatchString(l) {
			continue
		}
		dataLines = append(dataLines, l)
	}
	if ultra {
		return fmt.Sprintf("%d outdated", len(dataLines))
	}
	if len(dataLines) == 0 {
		return "✓ All up-to-date"
	}
	return fmt.Sprintf("%d outdated packages\n%s", len(dataLines), strings.Join(limit(dataLines, 20), "\n"))
}

func filterRun(raw string, exitCode int, ultra bool) string {
	clean := util.StripAnsi(raw)
	if exitCode == 0 {
		if ultra {
			return "✓"
		}
		var nonEmpty []string
		for _, l := range strings.Split(clean, "\n") {
			if strings.TrimSpace(l) != "" {
				nonEmpty = append(nonEmpty, l)
			}
		}
		if last := lastN(nonEmpty, 5); len(last) > 0 {
			return strings.Join(last, "\n")
		}
		return "✓ ok"
	}
	if ultra {
		return "✗"
	}
	return util.Truncate(clean, 30)
}
