// Package cache implements tok's output cache: when an idempotent read command produces
// byte-identical output to a previous run, tok returns a tiny "unchanged" marker instead of
// re-sending the whole payload to the model. The real command always runs, so exit codes and
// side effects are never skipped - only what the model sees shrinks.
package cache

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/store"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

type Decision struct {
	Output     string // what to show the caller (the compact marker on a hit)
	Hit        bool
	SavedBytes int
}

func normalizeArgs(args []string) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if t := strings.TrimSpace(a); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// IsCacheable reports whether a command type is on the read-only allowlist and caching is on.
func IsCacheable(cmdType string, cfg config.Config) bool {
	if !cfg.Cache.Enabled {
		return false
	}
	head := cmdType
	if i := strings.IndexAny(cmdType, " :"); i >= 0 {
		head = cmdType[:i]
	}
	for _, c := range cfg.Cache.Commands {
		if c == cmdType || c == head {
			return true
		}
	}
	return false
}

// CacheKey folds the command type, working directory, and args into a 24-char fingerprint so
// the same command in two repos never collides.
func CacheKey(cmdType string, args []string, cwd string) string {
	raw := cmdType + " " + cwd + " " + normalizeArgs(args)
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])[:24]
}

// Consult is called after a command has run. On an identical repeat it records the hit and
// returns a compact marker; otherwise it stores the fresh fingerprint and returns the output
// unchanged. Identity is decided on the FILTERED output - exactly what the model would see.
func Consult(s *store.Store, cfg config.Config, cmdType string, args []string, cwd, filtered string, exitCode int) Decision {
	miss := Decision{Output: filtered}
	if !IsCacheable(cmdType, cfg) || exitCode != 0 {
		return miss
	}
	filteredBytes := len(filtered)
	if filteredBytes > cfg.Cache.MaxOutputBytes {
		return miss
	}

	key := CacheKey(cmdType, args, cwd)
	outputHash := util.ShortHash(filtered)
	now := util.NowIso()
	existing, found := s.GetCacheEntry(key)

	if found && existing.OutputHash == outputHash {
		// Identical repeat: record it, then serve the marker only if it is actually smaller
		// than the output it replaces (already-tiny results stay as-is).
		s.BumpCacheHit(key, now)
		marker := unchangedMarker(cmdType, existing, filteredBytes)
		if len(marker) >= filteredBytes {
			return miss
		}
		return Decision{Output: marker, Hit: true, SavedBytes: filteredBytes - len(marker)}
	}

	firstSeen, hitCount := now, 0
	if found {
		firstSeen, hitCount = existing.FirstSeen, existing.HitCount
	}
	s.UpsertCacheEntry(store.CacheRow{
		CacheKey:      key,
		CmdType:       cmdType,
		OutputHash:    outputHash,
		FilteredBytes: filteredBytes,
		HitCount:      hitCount,
		FirstSeen:     firstSeen,
		LastSeen:      now,
	})
	s.PruneCache(cfg.Cache.MaxEntries)
	return miss
}

func unchangedMarker(cmdType string, existing store.CacheRow, filteredBytes int) string {
	times := existing.HitCount + 1
	repeat := ""
	if times > 1 {
		repeat = fmt.Sprintf(" %d×", times)
	}
	return fmt.Sprintf("◇ unchanged%s (%s, %s) - ~%d tok saved; already in context. --no-cache to force.",
		repeat, cmdType, util.RelativeTime(existing.LastSeen), util.BytesToTokens(filteredBytes))
}

// RunCache backs `tok cache`: inspect or clear the output cache.
func RunCache(s *store.Store, cfg config.Config, clear, list bool) string {
	if clear {
		n := s.ClearCache()
		return fmt.Sprintf("Cleared %s cache %s.", util.FormatNumber(n), plural(n, "entry", "entries"))
	}

	stats := s.CacheStats()
	lines := []string{
		"tok cache - unchanged-output detection",
		strings.Repeat("═", 58),
		"Status:        " + enabled(cfg.Cache.Enabled),
		fmt.Sprintf("Entries:       %s / %s max", util.FormatNumber(stats.Entries), util.FormatNumber(cfg.Cache.MaxEntries)),
		fmt.Sprintf("Cache hits:    %s  (repeats served as a marker)", util.FormatNumber(stats.Hits)),
		fmt.Sprintf("Tokens saved:  ~%s  (%s)", util.FormatNumber(util.BytesToTokens(stats.SavedBytes)), util.FormatBytes(stats.SavedBytes)),
	}

	if list || stats.Hits > 0 {
		var top []store.CacheRow
		for _, e := range s.TopCacheEntries(15) {
			if e.HitCount > 0 {
				top = append(top, e)
			}
		}
		if len(top) > 0 {
			lines = append(lines, "", "Most-reused commands:",
				fmt.Sprintf("  %s %s command (last seen)", util.Pad("hits", 6, false), util.Pad("bytes", 9, false)))
			for _, e := range top {
				lines = append(lines, fmt.Sprintf("  %s %s %s (%s)",
					util.Pad(strconv.Itoa(e.HitCount), 6, false),
					util.Pad(util.FormatBytes(e.FilteredBytes), 9, false),
					e.CmdType, util.RelativeTime(e.LastSeen)))
			}
		}
	}

	if stats.Entries == 0 {
		lines = append(lines, "",
			"No cached commands yet. The cache fills as idempotent reads",
			"(git status, ls, cat, grep, …) are run more than once.")
	}
	lines = append(lines, "", "Commands: tok cache --list | tok cache --clear")
	return strings.Join(lines, "\n")
}

func enabled(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
