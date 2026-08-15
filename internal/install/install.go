// Package install wires tok into AI coding tools. Claude Code and Cursor get a transparent
// PreToolUse hook that rewrites Bash commands; tools without a hook protocol (Copilot,
// Gemini, Windsurf, Cline) get an instruction file they read as a system prompt.
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
	Claude    bool
	Cursor    bool
	Copilot   bool
	Gemini    bool
	Windsurf  bool
	Cline     bool
	Uninstall bool
	Show      bool
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

	all := !opts.Claude && !opts.Cursor && !opts.Copilot && !opts.Gemini && !opts.Windsurf && !opts.Cline

	var results []installResult
	if all || opts.Claude {
		results = append(results, installClaudeCode()...)
	}
	if all || opts.Cursor {
		results = append(results, installCursor())
	}
	if all || opts.Copilot {
		results = append(results, installCopilot())
	}
	if all || opts.Gemini {
		results = append(results, installGemini())
	}
	if all || opts.Windsurf {
		results = append(results, installWindsurf())
	}
	if all || opts.Cline {
		results = append(results, installCline())
	}

	s.SetMeta("hook_version", constants.Version)
	return formatResults(results)
}

func formatResults(results []installResult) string {
	var lines []string
	anyTransparent, anyInstruction := false, false
	for _, r := range results {
		if r.mode == "transparent" {
			anyTransparent = true
		}
		if r.mode == "instruction" {
			anyInstruction = true
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
			"Instruction mode: tool reads tok-awareness.md as a system prompt.",
			"  Compression depends on the model voluntarily prefixing commands with tok.")
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

func writeHookIfChanged(p, content string) string {
	existing, had := util.ReadFileIfExists(p)
	if had {
		if m := hookVersionRe.FindStringSubmatch(existing); m != nil && m[1] == constants.Version && existing == content {
			return "skipped"
		}
	}
	if !util.WriteFileSafe(p, content) {
		return "failed"
	}
	util.ChmodIfPosix(p, 0o755)
	if had {
		return "updated"
	}
	return "installed"
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

func installCursor() installResult {
	cursorDir := filepath.Join(home(), ".cursor")
	if !util.FileExists(cursorDir) {
		return installResult{tool: "Cursor", status: "not-detected"}
	}
	hooksDir := filepath.Join(cursorDir, "hooks")
	util.EnsureDir(hooksDir)
	scriptPath := filepath.Join(hooksDir, "tok-rewrite.sh")
	scriptStatus := writeHookIfChanged(scriptPath, GenerateCursorHook(constants.Version, hook.ResolveTokInvocation()))

	// Replace any previous fake config with a real registration referencing the script.
	hooksPath := filepath.Join(cursorDir, "hooks.json")
	cfg := readJSONMap(hooksPath)
	if oldPre, ok := cfg["preToolUse"].([]any); ok {
		filtered := rejectEntries(oldPre, func(m map[string]any) bool {
			return str(m["id"]) == "tok-rewrite" || strings.Contains(str(m["command"]), "tok proxy")
		})
		if len(filtered) == 0 {
			delete(cfg, "preToolUse")
		} else {
			cfg["preToolUse"] = filtered
		}
	}
	portable := strings.ReplaceAll(tildify(scriptPath), `\`, "/")
	hooksObj := childMap(cfg, "hooks")
	hooksObj["preToolUse"] = []any{map[string]any{"id": "tok-rewrite", "version": constants.Version, "command": portable}}
	cfg["hooks"] = hooksObj
	writeJSONMap(hooksPath, cfg)

	return installResult{tool: "Cursor", status: scriptStatus, detail: "v" + constants.Version, mode: "transparent"}
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

func installWindsurf() installResult {
	dir := filepath.Join(home(), ".codeium", "windsurf")
	if !util.FileExists(dir) {
		return installResult{tool: "Windsurf", status: "not-detected"}
	}
	ok := util.WriteFileSafe(filepath.Join(dir, awarenessFilename), GenerateAwarenessMd(constants.Version))
	return installResult{tool: "Windsurf", status: okStatus(ok), detail: "v" + constants.Version, mode: "instruction"}
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
		{"Cursor (script)", filepath.Join(home(), ".cursor", "hooks", "tok-rewrite.sh")},
		{"Cursor (config)", filepath.Join(home(), ".cursor", "hooks.json")},
		{"VS Code (awareness)", filepath.Join(os.Getenv("APPDATA"), "Code", "User", awarenessFilename)},
		{"Gemini (awareness)", filepath.Join(home(), ".gemini", awarenessFilename)},
		{"Windsurf (awareness)", filepath.Join(home(), ".codeium", "windsurf", awarenessFilename)},
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

func tildify(p string) string {
	h := home()
	if h != "" && strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
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
