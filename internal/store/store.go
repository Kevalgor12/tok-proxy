// Package store is tok's zero-dependency local store. No database, no native modules -
// just files under ~/.tok:
//
//	commands.ndjson   append-only log of filtered-command savings (one JSON per line)
//	ai_usage.ndjson   append-only log of ingested AI token usage
//	meta.json         small key/value bag (versions, timestamps)
//	cache.json        output-cache index (unchanged-detection metadata, no payloads)
//
// Event logs are append-only (fast hot path); analytics read them back and aggregate.
// meta/cache are tiny and rewritten atomically (temp file + rename).
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/constants"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// On-disk field names are snake_case to stay byte-compatible with the Node build's files.

type CommandRow struct {
	Timestamp  string  `json:"timestamp"`
	CmdType    string  `json:"cmd_type"`
	InputBytes int     `json:"input_bytes"`
	OutBytes   int     `json:"out_bytes"`
	SavedBytes int     `json:"saved_bytes"`
	SavingsPct float64 `json:"savings_pct"`
	ExecMs     int     `json:"exec_ms"`
}

type AIUsageRecord struct {
	Timestamp        string  `json:"timestamp"`
	SessionID        string  `json:"session_id"`
	Model            string  `json:"model"`
	Source           string  `json:"source"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

type CacheRow struct {
	CacheKey      string `json:"cache_key"`
	CmdType       string `json:"cmd_type"`
	OutputHash    string `json:"output_hash"`
	FilteredBytes int    `json:"filtered_bytes"`
	HitCount      int    `json:"hit_count"`
	FirstSeen     string `json:"first_seen"`
	LastSeen      string `json:"last_seen"`
}

type Counts struct {
	Commands int
	AIUsage  int
}

type CacheStatsResult struct {
	Entries    int
	Hits       int
	SavedBytes int
}

type Store struct {
	Dir          string
	commandsFile string
	aiUsageFile  string
	metaFile     string
	cacheFile    string

	// Lazy in-memory caches; nil until first use (nil = not loaded).
	meta   map[string]string
	cache  map[string]CacheRow
	aiKeys map[string]struct{}
}

func newStore(dir string) *Store {
	util.EnsureDir(dir)
	return &Store{
		Dir:          dir,
		commandsFile: filepath.Join(dir, "commands.ndjson"),
		aiUsageFile:  filepath.Join(dir, "ai_usage.ndjson"),
		metaFile:     filepath.Join(dir, "meta.json"),
		cacheFile:    filepath.Join(dir, "cache.json"),
	}
}

var cached *Store

// Open returns the process-wide store, stamping version + install time on first use.
func Open() *Store {
	if cached != nil {
		return cached
	}
	s := newStore(util.DataDir())
	s.SetMeta("tok_version", constants.Version)
	if _, ok := s.GetMeta("install_at"); !ok {
		s.SetMeta("install_at", util.NowIso())
	}
	cached = s
	return s
}

// DataDir is the store's directory, reported by `tok doctor`.
func DataDir() string { return util.DataDir() }

// ---- NDJSON event logs -----------------------------------------------------

func readNdjson[T any](file string) []T {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil // missing file = empty log
	}
	var out []T
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row T
		if json.Unmarshal([]byte(line), &row) == nil {
			out = append(out, row)
		}
		// a torn/partial line is skipped rather than fatal
	}
	return out
}

func (s *Store) appendNdjson(file string, row any) {
	b, err := json.Marshal(row)
	if err != nil {
		util.AppendErrorLog("store.append", err)
		return
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		util.AppendErrorLog("store.append", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		util.AppendErrorLog("store.append", err)
	}
}

func (s *Store) ReadCommands() []CommandRow   { return readNdjson[CommandRow](s.commandsFile) }
func (s *Store) ReadAIUsage() []AIUsageRecord { return readNdjson[AIUsageRecord](s.aiUsageFile) }
func (s *Store) AppendCommand(row CommandRow) { s.appendNdjson(s.commandsFile, row) }

// AppendAIUsage dedups on (timestamp, source, model) so repeated ingests don't double-count.
// Returns true when the row was newly written.
func (s *Store) AppendAIUsage(row AIUsageRecord) bool {
	if s.aiKeys == nil {
		s.aiKeys = make(map[string]struct{})
		for _, r := range s.ReadAIUsage() {
			s.aiKeys[aiKey(r)] = struct{}{}
		}
	}
	k := aiKey(row)
	if _, seen := s.aiKeys[k]; seen {
		return false
	}
	s.aiKeys[k] = struct{}{}
	s.appendNdjson(s.aiUsageFile, row)
	return true
}

func aiKey(r AIUsageRecord) string { return r.Timestamp + "|" + r.Source + "|" + r.Model }

func countLines(file string) int {
	data, err := os.ReadFile(file)
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func (s *Store) RowCounts() Counts {
	return Counts{Commands: countLines(s.commandsFile), AIUsage: countLines(s.aiUsageFile)}
}

// ---- meta ------------------------------------------------------------------

func (s *Store) loadMeta() map[string]string {
	if s.meta == nil {
		m := map[string]string{}
		readJSON(s.metaFile, &m)
		s.meta = m
	}
	return s.meta
}

func (s *Store) GetMeta(key string) (string, bool) {
	v, ok := s.loadMeta()[key]
	return v, ok
}

func (s *Store) SetMeta(key, value string) {
	m := s.loadMeta()
	m[key] = value
	writeJSONAtomic(s.metaFile, m)
}

// ---- output cache index ----------------------------------------------------

func (s *Store) loadCache() map[string]CacheRow {
	if s.cache == nil {
		c := map[string]CacheRow{}
		readJSON(s.cacheFile, &c)
		s.cache = c
	}
	return s.cache
}

func (s *Store) saveCache() {
	if s.cache != nil {
		writeJSONAtomic(s.cacheFile, s.cache)
	}
}

func (s *Store) GetCacheEntry(key string) (CacheRow, bool) {
	e, ok := s.loadCache()[key]
	return e, ok
}

func (s *Store) UpsertCacheEntry(row CacheRow) {
	s.loadCache()[row.CacheKey] = row
	s.saveCache()
}

func (s *Store) BumpCacheHit(key, lastSeen string) {
	c := s.loadCache()
	e, ok := c[key]
	if !ok {
		return
	}
	e.HitCount++
	e.LastSeen = lastSeen
	c[key] = e // map values are copies in Go - write the mutated struct back
	s.saveCache()
}

func (s *Store) CacheStats() CacheStatsResult {
	var st CacheStatsResult
	for _, e := range s.loadCache() {
		st.Entries++
		st.Hits += e.HitCount
		st.SavedBytes += e.HitCount * e.FilteredBytes
	}
	return st
}

func (s *Store) TopCacheEntries(limit int) []CacheRow {
	c := s.loadCache()
	rows := make([]CacheRow, 0, len(c))
	for _, e := range c {
		rows = append(rows, e)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].HitCount != rows[j].HitCount {
			return rows[i].HitCount > rows[j].HitCount
		}
		return rows[i].LastSeen > rows[j].LastSeen
	})
	if limit < len(rows) {
		rows = rows[:limit]
	}
	return rows
}

func (s *Store) ClearCache() int {
	n := len(s.loadCache())
	s.cache = map[string]CacheRow{}
	s.saveCache()
	return n
}

// PruneCache bounds the cache by dropping the least-recently-seen entries past maxEntries.
func (s *Store) PruneCache(maxEntries int) {
	c := s.loadCache()
	if len(c) <= maxEntries {
		return
	}
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return c[keys[i]].LastSeen < c[keys[j]].LastSeen })
	for _, k := range keys[:len(c)-maxEntries] {
		delete(c, k)
	}
	s.saveCache()
}

// ---- json helpers ----------------------------------------------------------

func readJSON[T any](file string, into *T) {
	if data, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(data, into)
	}
}

// writeJSONAtomic writes to a per-process temp file then renames over the target, so a
// reader never sees a half-written file and concurrent tok processes don't clobber each
// other's temp file.
func writeJSONAtomic(file string, obj any) {
	b, err := json.Marshal(obj)
	if err != nil {
		util.AppendErrorLog("store.write", err)
		return
	}
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		util.AppendErrorLog("store.write", err)
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, file); err != nil {
		util.AppendErrorLog("store.write", err)
		_ = os.Remove(tmp)
	}
}
