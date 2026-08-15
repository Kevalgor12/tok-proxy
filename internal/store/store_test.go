package store

import "testing"

func TestCommandsRoundTrip(t *testing.T) {
	s := newStore(t.TempDir())
	s.AppendCommand(CommandRow{Timestamp: "2026-01-01T00:00:00.000Z", CmdType: "git status", InputBytes: 100, OutBytes: 10, SavedBytes: 90, SavingsPct: 90})
	s.AppendCommand(CommandRow{Timestamp: "2026-01-01T00:01:00.000Z", CmdType: "ls", InputBytes: 50, OutBytes: 20, SavedBytes: 30, SavingsPct: 60})

	rows := s.ReadCommands()
	if len(rows) != 2 {
		t.Fatalf("ReadCommands len = %d, want 2", len(rows))
	}
	if rows[0].CmdType != "git status" || rows[1].SavedBytes != 30 {
		t.Errorf("unexpected rows: %+v", rows)
	}
	if c := s.RowCounts(); c.Commands != 2 {
		t.Errorf("RowCounts.Commands = %d, want 2", c.Commands)
	}
}

func TestAIUsageDedup(t *testing.T) {
	s := newStore(t.TempDir())
	r := AIUsageRecord{Timestamp: "2026-01-01T00:00:00.000Z", Source: "manual", Model: "opus", InputTokens: 10}
	if !s.AppendAIUsage(r) {
		t.Error("first append should report new")
	}
	if s.AppendAIUsage(r) {
		t.Error("duplicate (timestamp,source,model) should be skipped")
	}
	if got := len(s.ReadAIUsage()); got != 1 {
		t.Errorf("ReadAIUsage len = %d, want 1", got)
	}
}

func TestMetaAndCache(t *testing.T) {
	s := newStore(t.TempDir())
	s.SetMeta("k", "v")
	if v, ok := s.GetMeta("k"); !ok || v != "v" {
		t.Errorf("GetMeta = %q, %v", v, ok)
	}

	s.UpsertCacheEntry(CacheRow{CacheKey: "a", CmdType: "git status", OutputHash: "h", FilteredBytes: 10, FirstSeen: "t", LastSeen: "t"})
	s.BumpCacheHit("a", "t2")
	if e, ok := s.GetCacheEntry("a"); !ok || e.HitCount != 1 || e.LastSeen != "t2" {
		t.Errorf("cache entry after bump = %+v ok=%v", e, ok)
	}
	if st := s.CacheStats(); st.Entries != 1 || st.Hits != 1 || st.SavedBytes != 10 {
		t.Errorf("CacheStats = %+v", st)
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s1 := newStore(dir)
	s1.AppendCommand(CommandRow{Timestamp: "2026-01-01T00:00:00.000Z", CmdType: "ls"})
	s1.SetMeta("install_at", "then")

	s2 := newStore(dir) // fresh instance, same directory
	if got := len(s2.ReadCommands()); got != 1 {
		t.Errorf("reopened commands = %d, want 1", got)
	}
	if v, _ := s2.GetMeta("install_at"); v != "then" {
		t.Errorf("reopened meta install_at = %q, want %q", v, "then")
	}
}
