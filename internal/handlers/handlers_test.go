package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kevalgor12/tok-proxy/internal/config"
)

func TestFormatGitStatus(t *testing.T) {
	// Long form (the default `git status`).
	long := "On branch main\nChanges not staged for commit:\n\tmodified:   src/a.go\n\nUntracked files:\n\tnew.txt\n"
	if got := formatGitStatus(long, false); !strings.Contains(got, "1 modified") || !strings.Contains(got, "1 untracked") {
		t.Errorf("long form = %q", got)
	}
	// Porcelain fallback (no untracked line, so M/A/D codes are parsed).
	porc := formatGitStatus(" M a.go\nA  b.go\n D c.go\n", false)
	for _, want := range []string{"1 modified", "1 new file", "1 deleted"} {
		if !strings.Contains(porc, want) {
			t.Errorf("porcelain %q missing %q", porc, want)
		}
	}
	// Mixed porcelain: modified + deleted + untracked together (`git status --short`). This
	// previously reported only "1 untracked" and hid the changed files.
	mixed := formatGitStatus(" M a.go\n?? new.txt\n D old.go\n", false)
	for _, want := range []string{"1 modified", "1 deleted", "1 untracked"} {
		if !strings.Contains(mixed, want) {
			t.Errorf("mixed porcelain %q missing %q", mixed, want)
		}
	}
	if s := formatGitStatus("nothing to commit, working tree clean\n", false); s != "nothing to commit, working tree clean" {
		t.Errorf("clean = %q", s)
	}
	if s := formatGitStatus(" M a\n", true); s != "1M" {
		t.Errorf("ultra = %q, want 1M", s)
	}
}

func TestGroupTscErrors(t *testing.T) {
	raw := "src/a.ts(1,2): error TS2304: Cannot find name 'x'.\nsrc/a.ts(5,1): error TS2304: Cannot find name 'y'.\n"
	got := groupTscErrors(raw, false)
	if !strings.Contains(got, "src/a.ts: 2 errors") || !strings.Contains(got, "TS2304 (×2)") {
		t.Errorf("tsc = %q", got)
	}
	if s := groupTscErrors("", false); s != "✓ no errors" {
		t.Errorf("no errors = %q", s)
	}
}

func TestGroupByFile(t *testing.T) {
	got := groupByFile("a.go:3:foo\na.go:9:foo\nb.go:1:foo\n", false, 100)
	if !strings.Contains(got, "a.go: 2 matches") || !strings.Contains(got, "L3, L9") {
		t.Errorf("grep = %q", got)
	}
	if s := groupByFile("", false, 100); s != "0 matches" {
		t.Errorf("no matches = %q", s)
	}
}

func TestHandleCatStripsComments(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x.go")
	if err := os.WriteFile(f, []byte("// header comment\npackage x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := HandleCat([]string{f}, false, config.Defaults())
	if strings.Contains(res.Filtered, "header comment") {
		t.Errorf("comment not stripped: %q", res.Filtered)
	}
	if !strings.Contains(res.Filtered, "package x") {
		t.Errorf("code dropped: %q", res.Filtered)
	}
}
