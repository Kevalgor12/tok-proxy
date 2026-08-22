package hook

import (
	"encoding/json"

	"github.com/Kevalgor12/tok-proxy/internal/registry"
)

// BuildWindsurfGuard is the deny-and-retry guard for Windsurf's pre_run_command hook. Windsurf
// blocks a command by exiting 2 with a message on stderr (not JSON on stdout), so this returns
// the message and whether to block; the caller wires the exit code. Anything tok doesn't
// recognize returns block=false, so only recognized commands are ever stopped.
//
// Input (stdin): {"agent_action_name":"pre_run_command","tool_info":{"command_line":"...","cwd":"..."}}
func BuildWindsurfGuard(payload string) (msg string, block bool) {
	var obj struct {
		ToolInfo struct {
			CommandLine string `json:"command_line"`
		} `json:"tool_info"`
	}
	if json.Unmarshal([]byte(payload), &obj) != nil {
		return "", false
	}
	cmd := obj.ToolInfo.CommandLine
	outcome := registry.RewriteCommand(cmd)
	if (outcome.Kind != "allow" && outcome.Kind != "ask") || outcome.Rewritten == "" || outcome.Rewritten == cmd {
		return "", false
	}
	return guardMessage(outcome.Rewritten), true
}
