package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertRulesBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	// Fresh file: installed.
	if st := upsertRulesBlock(path); st != "installed" {
		t.Fatalf("first upsert = %q, want installed", st)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), ruleStart) || !strings.Contains(string(got), "tok git status") {
		t.Fatalf("block not written: %q", got)
	}

	// Same content again: skipped, no duplication.
	if st := upsertRulesBlock(path); st != "skipped" {
		t.Errorf("second upsert = %q, want skipped", st)
	}
	if got, _ := os.ReadFile(path); strings.Count(string(got), ruleStart) != 1 {
		t.Errorf("block duplicated: %q", got)
	}

	// Removing leaves the file without the block.
	if !removeRulesBlock(path) {
		t.Error("removeRulesBlock returned false")
	}
	if got, _ := os.ReadFile(path); strings.Contains(string(got), ruleStart) {
		t.Errorf("block still present after remove: %q", got)
	}
}

func TestUpsertPreservesUserContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "global_rules.md")
	user := "# My rules\n\nAlways write tests.\n"
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}

	if st := upsertRulesBlock(path); st != "updated" {
		t.Fatalf("upsert onto existing = %q, want updated", st)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "Always write tests.") {
		t.Errorf("user content lost: %q", got)
	}
	if !strings.Contains(string(got), ruleStart) {
		t.Errorf("tok block missing: %q", got)
	}

	// Remove restores just the user content (no tok block, user text intact).
	removeRulesBlock(path)
	got, _ = os.ReadFile(path)
	if strings.Contains(string(got), ruleStart) || !strings.Contains(string(got), "Always write tests.") {
		t.Errorf("remove did not cleanly restore user content: %q", got)
	}
}
