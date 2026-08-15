package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/store"
)

func TestIngestClaudeCodeAndModels(t *testing.T) {
	t.Setenv("TOK_HOME", t.TempDir())

	ccDir := t.TempDir()
	proj := filepath.Join(ccDir, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","timestamp":"2026-08-15T10:00:00.000Z","message":{"model":"claude-opus-4-5","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":10}}}` + "\n" +
		`{"type":"user","timestamp":"2026-08-15T10:00:00.000Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "abc.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	s := store.Open()
	cfg := config.Defaults()
	cfg.ClaudeCodeDataDir = ccDir

	if msg := RunUsageIngest(s, cfg, IngestArgs{Source: "claude-code"}); !strings.Contains(msg, "Ingested 1 new entries") {
		t.Errorf("ingest = %q", msg)
	}
	if msg := RunUsageIngest(s, cfg, IngestArgs{Source: "claude-code"}); !strings.Contains(msg, "Ingested 0 new entries") || !strings.Contains(msg, "Skipped 1") {
		t.Errorf("re-ingest (dedup) = %q", msg)
	}
	if models := RunUsageModels(s); !strings.Contains(models, "Models seen") || !strings.Contains(models, "claude-opus-4-5") {
		t.Errorf("models = %q", models)
	}
	if logMsg := RunUsageLog(s, ManualLogArgs{Model: "claude-sonnet-4-5", Input: 10, Output: 5}); !strings.Contains(logMsg, "Logged: claude-sonnet-4-5") {
		t.Errorf("log = %q", logMsg)
	}
}

func TestParseSinceAndUUID(t *testing.T) {
	if _, ok := parseSince(""); ok {
		t.Error("empty since should not parse")
	}
	if _, ok := parseSince("2026-01-01"); !ok {
		t.Error("date since should parse")
	}
	if id := uuidV4(); len(id) != 36 || strings.Count(id, "-") != 4 {
		t.Errorf("uuid = %q", id)
	}
}
