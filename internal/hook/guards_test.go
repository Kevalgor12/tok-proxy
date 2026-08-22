package hook

import (
	"strings"
	"testing"
)

func TestBuildAntigravityHookOutput(t *testing.T) {
	// Recognized command -> transparent rewrite via allow + overwrite (no deny/bounce).
	out := BuildAntigravityHookOutput(`{"toolCall":{"name":"run_command","args":{"CommandLine":"git status"}}}`)
	if !strings.Contains(out, `"decision":"allow"`) || !strings.Contains(out, `"overwrite"`) || !strings.Contains(out, `"CommandLine":"tok git status"`) {
		t.Errorf("git status = %q", out)
	}
	if out := BuildAntigravityHookOutput(`{"toolCall":{"args":{"CommandLine":"cd api && npm ci"}}}`); !strings.Contains(out, `"CommandLine":"cd api && tok npm ci"`) {
		t.Errorf("compound = %q", out)
	}
	// Anything tok doesn't rewrite -> {} no-op, leaving Antigravity's normal auto-run decision alone.
	for _, cmd := range []string{"cd /tmp", "tok git status", "git status | grep x"} {
		p := `{"toolCall":{"args":{"CommandLine":"` + cmd + `"}}}`
		if got := BuildAntigravityHookOutput(p); got != "{}" {
			t.Errorf("expected {} no-op for %q, got %q", cmd, got)
		}
	}
	if got := BuildAntigravityHookOutput("not json"); got != "{}" {
		t.Errorf("malformed = %q", got)
	}
}

func TestBuildWindsurfGuard(t *testing.T) {
	msg, block := BuildWindsurfGuard(`{"tool_info":{"command_line":"git status"}}`)
	if !block || !strings.Contains(msg, "tok git status") {
		t.Errorf("git status = %q block=%v", msg, block)
	}
	if m, b := BuildWindsurfGuard(`{"tool_info":{"command_line":"cd api && npm ci"}}`); !b || !strings.Contains(m, "cd api && tok npm ci") {
		t.Errorf("compound = %q block=%v", m, b)
	}
	for _, cmd := range []string{"cd /tmp", "tok git status", "git status | grep x"} {
		if _, b := BuildWindsurfGuard(`{"tool_info":{"command_line":"` + cmd + `"}}`); b {
			t.Errorf("expected no block for %q", cmd)
		}
	}
	if _, b := BuildWindsurfGuard("not json"); b {
		t.Error("malformed should not block")
	}
}
