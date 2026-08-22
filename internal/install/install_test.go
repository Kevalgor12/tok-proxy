package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kevalgor12/tok-proxy/internal/hook"
	"github.com/Kevalgor12/tok-proxy/internal/store"
)

func TestGenerators(t *testing.T) {
	md := GenerateAwarenessMd("9.9.9")
	if !strings.Contains(md, "tok-hook-version: 9.9.9") || !strings.Contains(md, "tok git <args>") {
		t.Errorf("awareness md = %q", md)
	}
	// Cursor rule (.mdc) needs alwaysApply frontmatter and the tok guidance.
	rule := cursorRuleFile()
	if !strings.Contains(rule, "alwaysApply: true") || !strings.Contains(rule, "prefix each recognized part") {
		t.Errorf("cursor rule = %q", rule)
	}
}

func TestUpsertHookCommand(t *testing.T) {
	// Fresh array: installs.
	arr, st := upsertHookCommand(nil, "BIN hook claude")
	if st != "installed" || len(arr) != 1 || str(arr[0].(map[string]any)["command"]) != "BIN hook claude" {
		t.Fatalf("install = %q %#v", st, arr)
	}
	// Same command again: skipped.
	if _, st := upsertHookCommand(arr, "BIN hook claude"); st != "skipped" {
		t.Errorf("skipped expected, got %q", st)
	}
	// Legacy + duplicate ours + an unrelated entry: updates the first, drops the duplicate,
	// keeps the unrelated one.
	mixed := []any{
		map[string]any{"type": "command", "command": "~/.cursor/hooks/tok-rewrite.sh"},
		map[string]any{"type": "command", "command": "echo unrelated"},
		map[string]any{"type": "command", "command": "/abs/tok hook claude"},
	}
	out, st := upsertHookCommand(mixed, "NEW hook claude")
	if st != "updated" || len(out) != 2 {
		t.Fatalf("updated = %q len=%d", st, len(out))
	}
	if str(out[0].(map[string]any)["command"]) != "NEW hook claude" {
		t.Errorf("first not updated: %#v", out[0])
	}
	for _, e := range out {
		if strings.Contains(str(e.(map[string]any)["command"]), "tok-rewrite.sh") {
			t.Errorf("legacy entry not collapsed: %#v", out)
		}
	}
}

func TestInitClaudeRoundTrip(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("HOME", tempHome)
	t.Setenv("APPDATA", filepath.Join(tempHome, "AppData", "Roaming"))
	t.Setenv("TOK_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(tempHome, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := store.Open()
	if out := RunInit(s, InitOptions{Claude: true}); !strings.Contains(out, "Claude Code") || !strings.Contains(out, "[transparent]") {
		t.Errorf("init = %q", out)
	}
	if cmd := hook.ReadRegisteredClaudeCommand(); !strings.Contains(cmd, "hook claude") {
		t.Errorf("registered command = %q", cmd)
	}
	settings, _ := os.ReadFile(filepath.Join(tempHome, ".claude", "settings.json"))
	if !strings.Contains(string(settings), "hook claude") || !strings.Contains(string(settings), `"matcher": "Bash"`) {
		t.Errorf("settings.json = %q", settings)
	}

	if out := RunInit(s, InitOptions{Show: true}); !strings.Contains(out, "Claude Code (hook)") {
		t.Errorf("show = %q", out)
	}

	if out := RunInit(s, InitOptions{Uninstall: true}); !strings.Contains(out, "Removed") {
		t.Errorf("uninstall = %q", out)
	}
	if cmd := hook.ReadRegisteredClaudeCommand(); cmd != "" {
		t.Errorf("hook should be gone, got %q", cmd)
	}
}
