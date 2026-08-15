// Package hook implements the Node-free `tok hook claude` PreToolUse hook: Claude Code
// pipes the tool-call JSON to stdin and reads the rewrite decision from stdout. Doing the
// whole protocol inside tok (no shell script, no jq) is what lets tok ship as one binary.
package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/registry"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// BuildClaudeHookOutput returns the JSON to print for a PreToolUse payload, and whether
// there is anything to print (false = pass the command through untouched).
func BuildClaudeHookOutput(payload string) (string, bool) {
	var obj struct {
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
	}
	if json.Unmarshal([]byte(payload), &obj) != nil {
		return "", false
	}
	if obj.ToolName != "Bash" {
		return "", false
	}
	command, _ := obj.ToolInput["command"].(string)
	if command == "" {
		return "", false
	}

	outcome := registry.RewriteCommand(command)
	if outcome.Kind != "allow" && outcome.Kind != "ask" {
		return "", false // none / deny leaves the command untouched
	}

	// Carry the original tool_input over, overriding just the command.
	toolInput := map[string]any{}
	for k, v := range obj.ToolInput {
		toolInput[k] = v
	}
	toolInput["command"] = outcome.Rewritten

	hookSpecificOutput := map[string]any{
		"hookEventName": "PreToolUse",
		"updatedInput":  toolInput,
	}
	if outcome.Kind == "allow" {
		hookSpecificOutput["permissionDecision"] = "allow"
		hookSpecificOutput["permissionDecisionReason"] = "tok auto-rewrite"
	}

	out, ok := marshalNoEscape(map[string]any{"hookSpecificOutput": hookSpecificOutput})
	if !ok {
		return "", false
	}
	return out, true
}

// marshalNoEscape matches JSON.stringify: no HTML escaping, no trailing newline.
func marshalNoEscape(v any) (string, bool) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", false
	}
	return strings.TrimRight(buf.String(), "\n"), true
}

// ResolveTokInvocation is how the hook should call tok - always an absolute path, never
// bare `tok`. Claude Code runs the hook in a fresh shell (Git Bash on Windows) whose PATH
// may not include tok's directory, especially under Store/MSIX-packaged hosts. os.Executable
// is the running binary's absolute path, so the hook works regardless of PATH.
func ResolveTokInvocation() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		if abs, err := filepath.Abs(exe); err == nil {
			exe = abs
		}
		return shellPath(exe)
	}
	if p := whichTokPath(); p != "" {
		return shellPath(p)
	}
	return "tok"
}

// ClaudeHookCommand is the command string registered in settings.json.
func ClaudeHookCommand() string { return ResolveTokInvocation() + " hook claude" }

func shellPath(p string) string {
	s := strings.ReplaceAll(p, `\`, "/")
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}

func whichTokPath() string {
	prog := "which"
	if runtime.GOOS == "windows" {
		prog = "where"
	}
	out, err := exec.Command(prog, "tok").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

var registeredCmdRe = regexp.MustCompile(`hook\s+claude|tok-rewrite\.sh`)

// ReadRegisteredClaudeCommand returns the tok PreToolUse command in ~/.claude/settings.json
// (both the command form and the legacy shell script), or "" if none.
func ReadRegisteredClaudeCommand() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	raw, ok := util.ReadFileIfExists(filepath.Join(home, ".claude", "settings.json"))
	if !ok {
		return ""
	}
	var cfg struct {
		Hooks struct {
			PreToolUse []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return ""
	}
	for _, entry := range cfg.Hooks.PreToolUse {
		for _, h := range entry.Hooks {
			if registeredCmdRe.MatchString(h.Command) {
				return h.Command
			}
		}
	}
	return ""
}

// ProbeClaudeHook runs the hook's decision logic on a fake `git status` payload and
// confirms it rewrites to a tok command. In-process, so the self-check is reliable
// (a packaged binary re-spawning itself with piped stdin is not).
func ProbeClaudeHook() (pass bool, rewrite, reason string) {
	out, ok := BuildClaudeHookOutput(`{"tool_name":"Bash","tool_input":{"command":"git status"}}`)
	if !ok {
		return false, "", "hook did not rewrite a Bash git command"
	}
	var parsed struct {
		HookSpecificOutput struct {
			UpdatedInput struct {
				Command string `json:"command"`
			} `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if json.Unmarshal([]byte(out), &parsed) != nil {
		return false, "", "unexpected hook output"
	}
	if rw := parsed.HookSpecificOutput.UpdatedInput.Command; strings.HasPrefix(rw, "tok ") {
		return true, rw, ""
	}
	return false, "", "unexpected hook output"
}
