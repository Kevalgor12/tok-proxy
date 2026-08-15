package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// Non-Node package managers and codegen: pip, uv, bundle, prisma, gem. Installs
// collapse to "N installed" plus any errors; listings to counts.

func HandlePip(args []string, ultra bool) Result {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("pip", args)
	raw := combined(r)
	var filtered string
	switch sub {
	case "install":
		filtered = summarizePipInstall(raw, ultra)
	case "list", "freeze":
		filtered = summarizeList(raw, ultra, "packages")
	default:
		filtered = strings.TrimSpace(util.StripAnsi(raw))
	}
	return finalizeRaw(filtered, r, raw, "pip "+firstNonEmpty(sub, "cmd"))
}

func HandleUv(args []string, ultra bool) Result {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("uv", args)
	raw := combined(r)
	var filtered string
	switch sub {
	case "sync", "pip", "add", "install":
		filtered = summarizeUvSync(raw, ultra, r.ExitCode)
	default:
		filtered = strings.TrimSpace(util.StripAnsi(raw))
	}
	return finalizeRaw(filtered, r, raw, "uv "+firstNonEmpty(sub, "cmd"))
}

var (
	bundleDoneRe  = regexp.MustCompile(`Bundle complete!\s+(\d+)\s+Gemfile dependencies,\s+(\d+)\s+gems now installed`)
	prismaGenRe   = regexp.MustCompile(`Generated Prisma Client\s+\(([^)]+)\)`)
	prismaMigRe   = regexp.MustCompile(`(?i)(Your database is now in sync|already in sync|migration.+applied)`)
	gemInstallRe  = regexp.MustCompile(`(\d+)\s+gems? installed`)
	pipErrorRe    = regexp.MustCompile(`^ERROR:`)
	pipSuccessRe  = regexp.MustCompile(`Successfully installed\s+(.+)`)
	pipSatisfyRe  = regexp.MustCompile(`(?i)already satisfied`)
	uvAddedRe     = regexp.MustCompile(`(?m)^\s*[+]\s+\S+`)
	uvRemovedRe   = regexp.MustCompile(`(?m)^\s*[-]\s+\S+`)
	listSkipRe    = regexp.MustCompile(`(?i)^-+\s|^Package\s`)
	tailErrRe     = regexp.MustCompile(`(?i)error|failed|cannot|not found`)
	whitespaceSep = regexp.MustCompile(`\s+`)
)

func HandleBundle(args []string, ultra bool) Result {
	sub := "install"
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("bundle", args)
	raw := combined(r)
	var filtered string
	if m := bundleDoneRe.FindStringSubmatch(util.StripAnsi(raw)); m != nil {
		if ultra {
			filtered = "✓" + m[2] + "gems"
		} else {
			filtered = fmt.Sprintf("✓ Bundle complete: %s gems (%s deps)", m[2], m[1])
		}
	} else if r.ExitCode != 0 {
		filtered = errorTail(raw)
	} else {
		filtered = strings.TrimSpace(util.StripAnsi(raw))
	}
	return finalizeRaw(filtered, r, raw, "bundle "+sub)
}

func HandlePrisma(args []string, ultra bool) Result {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("prisma", args)
	raw := combined(r)
	clean := util.StripAnsi(raw)
	var filtered string
	switch {
	case prismaGenRe.MatchString(clean):
		if ultra {
			filtered = "✓gen"
		} else {
			filtered = "✓ Generated Prisma Client (" + prismaGenRe.FindStringSubmatch(clean)[1] + ")"
		}
	case prismaMigRe.MatchString(clean):
		if ultra {
			filtered = "✓migrate"
		} else {
			filtered = "✓ " + prismaMigRe.FindStringSubmatch(clean)[1]
		}
	case r.ExitCode != 0:
		filtered = errorTail(raw)
	default:
		filtered = strings.TrimSpace(clean)
	}
	return finalizeRaw(filtered, r, raw, "prisma "+firstNonEmpty(sub, "cmd"))
}

func HandleGem(args []string, ultra bool) Result {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("gem", args)
	raw := combined(r)
	var filtered string
	switch {
	case gemInstallRe.MatchString(util.StripAnsi(raw)):
		n := gemInstallRe.FindStringSubmatch(util.StripAnsi(raw))[1]
		if ultra {
			filtered = "✓" + n + "gems"
		} else {
			filtered = "✓ " + n + " gems installed"
		}
	case sub == "list":
		filtered = summarizeList(raw, ultra, "gems")
	case r.ExitCode != 0:
		filtered = errorTail(raw)
	default:
		filtered = strings.TrimSpace(util.StripAnsi(raw))
	}
	return finalizeRaw(filtered, r, raw, "gem "+firstNonEmpty(sub, "cmd"))
}

func summarizePipInstall(raw string, ultra bool) string {
	clean := util.StripAnsi(raw)
	var errors []string
	for _, l := range strings.Split(clean, "\n") {
		if pipErrorRe.MatchString(l) {
			errors = append(errors, l)
		}
	}
	if len(errors) > 0 {
		if ultra {
			return fmt.Sprintf("✗%derr", len(errors))
		}
		return "✗ Install failed\n" + strings.Join(limit(errors, 5), "\n")
	}
	if m := pipSuccessRe.FindStringSubmatch(clean); m != nil {
		pkgs := whitespaceSep.Split(strings.TrimSpace(m[1]), -1)
		if ultra {
			return fmt.Sprintf("✓%dpkg", len(pkgs))
		}
		return fmt.Sprintf("✓ Installed %d %s: %s",
			len(pkgs), plural(len(pkgs), "package", "packages"), strings.Join(limit(pkgs, 10), ", "))
	}
	if pipSatisfyRe.MatchString(clean) {
		if ultra {
			return "✓cached"
		}
		return "✓ requirements already satisfied"
	}
	if ultra {
		return "✓"
	}
	return "✓ ok"
}

func summarizeUvSync(raw string, ultra bool, exitCode int) string {
	clean := util.StripAnsi(raw)
	installed := len(uvAddedRe.FindAllString(clean, -1))
	removed := len(uvRemovedRe.FindAllString(clean, -1))
	if exitCode != 0 {
		return errorTail(raw)
	}
	if installed == 0 && removed == 0 {
		if ultra {
			return "✓"
		}
		return "✓ up-to-date"
	}
	if ultra {
		return fmt.Sprintf("✓+%d-%d", installed, removed)
	}
	return fmt.Sprintf("✓ synced: +%d / -%d packages", installed, removed)
}

func summarizeList(raw string, ultra bool, noun string) string {
	var lines []string
	for _, l := range strings.Split(util.StripAnsi(raw), "\n") {
		if strings.TrimSpace(l) != "" && !listSkipRe.MatchString(l) {
			lines = append(lines, l)
		}
	}
	if ultra {
		return fmt.Sprintf("%d %s", len(lines), noun)
	}
	return fmt.Sprintf("%d %s installed\n%s", len(lines), noun, strings.Join(limit(lines, 25), "\n"))
}

func errorTail(raw string) string {
	lines := nonEmptyLines(util.StripAnsi(raw))
	var errs []string
	for _, l := range lines {
		if tailErrRe.MatchString(l) {
			errs = append(errs, l)
		}
	}
	if len(errs) > 0 {
		return strings.Join(limit(errs, 8), "\n")
	}
	return strings.Join(lastN(lines, 8), "\n")
}
