package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// Go and Rust toolchains multiplex several actions behind one binary
// (go test|build|vet, cargo test|build|check|clippy). We dispatch on the
// sub-command: tests get a pass/fail summary, builds/lints get diagnostics.

func HandleGo(args []string, ultra bool) Result {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("go", args)
	raw := combined(r)
	var filtered string
	switch sub {
	case "test":
		filtered = summarizeGoTest(raw, ultra, r.ExecMs)
	case "build", "vet", "install":
		filtered = summarizeDiagnostics(raw, "go "+sub, ultra, r.ExitCode)
	default:
		filtered = strings.TrimSpace(util.StripAnsi(raw))
	}
	return finalizeRaw(filtered, r, raw, "go "+firstNonEmpty(sub, "cmd"))
}

func HandleCargo(args []string, ultra bool) Result {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("cargo", args)
	raw := combined(r)
	var filtered string
	switch sub {
	case "test":
		filtered = summarizeCargoTest(raw, ultra, r.ExecMs)
	case "build", "check", "clippy":
		filtered = summarizeDiagnostics(raw, "cargo "+sub, ultra, r.ExitCode)
	default:
		filtered = strings.TrimSpace(util.StripAnsi(raw))
	}
	return finalizeRaw(filtered, r, raw, "cargo "+firstNonEmpty(sub, "cmd"))
}

var (
	goFailNameRe  = regexp.MustCompile(`(?m)^\s*--- FAIL:\s+(\S+)`)
	goOkPkgRe     = regexp.MustCompile(`(?m)^ok\s+\S+`)
	goFailPkgRe   = regexp.MustCompile(`(?m)^FAIL\s+\S+`)
	cargoResultRe = regexp.MustCompile(`test result:\s+\w+\.\s+(\d+)\s+passed;\s+(\d+)\s+failed`)
	cargoFailsRe  = regexp.MustCompile(`\nfailures:\n([\s\S]*?)(?:\ntest result:|\n\n|$)`)
)

func summarizeGoTest(raw string, ultra bool, execMs int) string {
	clean := util.StripAnsi(raw)
	var failNames []string
	for _, m := range goFailNameRe.FindAllStringSubmatch(clean, -1) {
		failNames = append(failNames, m[1])
	}
	okPkgs := len(goOkPkgRe.FindAllString(clean, -1))
	failPkgs := len(goFailPkgRe.FindAllString(clean, -1))

	if len(failNames) == 0 && failPkgs == 0 {
		if ultra {
			return fmt.Sprintf("✓%dpkg", okPkgs)
		}
		return fmt.Sprintf("✓ tests passed (%d %s, %dms)", okPkgs, plural(okPkgs, "package", "packages"), execMs)
	}
	if ultra {
		return fmt.Sprintf("✗%d", len(failNames))
	}
	out := []string{
		fmt.Sprintf("%d failed %s in %d %s:",
			len(failNames), plural(len(failNames), "test", "tests"), failPkgs, plural(failPkgs, "package", "packages")),
		"",
	}
	for _, n := range limit(failNames, 20) {
		out = append(out, "  ✗ "+n)
	}
	return strings.Join(out, "\n")
}

func summarizeCargoTest(raw string, ultra bool, execMs int) string {
	clean := util.StripAnsi(raw)
	passed, failed := 0, 0
	for _, m := range cargoResultRe.FindAllStringSubmatch(clean, -1) {
		passed += atoi(m[1])
		failed += atoi(m[2])
	}
	if failed == 0 {
		if ultra {
			return fmt.Sprintf("✓%d", passed)
		}
		return fmt.Sprintf("✓ All %d tests passed (%dms)", passed, execMs)
	}
	var names []string
	if b := cargoFailsRe.FindStringSubmatch(clean); b != nil {
		for _, l := range strings.Split(b[1], "\n") {
			t := strings.TrimSpace(l)
			if t != "" && !strings.HasPrefix(t, "----") {
				names = append(names, t)
			}
		}
	}
	if ultra {
		return fmt.Sprintf("✗%d/%d", failed, passed+failed)
	}
	out := []string{fmt.Sprintf("%d failed %s:", failed, plural(failed, "test", "tests")), ""}
	for _, n := range limit(names, 20) {
		out = append(out, "  ✗ "+n)
	}
	out = append(out, "", fmt.Sprintf("Summary: %d passed, %d failed", passed, failed))
	return strings.TrimSpace(strings.Join(out, "\n"))
}
