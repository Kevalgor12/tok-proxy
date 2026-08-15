package cache

import (
	"strings"
	"testing"

	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/store"
)

func TestIsCacheableAndKey(t *testing.T) {
	cfg := config.Defaults()
	if !IsCacheable("git status", cfg) {
		t.Error("git status should be cacheable")
	}
	if !IsCacheable("cat", cfg) {
		t.Error("cat should be cacheable")
	}
	if !IsCacheable("cat:file.go", cfg) {
		t.Error("cat:file.go should be cacheable via its head")
	}
	if IsCacheable("npm install", cfg) {
		t.Error("npm install must NOT be cacheable")
	}
	k1 := CacheKey("cat", []string{"a.txt"}, "/x")
	k2 := CacheKey("cat", []string{"a.txt"}, "/y")
	if len(k1) != 24 || k1 == k2 {
		t.Errorf("keys: %q %q", k1, k2)
	}
}

func TestConsult(t *testing.T) {
	t.Setenv("TOK_HOME", t.TempDir())
	s := store.Open()
	cfg := config.Defaults()
	filtered := strings.Repeat("status line here\n", 40) // ~680 bytes, longer than the marker

	if d := Consult(s, cfg, "git status", nil, "/repo", filtered, 0); d.Hit {
		t.Error("first consult should miss")
	}
	d := Consult(s, cfg, "git status", nil, "/repo", filtered, 0)
	if !d.Hit || !strings.Contains(d.Output, "unchanged") || d.SavedBytes <= 0 {
		t.Errorf("second consult should hit with a marker: %+v", d)
	}
	// A failing command is never cached.
	if d := Consult(s, cfg, "git status", nil, "/repo", filtered, 1); d.Hit {
		t.Error("failures must not be served from cache")
	}
}
