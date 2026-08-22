package hook

import (
	"encoding/json"

	"github.com/Kevalgor12/tok-proxy/internal/registry"
)

// BuildCursorHookOutput implements the "deny-and-retry" guard for Cursor's beforeShellExecution
// hook. Cursor hooks cannot rewrite a command, only allow/deny/ask, so tok can't intercept
// silently. Instead: when the agent is about to run a command tok would compress, deny it with
// an agent_message telling the agent to re-run it as the tok-prefixed form. Everything tok does
// not recognize is allowed untouched, so only recognized commands ever bounce.
//
// Input (Cursor beforeShellExecution, on stdin): {"command": "...", "cwd": "...", ...}
// Output (on stdout): {"permission":"allow"} or {"permission":"deny","agent_message":...,"user_message":...}
func BuildCursorHookOutput(payload string) string {
	allow := `{"permission":"allow"}`

	var obj struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(payload), &obj) != nil || obj.Command == "" {
		return allow
	}

	outcome := registry.RewriteCommand(obj.Command)
	if (outcome.Kind != "allow" && outcome.Kind != "ask") || outcome.Rewritten == "" || outcome.Rewritten == obj.Command {
		return allow
	}

	out, ok := marshalNoEscape(map[string]any{
		"permission":    "deny",
		"agent_message": guardMessage(outcome.Rewritten),
		"user_message":  "tok: re-run as `" + outcome.Rewritten + "`",
	})
	if !ok {
		return allow
	}
	return out
}

// guardMessage is the retry instruction tok sends back to a block-only IDE's agent when it
// denies a recognized command.
func guardMessage(rewritten string) string {
	return "Intercepted by tok to save tokens. Re-run this exact command instead: " + rewritten +
		" (tok runs the real command, compresses its output, and preserves the exit code)."
}
