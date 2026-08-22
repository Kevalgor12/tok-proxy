package hook

import (
	"encoding/json"

	"github.com/Kevalgor12/tok-proxy/internal/registry"
)

// BuildAntigravityHookOutput is tok's PreToolUse hook for Antigravity. Unlike Cursor and Windsurf
// (whose hooks can only allow/deny), Antigravity's PreToolUse contract supports "overwrite" - a
// shallow, top-level merge into the tool call's args whose result is what actually executes and
// is recorded. That lets tok rewrite the command TRANSPARENTLY here, exactly like Claude Code,
// with no deny-and-retry bounce: a recognized command is allowed with its CommandLine overwritten
// to the tok form. Anything tok doesn't rewrite returns an empty object, which leaves
// Antigravity's normal auto-run decision (AUTO_RUN_DECISION_*) untouched.
//
// Verified against this build's bundled spec (~/.gemini/antigravity-ide/builtin/skills/
// agy-customizations/docs/hooks.md) and the language_server binary: keys are camelCase (protojson).
//
// Input (stdin):  {"toolCall":{"name":"run_command","args":{"CommandLine":"..."}}, ...}
// Output (stdout): {"decision":"allow","overwrite":{"CommandLine":"tok ..."}}   or   {}
func BuildAntigravityHookOutput(payload string) string {
	const noop = "{}"

	var obj struct {
		ToolCall struct {
			Args struct {
				CommandLine string `json:"CommandLine"`
			} `json:"args"`
		} `json:"toolCall"`
	}
	if json.Unmarshal([]byte(payload), &obj) != nil {
		return noop
	}
	cmd := obj.ToolCall.Args.CommandLine
	outcome := registry.RewriteCommand(cmd)
	if (outcome.Kind != "allow" && outcome.Kind != "ask") || outcome.Rewritten == "" || outcome.Rewritten == cmd {
		return noop
	}
	out, ok := marshalNoEscape(map[string]any{
		"decision":  "allow",
		"overwrite": map[string]any{"CommandLine": outcome.Rewritten},
	})
	if !ok {
		return noop
	}
	return out
}
