package run

import (
	"strings"
	"testing"
)

func TestRunEcho(t *testing.T) {
	t.Setenv("TOK_HOME", t.TempDir())
	r := Run("echo", []string{"hello"})
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "hello") {
		t.Errorf("stdout = %q, want to contain %q", r.Stdout, "hello")
	}
}

func TestRunUnknownIsNonZero(t *testing.T) {
	t.Setenv("TOK_HOME", t.TempDir())
	if r := Run("tok-nonexistent-cmd-xyz", nil); r.ExitCode == 0 {
		t.Error("unknown command should exit non-zero")
	}
}
