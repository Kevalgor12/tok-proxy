// Package doctor implements tok's self-diagnostics: doctor (end-to-end health check),
// verify (per-tool hook status), and hook-test (exercise the rewrite decision logic).
package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/constants"
	"github.com/Kevalgor12/tok-proxy/internal/hook"
	"github.com/Kevalgor12/tok-proxy/internal/store"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

type check struct {
	level  string // ok | warn | fail
	label  string
	detail string
	fix    string
}

// RunDoctor is the end-to-end health check: runtime, PATH, the local store, config, cache,
// and a live in-process probe through the Claude hook exactly as the AI tool would trigger it.
func RunDoctor(s *store.Store, cfg config.Config) string {
	var checks []check
	checks = append(checks, checkRuntime())
	checks = append(checks, checkBash())
	checks = append(checks, checkTokOnPath()...)
	checks = append(checks, checkDatabase(s))
	checks = append(checks, checkConfig(cfg))
	checks = append(checks, checkCache(s, cfg))
	checks = append(checks, checkClaudeHook()...)
	checks = append(checks, checkCursorHook())

	fails, warns := 0, 0
	for _, c := range checks {
		switch c.level {
		case "fail":
			fails++
		case "warn":
			warns++
		}
	}

	lines := []string{"tok doctor - v" + constants.Version, strings.Repeat("═", 58)}
	for _, c := range checks {
		tag := "OK  "
		switch c.level {
		case "warn":
			tag = "WARN"
		case "fail":
			tag = "FAIL"
		}
		lines = append(lines, "  "+tag+"  "+c.label)
		if c.detail != "" {
			lines = append(lines, "        "+c.detail)
		}
		if c.fix != "" && c.level != "ok" {
			lines = append(lines, "        → fix: "+c.fix)
		}
	}
	lines = append(lines, "")
	if fails == 0 && warns == 0 {
		lines = append(lines, "All checks passed. tok is wired up and healthy.")
	} else {
		lines = append(lines, fmt.Sprintf("%d failing, %d %s. Address the items above.", fails, warns, plural(warns, "warning", "warnings")))
	}
	return strings.Join(lines, "\n")
}

func checkRuntime() check {
	return check{
		level:  "ok",
		label:  "tok runtime",
		detail: fmt.Sprintf("tok v%s (self-contained Go binary - the Claude hook needs no node)", constants.Version),
	}
}

var bashVersionRe = regexp.MustCompile(`version\s+([\d.]+)`)

func checkBash() check {
	if out, err := exec.Command("bash", "--version").Output(); err == nil {
		v := "?"
		if m := bashVersionRe.FindStringSubmatch(string(out)); m != nil {
			v = m[1]
		}
		return check{"ok", "Bash shell", "bash " + v + " available", ""}
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{
			"C:/Program Files/Git/bin/bash.exe",
			"C:/Program Files/Git/usr/bin/bash.exe",
			"C:/Program Files (x86)/Git/bin/bash.exe",
			filepath.Join(home(), "AppData", "Local", "Programs", "Git", "bin", "bash.exe"),
		} {
			if util.FileExists(p) {
				return check{"ok", "Bash shell", "Git Bash at " + p + " (not on this shell's PATH, but Claude Code finds it)", ""}
			}
		}
	}
	return check{"warn", "Bash shell",
		"bash not found - the `tok hook claude` hook usually runs without it, but Claude Code on Windows may want Git Bash",
		"install Git for Windows if hooks don't fire after a restart"}
}

var pathExtRe = regexp.MustCompile(`(?i)\.(cmd|exe|ps1|bat)$`)

func checkTokOnPath() []check {
	var out []byte
	var err error
	if runtime.GOOS == "windows" {
		out, err = exec.Command("where", "tok").Output()
	} else {
		out, err = exec.Command("which", "-a", "tok").Output()
	}
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return []check{{"ok", "tok on PATH",
			"not on PATH - fine, hooks call tok by its full path. `npm link` adds a global `tok`.", ""}}
	}
	var found []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			found = append(found, l)
		}
	}
	checks := []check{{"ok", "tok on PATH", found[0], ""}}

	// Collapse the npm/nvm shim trio (tok/tok.cmd/tok.ps1) and symlink duplicates that
	// resolve to the same real file; only warn on genuinely distinct binaries.
	distinct := map[string]struct{}{}
	for _, f := range found {
		real := f
		if rp, e := filepath.EvalSymlinks(f); e == nil {
			real = rp
		}
		distinct[strings.ToLower(pathExtRe.ReplaceAllString(real, ""))] = struct{}{}
	}
	if len(distinct) > 1 {
		keys := make([]string, 0, len(distinct))
		for k := range distinct {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		checks = append(checks, check{"warn", "PATH collision",
			fmt.Sprintf("%d distinct \"tok\" binaries on PATH - the first one wins:\n        %s", len(distinct), strings.Join(keys, "\n        ")),
			"remove the shadowing binaries so the intended tok is invoked"})
	}
	return checks
}

func checkDatabase(s *store.Store) check {
	c := s.RowCounts()
	return check{"ok", "Local data store",
		fmt.Sprintf("%s - %d commands, %d usage rows (JSON/NDJSON, zero native deps)", store.DataDir(), c.Commands, c.AIUsage), ""}
}

func checkConfig(cfg config.Config) check {
	p := config.Path()
	if !util.FileExists(p) {
		return check{"ok", "Config", "using built-in defaults (no config file yet - created on first run)", ""}
	}
	raw, _ := util.ReadFileIfExists(p)
	var probe any
	if json.Unmarshal([]byte(raw), &probe) == nil {
		return check{"ok", "Config", fmt.Sprintf("%s (valid, %d excluded commands)", p, len(cfg.ExcludeCommands)), ""}
	}
	return check{"warn", "Config", p + " is not valid JSON - defaults are being used instead",
		"fix the JSON syntax or delete the file to regenerate defaults"}
}

func checkCache(s *store.Store, cfg config.Config) check {
	if !cfg.Cache.Enabled {
		return check{"ok", "Output cache", "disabled in config", ""}
	}
	st := s.CacheStats()
	return check{"ok", "Output cache", fmt.Sprintf("enabled - %d entries, %d hits served as markers", st.Entries, st.Hits), ""}
}

func checkClaudeHook() []check {
	if !util.FileExists(filepath.Join(home(), ".claude")) {
		return []check{{"ok", "Claude Code", "not detected on this system (skipped)", ""}}
	}
	registered := hook.ReadRegisteredClaudeCommand()
	var out []check
	if registered != "" {
		out = append(out, check{"ok", "Claude Code hook", "registered in settings.json: `" + registered + "`", ""})
	} else {
		out = append(out, check{"fail", "Claude Code hook",
			"Claude Code is present but the tok hook is NOT registered (it will never fire)", "tok init --claude"})
	}
	if registered != "" {
		pass, rewrite, reason := hook.ProbeClaudeHook()
		if pass {
			out = append(out, check{"ok", "Claude Code hook logic", `rewrites "git status" → "` + rewrite + `"`, ""})
		} else {
			out = append(out, check{"fail", "Claude Code hook logic", "hook did not produce a valid rewrite: " + reason, "tok hook-test  for details"})
		}
	}
	return out
}

func checkCursorHook() check {
	if !util.FileExists(filepath.Join(home(), ".cursor")) {
		return check{"ok", "Cursor", "not detected on this system (skipped)", ""}
	}
	if !util.FileExists(filepath.Join(home(), ".cursor", "hooks", "tok-rewrite.sh")) {
		return check{"warn", "Cursor hook", "Cursor present but hook not installed", "tok init --cursor"}
	}
	return check{"ok", "Cursor hook", "~/.cursor/hooks/tok-rewrite.sh installed", ""}
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
