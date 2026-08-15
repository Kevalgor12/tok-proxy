package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/store"
)

func TestHookTest(t *testing.T) {
	out, code := RunHookTest("")
	if code != 0 || !strings.Contains(out, "0 failed") {
		t.Errorf("hooktest code=%d out=%q", code, out)
	}
	for _, want := range []string{"PASS  rewrites bare git status", "PASS  leaves cd alone", "PASS  no-op when already prefixed with tok"} {
		if !strings.Contains(out, want) {
			t.Errorf("hooktest missing %q in %q", want, out)
		}
	}
}

func TestDoctorAndVerify(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("HOME", tempHome)
	t.Setenv("APPDATA", filepath.Join(tempHome, "AppData", "Roaming"))
	t.Setenv("TOK_HOME", t.TempDir())

	claudeDir := filepath.Join(tempHome, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/abs/tok hook claude"}]}]}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	s := store.Open()
	cfg := config.Defaults()

	doc := RunDoctor(s, cfg)
	for _, want := range []string{"tok doctor", "Claude Code hook", "registered in settings.json", `rewrites "git status"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("doctor missing %q in %q", want, doc)
		}
	}

	ver := RunVerify(s)
	if !strings.Contains(ver, "Hook status") || !strings.Contains(ver, "Claude Code") || !strings.Contains(ver, "Probe:   PASS") {
		t.Errorf("verify = %q", ver)
	}
}
