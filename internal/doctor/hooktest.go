package doctor

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/hook"
)

type hookCase struct {
	label                 string
	payload               string
	expectRewriteContains string
	expectPassThrough     bool
}

var hookCases = []hookCase{
	{"rewrites bare git status", `{"tool_name":"Bash","tool_input":{"command":"git status"}}`, "tok git status", false},
	{"rewrites npm install", `{"tool_name":"Bash","tool_input":{"command":"npm install react"}}`, "tok npm install", false},
	{"rewrites npx tsc", `{"tool_name":"Bash","tool_input":{"command":"npx tsc --noEmit"}}`, "tok tsc", false},
	{"leaves cd alone", `{"tool_name":"Bash","tool_input":{"command":"cd /tmp"}}`, "", true},
	{"leaves shell pipelines alone (safety)", `{"tool_name":"Bash","tool_input":{"command":"git status | head"}}`, "", true},
	{"no-op when already prefixed with tok", `{"tool_name":"Bash","tool_input":{"command":"tok git status"}}`, "", true},
	{"passes non-Bash tools through", `{"tool_name":"Read","tool_input":{"command":""}}`, "", true},
}

// RunHookTest exercises the Claude hook's decision logic in-process against a fixed battery
// of cases and returns a report plus a process exit code (non-zero on any failure). hookPath
// is informational (the logic runs in-process regardless).
func RunHookTest(hookPath string) (string, int) {
	command := hookPath
	if command == "" {
		command = firstNonEmpty(hook.ReadRegisteredClaudeCommand(), hook.ClaudeHookCommand())
	}
	lines := []string{"Testing hook: " + command, ""}
	passed, failed := 0, 0

	for _, c := range hookCases {
		stdout, _ := hook.BuildClaudeHookOutput(c.payload)
		ok, reason := checkOutcome(c, stdout)
		if ok {
			passed++
			lines = append(lines, "  PASS  "+c.label)
		} else {
			failed++
			lines = append(lines, "  FAIL  "+c.label, "        "+reason, "        stdout: "+truncate(stdout, 200))
		}
	}

	lines = append(lines, "", strconv.Itoa(passed)+" passed, "+strconv.Itoa(failed)+" failed")
	if failed > 0 {
		lines = append(lines, "",
			"If tests fail, check that:",
			"  1. tok is on PATH (run: which tok  or  where tok)",
			"  2. The registered hook command matches your tok install (re-run: tok init --claude)")
	} else {
		lines = append(lines, "Hook is wired up correctly. Restart your AI tool if you haven't already.")
	}

	if cmd := hook.ReadRegisteredClaudeCommand(); cmd != "" {
		lines = append(lines, "", "Registered: ~/.claude/settings.json PreToolUse runs `"+cmd+"`")
	} else {
		lines = append(lines, "", "Note: no tok hook registered in ~/.claude/settings.json - run: tok init --claude")
	}

	exitCode := 0
	if failed > 0 {
		exitCode = 1
	}
	return strings.Join(lines, "\n"), exitCode
}

func checkOutcome(c hookCase, stdout string) (bool, string) {
	if c.expectPassThrough {
		s := strings.TrimSpace(stdout)
		if s == "" || s == "{}" {
			return true, ""
		}
		return false, "expected empty stdout (pass-through), got payload"
	}
	if c.expectRewriteContains != "" {
		var parsed struct {
			HookSpecificOutput struct {
				UpdatedInput struct {
					Command *string `json:"command"`
				} `json:"updatedInput"`
			} `json:"hookSpecificOutput"`
			UpdatedInput struct {
				Command *string `json:"command"`
			} `json:"updated_input"`
		}
		if json.Unmarshal([]byte(stdout), &parsed) != nil {
			return false, "stdout is not valid JSON"
		}
		updated := parsed.HookSpecificOutput.UpdatedInput.Command
		if updated == nil {
			updated = parsed.UpdatedInput.Command
		}
		if updated == nil {
			return false, "no updatedInput.command in hook output (wrong protocol shape)"
		}
		if !strings.Contains(*updated, c.expectRewriteContains) {
			return false, `expected to contain "` + c.expectRewriteContains + `", got "` + *updated + `"`
		}
		return true, ""
	}
	return false, "malformed test case"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
