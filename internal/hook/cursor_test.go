package hook

import (
	"strings"
	"testing"
)

func TestBuildCursorHookOutput(t *testing.T) {
	// A recognized command is denied, with an agent_message telling it to re-run via tok.
	out := BuildCursorHookOutput(`{"command":"git status","cwd":"/x"}`)
	if !strings.Contains(out, `"permission":"deny"`) || !strings.Contains(out, "tok git status") {
		t.Errorf("git status = %q", out)
	}
	// A compound command denies with each recognized part rewritten.
	if out := BuildCursorHookOutput(`{"command":"cd api && npm ci"}`); !strings.Contains(out, "cd api && tok npm ci") {
		t.Errorf("compound = %q", out)
	}
	// Anything tok doesn't rewrite is allowed - the guard never blocks those.
	for _, cmd := range []string{"cd /tmp", "tok git status", "git status | grep x", ""} {
		if got := BuildCursorHookOutput(`{"command":"` + cmd + `"}`); got != `{"permission":"allow"}` {
			t.Errorf("allow expected for %q, got %q", cmd, got)
		}
	}
	if got := BuildCursorHookOutput("not json"); got != `{"permission":"allow"}` {
		t.Errorf("malformed = %q", got)
	}
}
