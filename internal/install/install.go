// Package install wires tok into AI coding tools. Claude Code and Cursor get a transparent
// PreToolUse hook that rewrites Bash commands; tools whose hooks cannot rewrite a command
// (Copilot, Gemini, Windsurf, Antigravity, Cline) instead get a tok rule written into the
// global-rules file they actually read, asking the agent to prefix commands with tok.
package install

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/constants"
	"github.com/Kevalgor12/tok-proxy/internal/hook"
	"github.com/Kevalgor12/tok-proxy/internal/store"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

type InitOptions struct {
	Claude      bool
	Cursor      bool
	Copilot     bool
	Gemini      bool
	Windsurf    bool
	Cline       bool
	Antigravity bool
	Uninstall   bool
	Show        bool
	// Enforce opts Cursor into the deny-and-retry guard hook (reliable but the agent bounces
	// off a blocked command and retries). Without it Cursor gets the safe instruction rule only.
	Enforce bool
}

type installResult struct {
	tool   string
	status string // installed | updated | skipped | not-detected | failed | removed
	detail string
	mode   string // transparent | instruction | ""
}

const awarenessFilename = "tok-awareness.md"

var (
	hookVersionRe  = regexp.MustCompile(`tok-hook-version:\s*([\w.-]+)`)
	reTokRewriteSh = regexp.MustCompile(`tok-rewrite\.sh`)
	reHookClaude   = regexp.MustCompile(`\bhook\s+claude\b`)
	reTokRewrite   = regexp.MustCompile(`\btok\b[^\n]*\brewrite\b`)
)

// RunInit installs, shows, or uninstalls tok's hooks. With no per-tool flag it targets every
// detected tool.
func RunInit(s *store.Store, opts InitOptions) string {
	if opts.Show {
		return showHookStatus()
	}
	if opts.Uninstall {
		return uninstallAll()
	}

	all := !opts.Claude && !opts.Cursor && !opts.Copilot && !opts.Gemini && !opts.Windsurf && !opts.Cline && !opts.Antigravity

	var results []installResult
	if all || opts.Claude {
		results = append(results, installClaudeCode()...)
	}
	if all || opts.Cursor {
		results = append(results, installCursor(opts.Enforce))
	}
	if all || opts.Copilot {
		results = append(results, installCopilot())
	}
	if all || opts.Gemini {
		results = append(results, installGemini())
	}
	if all || opts.Windsurf {
		results = append(results, installWindsurf(opts.Enforce))
	}
	if all || opts.Antigravity {
		results = append(results, installAntigravity(opts.Enforce))
	}
	if all || opts.Cline {
		results = append(results, installCline())
	}

	s.SetMeta("hook_version", constants.Version)
	return formatResults(results)
}

func formatResults(results []installResult) string {
	var lines []string
	anyTransparent, anyInstruction, anyEnforce := false, false, false
	for _, r := range results {
		switch r.mode {
		case "transparent":
			anyTransparent = true
		case "instruction":
			anyInstruction = true
		case "enforce":
			anyEnforce = true
		}
		tag := "-"
		switch r.status {
		case "installed", "updated", "skipped", "removed":
			tag = "OK"
		case "failed":
			tag = "FAIL"
		}
		note := ""
		if r.detail != "" {
			note = " (" + r.detail + ")"
		}
		if r.status == "not-detected" {
			lines = append(lines, "  - "+util.Pad(r.tool, 22, false)+" not detected"+note)
			continue
		}
		modeTag := ""
		switch r.mode {
		case "instruction":
			modeTag = " [instruction-mode]"
		case "transparent":
			modeTag = " [transparent]"
		case "enforce":
			modeTag = " [enforce]"
		}
		lines = append(lines, "  "+util.Pad(tag, 4, false)+" "+util.Pad(r.tool, 22, false)+" "+r.status+modeTag+note)
	}
	lines = append(lines, "")
	if anyTransparent {
		lines = append(lines,
			"Transparent mode: hook intercepts Bash tool calls and rewrites them automatically.",
			"  → Restart the AI tool, then run: tok hook-test  to confirm.")
	}
	if anyInstruction {
		lines = append(lines,
			"Instruction mode: a tok rule is written into the tool's global-rules file.",
			"  Compression depends on the model voluntarily prefixing commands with tok.")
	}
	if anyEnforce {
		lines = append(lines,
			"Enforce mode (EXPERIMENTAL): the hook blocks a recognized command and tells the agent to re-run it as tok.",
			"  Reliable savings, but the agent hits one blocked call per command. Restart the tool; validate it actually retries.")
	}
	lines = append(lines, "  → Then run: tok verify  for a full status report.")
	return strings.Join(lines, "\n")
}

func installClaudeCode() []installResult {
	claudeDir := filepath.Join(home(), ".claude")
	if !util.FileExists(claudeDir) {
		return []installResult{{tool: "Claude Code", status: "not-detected", detail: "install Claude Code to enable"}}
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Remove the legacy script-based hooks from older tok versions - the hook is now the
	// single self-contained `tok hook claude` command.
	tryUnlink(filepath.Join(claudeDir, "hooks", "tok-rewrite.sh"))
	tryUnlink(filepath.Join(claudeDir, "hooks", "tok-usage.sh"))

	status := mergeClaudeSettings(settingsPath, hook.ClaudeHookCommand())
	return []installResult{{tool: "Claude Code", status: status, detail: "v" + constants.Version, mode: "transparent"}}
}

func mergeClaudeSettings(settingsPath, hookCommand string) string {
	settings := readJSONMap(settingsPath)
	hooks := childMap(settings, "hooks")

	pre, _ := hooks["PreToolUse"].([]any)
	var bashEntry map[string]any
	for _, e := range pre {
		if m, ok := e.(map[string]any); ok && str(m["matcher"]) == "Bash" {
			bashEntry = m
			break
		}
	}
	if bashEntry == nil {
		bashEntry = map[string]any{"matcher": "Bash", "hooks": []any{}}
		pre = append(pre, bashEntry)
	}
	hooks["PreToolUse"] = pre

	inner, _ := bashEntry["hooks"].([]any)
	inner, status := upsertHookCommand(inner, hookCommand)
	bashEntry["hooks"] = inner

	// Strip any obsolete PostToolUse entry pointing at the old tok-usage.sh.
	if post, ok := hooks["PostToolUse"].([]any); ok {
		for _, e := range post {
			if em, ok := e.(map[string]any); ok {
				if hi, ok := em["hooks"].([]any); ok {
					em["hooks"] = rejectCommands(hi, func(cmd string) bool { return strings.Contains(cmd, "tok-usage.sh") })
				}
			}
		}
	}

	settings["hooks"] = hooks
	writeJSONMap(settingsPath, settings)
	return status
}

// upsertHookCommand registers the tok PreToolUse command, collapsing any prior tok entries
// (command form or legacy tok-rewrite.sh) into a single current one.
func upsertHookCommand(arr []any, hookCommand string) ([]any, string) {
	isOurs := func(cmd string) bool {
		return reTokRewriteSh.MatchString(cmd) || reHookClaude.MatchString(cmd) || reTokRewrite.MatchString(cmd)
	}
	firstIdx := -1
	for i, e := range arr {
		if m, ok := e.(map[string]any); ok && isOurs(str(m["command"])) {
			firstIdx = i
			break
		}
	}
	if firstIdx >= 0 {
		first := arr[firstIdx].(map[string]any)
		alreadyCurrent := str(first["command"]) == hookCommand && countOurs(arr, isOurs) == 1
		first["type"] = "command"
		first["command"] = hookCommand
		out := make([]any, 0, len(arr))
		for i, e := range arr {
			if i != firstIdx {
				if m, ok := e.(map[string]any); ok && isOurs(str(m["command"])) {
					continue // drop duplicate tok entries left by earlier installs
				}
			}
			out = append(out, e)
		}
		if alreadyCurrent {
			return out, "skipped"
		}
		return out, "updated"
	}
	return append(arr, map[string]any{"type": "command", "command": hookCommand}), "installed"
}

func countOurs(arr []any, isOurs func(string) bool) int {
	n := 0
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok && isOurs(str(m["command"])) {
			n++
		}
	}
	return n
}

// installCursor writes tok's instruction rule for Cursor. tok used to register a
// tok-rewrite.sh PreToolUse hook, but current Cursor hooks (beforeShellExecution) can only
// allow/deny/ask - they cannot rewrite a command - so that hook never actually took effect.
// We remove it and drop an instruction rule Cursor reads from ~/.cursor/rules/tok.mdc instead.
func installCursor(enforce bool) installResult {
	cursorDir := filepath.Join(home(), ".cursor")
	if !util.FileExists(cursorDir) {
		return installResult{tool: "Cursor", status: "not-detected"}
	}
	// Clear the ancient, non-functional tok-rewrite.sh script + its legacy preToolUse entry, and
	// always drop the safe instruction rule as a baseline.
	tryUnlink(filepath.Join(cursorDir, "hooks", "tok-rewrite.sh"))
	removeFromCursor(filepath.Join(cursorDir, "hooks.json"))
	rulesDir := filepath.Join(cursorDir, "rules")
	util.EnsureDir(rulesDir)
	util.WriteFileSafe(filepath.Join(rulesDir, "tok.mdc"), cursorRuleFile())

	hooksPath := filepath.Join(cursorDir, "hooks.json")
	if !enforce {
		removeCursorGuard(hooksPath) // clear any guard left by a prior --enforce run
		return installResult{tool: "Cursor", status: "installed", detail: "v" + constants.Version + ", rule", mode: "instruction"}
	}
	registerCursorGuard(hooksPath, hook.ResolveTokInvocation()+" hook cursor")
	return installResult{tool: "Cursor", status: "installed", detail: "v" + constants.Version + ", enforce+rule", mode: "enforce"}
}

// registerCursorGuard adds tok's beforeShellExecution deny-and-retry hook to Cursor's hooks.json
// (schema v1), replacing any prior tok guard and preserving the user's own hooks.
func registerCursorGuard(hooksPath, hookCmd string) {
	cfg := readJSONMap(hooksPath)
	if _, ok := cfg["version"]; !ok {
		cfg["version"] = 1
	}
	hooks := childMap(cfg, "hooks")
	arr, _ := hooks["beforeShellExecution"].([]any)
	arr = append(rejectEntries(arr, isTokCursorGuard), map[string]any{"command": hookCmd})
	hooks["beforeShellExecution"] = arr
	cfg["hooks"] = hooks
	writeJSONMap(hooksPath, cfg)
}

// removeCursorGuard strips tok's beforeShellExecution guard from Cursor's hooks.json.
func removeCursorGuard(hooksPath string) bool {
	cfg := readJSONMap(hooksPath)
	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		return false
	}
	arr, ok := hooks["beforeShellExecution"].([]any)
	if !ok {
		return false
	}
	filtered := rejectEntries(arr, isTokCursorGuard)
	if len(filtered) == len(arr) {
		return false
	}
	if len(filtered) == 0 {
		delete(hooks, "beforeShellExecution")
	} else {
		hooks["beforeShellExecution"] = filtered
	}
	writeJSONMap(hooksPath, cfg)
	return true
}

func isTokCursorGuard(m map[string]any) bool {
	return strings.Contains(str(m["command"]), "hook cursor")
}

func installCopilot() installResult {
	target := ""
	for _, d := range []string{
		filepath.Join(home(), ".config", "Code"),
		filepath.Join(home(), "Library", "Application Support", "Code"),
		filepath.Join(os.Getenv("APPDATA"), "Code"),
	} {
		if util.FileExists(d) {
			target = d
			break
		}
	}
	if target == "" {
		return installResult{tool: "Copilot (VS Code)", status: "not-detected"}
	}
	userDir := filepath.Join(target, "User")
	util.EnsureDir(userDir)
	ok := util.WriteFileSafe(filepath.Join(userDir, awarenessFilename), GenerateAwarenessMd(constants.Version))
	return installResult{tool: "Copilot (VS Code)", status: okStatus(ok), detail: "v" + constants.Version, mode: "instruction"}
}

func installGemini() installResult {
	geminiDir := filepath.Join(home(), ".gemini")
	if !util.FileExists(geminiDir) && !which("gemini") {
		return installResult{tool: "Gemini CLI", status: "not-detected"}
	}
	util.EnsureDir(geminiDir)

	// Wipe the legacy fake BeforeTool hook we used to write.
	settingsPath := filepath.Join(geminiDir, "settings.json")
	if existing, ok := util.ReadFileIfExists(settingsPath); ok {
		var parsed map[string]any
		if json.Unmarshal([]byte(existing), &parsed) == nil {
			if hooks, ok := parsed["hooks"].(map[string]any); ok {
				if arr, ok := hooks["BeforeTool"].([]any); ok {
					filtered := rejectEntries(arr, func(m map[string]any) bool { return str(m["id"]) == "tok-rewrite" })
					if len(filtered) == 0 {
						delete(hooks, "BeforeTool")
					} else {
						hooks["BeforeTool"] = filtered
					}
					if len(hooks) == 0 {
						delete(parsed, "hooks")
					}
					writeJSONMap(settingsPath, parsed)
				}
			}
		}
	}

	ok := util.WriteFileSafe(filepath.Join(geminiDir, awarenessFilename), GenerateAwarenessMd(constants.Version))
	return installResult{tool: "Gemini CLI", status: okStatus(ok), detail: "v" + constants.Version, mode: "instruction"}
}

// installWindsurf writes tok's instruction rule to Windsurf's real global-rules file, and with
// enforce also registers the pre_run_command deny-and-retry guard. Windsurf hooks can only
// allow/block, never rewrite.
func installWindsurf(enforce bool) installResult {
	dir := filepath.Join(home(), ".codeium", "windsurf")
	if !util.FileExists(dir) {
		return installResult{tool: "Windsurf", status: "not-detected"}
	}
	// The old tok-awareness.md sitting here was never read by Windsurf; drop it.
	tryUnlink(filepath.Join(dir, awarenessFilename))
	util.EnsureDir(filepath.Join(dir, "memories"))
	upsertRulesBlock(filepath.Join(dir, "memories", "global_rules.md"))

	hooksPath := filepath.Join(dir, "hooks.json")
	if !enforce {
		removeWindsurfGuard(hooksPath)
		return installResult{tool: "Windsurf", status: "installed", detail: "v" + constants.Version + ", rule", mode: "instruction"}
	}
	registerWindsurfGuard(hooksPath, hook.ResolveTokInvocation()+" hook windsurf")
	return installResult{tool: "Windsurf", status: "installed", detail: "v" + constants.Version + ", enforce+rule", mode: "enforce"}
}

// installAntigravity writes tok's rule into Antigravity's global cross-tool rules file
// (~/.gemini/AGENTS.md), and with enforce also registers the PreToolUse deny-and-retry guard in
// ~/.gemini/config/hooks.json. Antigravity hooks can only allow/deny/ask, never rewrite.
func installAntigravity(enforce bool) installResult {
	geminiDir := filepath.Join(home(), ".gemini")
	if !util.FileExists(filepath.Join(home(), ".antigravity")) && !util.FileExists(geminiDir) && !which("antigravity") {
		return installResult{tool: "Antigravity", status: "not-detected"}
	}
	util.EnsureDir(geminiDir)
	upsertRulesBlock(filepath.Join(geminiDir, "AGENTS.md"))

	hooksPath := filepath.Join(geminiDir, "config", "hooks.json")
	if !enforce {
		removeAntigravityGuard(hooksPath)
		return installResult{tool: "Antigravity", status: "installed", detail: "v" + constants.Version + ", rule", mode: "instruction"}
	}
	util.EnsureDir(filepath.Join(geminiDir, "config"))
	registerAntigravityGuard(hooksPath, hook.ResolveTokInvocation()+" hook antigravity")
	return installResult{tool: "Antigravity", status: "installed", detail: "v" + constants.Version + ", enforce+rule", mode: "enforce"}
}

// registerAntigravityGuard sets tok's named PreToolUse guard in Antigravity's hooks.json,
// preserving any other named hooks.
func registerAntigravityGuard(hooksPath, hookCmd string) {
	cfg := readJSONMap(hooksPath)
	cfg["tok"] = map[string]any{
		"PreToolUse": []any{
			map[string]any{
				"matcher": "run_command",
				"hooks": []any{
					map[string]any{"type": "command", "command": hookCmd, "timeout": 30},
				},
			},
		},
	}
	writeJSONMap(hooksPath, cfg)
}

func removeAntigravityGuard(hooksPath string) bool {
	cfg := readJSONMap(hooksPath)
	if _, ok := cfg["tok"]; !ok {
		return false
	}
	delete(cfg, "tok")
	writeJSONMap(hooksPath, cfg)
	return true
}

// registerWindsurfGuard adds tok's pre_run_command guard to Windsurf's hooks.json, replacing any
// prior tok guard and preserving the user's own hooks.
func registerWindsurfGuard(hooksPath, hookCmd string) {
	cfg := readJSONMap(hooksPath)
	hooks := childMap(cfg, "hooks")
	arr, _ := hooks["pre_run_command"].([]any)
	arr = append(rejectEntries(arr, isTokWindsurfGuard), map[string]any{"command": hookCmd, "powershell": hookCmd})
	hooks["pre_run_command"] = arr
	cfg["hooks"] = hooks
	writeJSONMap(hooksPath, cfg)
}

func removeWindsurfGuard(hooksPath string) bool {
	cfg := readJSONMap(hooksPath)
	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		return false
	}
	arr, ok := hooks["pre_run_command"].([]any)
	if !ok {
		return false
	}
	filtered := rejectEntries(arr, isTokWindsurfGuard)
	if len(filtered) == len(arr) {
		return false
	}
	if len(filtered) == 0 {
		delete(hooks, "pre_run_command")
	} else {
		hooks["pre_run_command"] = filtered
	}
	writeJSONMap(hooksPath, cfg)
	return true
}

func isTokWindsurfGuard(m map[string]any) bool {
	return strings.Contains(str(m["command"]), "hook windsurf")
}

func installCline() installResult {
	dir := filepath.Join(home(), ".cline")
	if !util.FileExists(dir) {
		return installResult{tool: "Cline / Roo Code", status: "not-detected"}
	}
	ok := util.WriteFileSafe(filepath.Join(dir, awarenessFilename), GenerateAwarenessMd(constants.Version))
	return installResult{tool: "Cline / Roo Code", status: okStatus(ok), detail: "v" + constants.Version, mode: "instruction"}
}

func uninstallAll() string {
	var removed []string
	add := func(label string, ok bool) {
		if ok {
			removed = append(removed, label)
		}
	}

	claudePre := filepath.Join(home(), ".claude", "hooks", "tok-rewrite.sh")
	claudePost := filepath.Join(home(), ".claude", "hooks", "tok-usage.sh")
	add(claudePre, tryUnlink(claudePre))
	add(claudePost, tryUnlink(claudePost))

	claudeSettings := filepath.Join(home(), ".claude", "settings.json")
	add(claudeSettings+" (tok entries)", removeFromClaudeSettings(claudeSettings))

	cursorScript := filepath.Join(home(), ".cursor", "hooks", "tok-rewrite.sh")
	add(cursorScript, tryUnlink(cursorScript))
	cursorCfg := filepath.Join(home(), ".cursor", "hooks.json")
	add(cursorCfg+" (tok entries)", removeFromCursor(cursorCfg))
	add(cursorCfg+" (guard)", removeCursorGuard(cursorCfg))
	cursorRule := filepath.Join(home(), ".cursor", "rules", "tok.mdc")
	add(cursorRule, tryUnlink(cursorRule))

	// Instruction-mode rule blocks in the IDEs' real global-rules files, plus any deny-and-retry
	// guard hooks from --enforce.
	antigravityRules := filepath.Join(home(), ".gemini", "AGENTS.md")
	add(antigravityRules+" (tok rule)", removeRulesBlock(antigravityRules))
	add(filepath.Join(home(), ".gemini", "config", "hooks.json")+" (guard)", removeAntigravityGuard(filepath.Join(home(), ".gemini", "config", "hooks.json")))
	windsurfRules := filepath.Join(home(), ".codeium", "windsurf", "memories", "global_rules.md")
	add(windsurfRules+" (tok rule)", removeRulesBlock(windsurfRules))
	add(filepath.Join(home(), ".codeium", "windsurf", "hooks.json")+" (guard)", removeWindsurfGuard(filepath.Join(home(), ".codeium", "windsurf", "hooks.json")))

	awareness := [][2]string{
		{"VS Code (Linux)", filepath.Join(home(), ".config", "Code", "User", awarenessFilename)},
		{"VS Code (mac)", filepath.Join(home(), "Library", "Application Support", "Code", "User", awarenessFilename)},
		{"VS Code (win)", filepath.Join(os.Getenv("APPDATA"), "Code", "User", awarenessFilename)},
		{"Gemini", filepath.Join(home(), ".gemini", awarenessFilename)},
		{"Windsurf", filepath.Join(home(), ".codeium", "windsurf", awarenessFilename)},
		{"Cline", filepath.Join(home(), ".cline", awarenessFilename)},
	}
	for _, t := range awareness {
		add(t[0]+": "+t[1], tryUnlink(t[1]))
	}

	if len(removed) == 0 {
		return "Nothing to uninstall."
	}
	out := []string{"Removed:"}
	for _, r := range removed {
		out = append(out, "  - "+r)
	}
	return strings.Join(out, "\n")
}

func tryUnlink(p string) bool {
	if !util.FileExists(p) {
		return false
	}
	if err := os.Remove(p); err != nil {
		util.AppendErrorLog("uninstall.unlink", err)
		return false
	}
	return true
}

func removeFromClaudeSettings(p string) bool {
	existing, ok := util.ReadFileIfExists(p)
	if !ok {
		return false
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(existing), &parsed) != nil {
		return false
	}
	hooks, ok := parsed["hooks"].(map[string]any)
	if !ok {
		return false
	}
	drop := func(cmd string) bool {
		return regexp.MustCompile(`tok-(rewrite|usage)\.sh`).MatchString(cmd) || reHookClaude.MatchString(cmd) || reTokRewrite.MatchString(cmd)
	}
	changed := false
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		arr, ok := hooks[event].([]any)
		if !ok {
			continue
		}
		for _, e := range arr {
			em, ok := e.(map[string]any)
			if !ok {
				continue
			}
			inner, ok := em["hooks"].([]any)
			if !ok {
				continue
			}
			filtered := rejectCommands(inner, drop)
			if len(filtered) != len(inner) {
				changed = true
			}
			em["hooks"] = filtered
		}
	}
	if changed {
		writeJSONMap(p, parsed)
	}
	return changed
}

func removeFromCursor(p string) bool {
	existing, ok := util.ReadFileIfExists(p)
	if !ok {
		return false
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(existing), &parsed) != nil {
		return false
	}
	isTok := func(m map[string]any) bool { return str(m["id"]) == "tok-rewrite" }
	changed := false
	if pre, ok := parsed["preToolUse"].([]any); ok {
		filtered := rejectEntries(pre, isTok)
		if len(filtered) != len(pre) {
			changed = true
		}
		if len(filtered) == 0 {
			delete(parsed, "preToolUse")
		} else {
			parsed["preToolUse"] = filtered
		}
	}
	if obj, ok := parsed["hooks"].(map[string]any); ok {
		if pre, ok := obj["preToolUse"].([]any); ok {
			filtered := rejectEntries(pre, isTok)
			if len(filtered) != len(pre) {
				changed = true
			}
			if len(filtered) == 0 {
				delete(obj, "preToolUse")
			} else {
				obj["preToolUse"] = filtered
			}
			if len(obj) == 0 {
				delete(parsed, "hooks")
			}
		}
	}
	if changed {
		writeJSONMap(p, parsed)
	}
	return changed
}

func showHookStatus() string {
	lines := []string{"Hook installation status:"}

	if claudeCmd := hook.ReadRegisteredClaudeCommand(); claudeCmd != "" {
		lines = append(lines, "  OK  "+util.Pad("Claude Code (hook)", 22, false)+" "+claudeCmd)
	} else {
		lines = append(lines, "  -   "+util.Pad("Claude Code (hook)", 22, false)+" not installed")
	}

	checks := [][2]string{
		{"VS Code (awareness)", filepath.Join(os.Getenv("APPDATA"), "Code", "User", awarenessFilename)},
		{"Gemini (awareness)", filepath.Join(home(), ".gemini", awarenessFilename)},
		{"Cline (awareness)", filepath.Join(home(), ".cline", awarenessFilename)},
	}
	for _, c := range checks {
		if util.FileExists(c[1]) {
			v := ""
			if ver := readHookVersionFromPath(c[1]); ver != "" {
				v = " (v" + ver + ")"
			}
			lines = append(lines, "  OK  "+util.Pad(c[0], 22, false)+" "+c[1]+v)
		} else {
			lines = append(lines, "  -   "+util.Pad(c[0], 22, false)+" not installed")
		}
	}

	// Instruction-mode rule files. Cursor owns a dedicated tok.mdc; Antigravity/Windsurf share a
	// rules file with the user, so those are marker-detected.
	cursorRule := filepath.Join(home(), ".cursor", "rules", "tok.mdc")
	if util.FileExists(cursorRule) {
		lines = append(lines, "  OK  "+util.Pad("Cursor (rule)", 22, false)+" "+cursorRule)
	} else {
		lines = append(lines, "  -   "+util.Pad("Cursor (rule)", 22, false)+" not installed")
	}
	ruleChecks := [][2]string{
		{"Antigravity (rule)", filepath.Join(home(), ".gemini", "AGENTS.md")},
		{"Windsurf (rule)", filepath.Join(home(), ".codeium", "windsurf", "memories", "global_rules.md")},
	}
	for _, c := range ruleChecks {
		if hasRulesBlock(c[1]) {
			lines = append(lines, "  OK  "+util.Pad(c[0], 22, false)+" "+c[1])
		} else {
			lines = append(lines, "  -   "+util.Pad(c[0], 22, false)+" not installed")
		}
	}
	return strings.Join(lines, "\n")
}

func readHookVersionFromPath(p string) string {
	c, ok := util.ReadFileIfExists(p)
	if !ok {
		return ""
	}
	if m := hookVersionRe.FindStringSubmatch(c); m != nil {
		return m[1]
	}
	return ""
}

// ---- helpers ---------------------------------------------------------------

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func okStatus(ok bool) string {
	if ok {
		return "installed"
	}
	return "failed"
}

func which(cmd string) bool {
	prog := "which"
	if runtime.GOOS == "windows" {
		prog = "where"
	}
	return exec.Command(prog, cmd).Run() == nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// childMap returns parent[key] as a map, creating and storing an empty one if it is absent
// or the wrong type.
func childMap(parent map[string]any, key string) map[string]any {
	if m, ok := parent[key].(map[string]any); ok {
		return m
	}
	m := map[string]any{}
	parent[key] = m
	return m
}

// rejectCommands returns the entries whose "command" field does not match drop.
func rejectCommands(arr []any, drop func(string) bool) []any {
	out := make([]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok && drop(str(m["command"])) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// rejectEntries returns the map entries for which drop is false.
func rejectEntries(arr []any, drop func(map[string]any) bool) []any {
	out := make([]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok && drop(m) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func readJSONMap(p string) map[string]any {
	m := map[string]any{}
	if raw, ok := util.ReadFileIfExists(p); ok {
		_ = json.Unmarshal([]byte(raw), &m)
	}
	return m
}

// writeJSONMap writes a settings map as 2-space-indented JSON without HTML escaping, matching
// the Node build's JSON.stringify(obj, null, 2).
func writeJSONMap(p string, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if enc.Encode(v) != nil {
		return
	}
	util.WriteFileSafe(p, strings.TrimRight(buf.String(), "\n"))
}
