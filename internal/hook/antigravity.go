package hook

import (
	"encoding/json"

	"github.com/Kevalgor12/tok-proxy/internal/registry"
)

// BuildAntigravityHookOutput is the deny-and-retry guard for Antigravity's PreToolUse hook.
// Antigravity hooks can't rewrite a command, so when the agent is about to run one tok would
// compress, we deny it with a reason telling the agent to re-run it as the tok form. Anything
// tok doesn't recognize returns empty output, which leaves Antigravity's normal flow untouched.
//
// Input (stdin): {"toolCall":{"name":"run_command","args":{"CommandLine":"...","Cwd":"..."}}, ...}
// Output (stdout): {"decision":"deny","reason":"..."}  or empty (no intervention).
func BuildAntigravityHookOutput(payload string) string {
	var obj struct {
		ToolCall struct {
			Args struct {
				CommandLine string `json:"CommandLine"`
			} `json:"args"`
		} `json:"toolCall"`
	}
	if json.Unmarshal([]byte(payload), &obj) != nil {
		return ""
	}
	cmd := obj.ToolCall.Args.CommandLine
	outcome := registry.RewriteCommand(cmd)
	if (outcome.Kind != "allow" && outcome.Kind != "ask") || outcome.Rewritten == "" || outcome.Rewritten == cmd {
		return ""
	}
	out, ok := marshalNoEscape(map[string]any{
		"decision": "deny",
		"reason":   guardMessage(outcome.Rewritten),
	})
	if !ok {
		return ""
	}
	return out
}
