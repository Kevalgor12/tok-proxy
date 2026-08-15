package hook

import (
	"strings"
	"testing"
)

func TestBuildClaudeHookOutputRewrites(t *testing.T) {
	out, ok := BuildClaudeHookOutput(`{"tool_name":"Bash","tool_input":{"command":"git status"}}`)
	if !ok {
		t.Fatal("expected a rewrite for git status")
	}
	if !strings.Contains(out, `"command":"tok git status"`) {
		t.Errorf("missing rewritten command:\n%s", out)
	}
	if !strings.Contains(out, `"permissionDecision":"allow"`) {
		t.Errorf("missing allow decision:\n%s", out)
	}
}

func TestBuildClaudeHookOutputPassThrough(t *testing.T) {
	cases := map[string]string{
		"non-Bash tool": `{"tool_name":"Read","tool_input":{"file":"x"}}`,
		"pipeline":      `{"tool_name":"Bash","tool_input":{"command":"git log | head"}}`,
		"no rule":       `{"tool_name":"Bash","tool_input":{"command":"whoami"}}`,
		"bad json":      `not json`,
	}
	for name, payload := range cases {
		if _, ok := BuildClaudeHookOutput(payload); ok {
			t.Errorf("%s: expected pass-through (no output)", name)
		}
	}
}

func TestProbeClaudeHook(t *testing.T) {
	pass, rewrite, reason := ProbeClaudeHook()
	if !pass {
		t.Fatalf("probe failed: %s", reason)
	}
	if rewrite != "tok git status" {
		t.Errorf("probe rewrite = %q, want %q", rewrite, "tok git status")
	}
}
