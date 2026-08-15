// Package usage ingests AI token-usage records into tok's local store from Claude Code's
// local logs, the ccusage CLI, or manual entry, and reports the models it has seen.
package usage

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/store"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

type IngestArgs struct {
	Source string // "claude-code" | "ccusage"
	Since  string
}

type ManualLogArgs struct {
	Model      string
	Input      int
	Output     int
	CacheWrite int
	CacheRead  int
	Cost       float64
}

func RunUsageIngest(s *store.Store, cfg config.Config, args IngestArgs) string {
	switch args.Source {
	case "claude-code":
		return ingestClaudeCode(s, cfg, args.Since)
	case "ccusage":
		return ingestCcusage(s, args.Since)
	default:
		return "unknown source"
	}
}

// claudeLine is the subset of a Claude Code transcript line we read: an assistant turn
// carries the model and its token usage.
type claudeLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func ingestClaudeCode(s *store.Store, cfg config.Config, since string) string {
	root := cfg.ClaudeCodeDataDir
	if !util.FileExists(root) {
		return "Claude Code data directory not found: " + root
	}

	sinceT, hasSince := parseSince(since)
	inserted, skipped, fileCount := 0, 0, 0

	for _, file := range collectJsonl(root) {
		fileCount++
		content, err := os.ReadFile(file)
		if err != nil {
			util.AppendErrorLog("ingestClaudeCode.read", err)
			continue
		}
		sessionID := strings.TrimSuffix(filepath.Base(file), ".jsonl")
		for _, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			var obj claudeLine
			if json.Unmarshal([]byte(trimmed), &obj) != nil || obj.Type != "assistant" {
				continue
			}
			if obj.Message.Usage == nil || obj.Message.Model == "" || obj.Timestamp == "" {
				continue
			}
			if hasSince {
				if t, ok := parseTime(obj.Timestamp); ok && t.Before(sinceT) {
					continue
				}
			}
			u := obj.Message.Usage
			ok := s.AppendAIUsage(store.AIUsageRecord{
				Timestamp:        obj.Timestamp,
				SessionID:        sessionID,
				Model:            obj.Message.Model,
				Source:           "claude-code",
				InputTokens:      u.InputTokens,
				OutputTokens:     u.OutputTokens,
				CacheWriteTokens: u.CacheCreationInputTokens,
				CacheReadTokens:  u.CacheReadInputTokens,
			})
			if ok {
				inserted++
			} else {
				skipped++
			}
		}
	}
	return fmt.Sprintf("Ingested %s new entries from %d files. Skipped %s already imported.",
		util.FormatNumber(inserted), fileCount, util.FormatNumber(skipped))
}

// collectJsonl walks root and returns every .jsonl file beneath it.
func collectJsonl(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable dir: skip its subtree, keep walking the rest
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// ccusageOutput is the daily rollup ccusage --json emits.
type ccusageOutput struct {
	Daily []struct {
		Date   string `json:"date"`
		Models map[string]struct {
			InputTokens              int     `json:"input_tokens"`
			OutputTokens             int     `json:"output_tokens"`
			CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
			Cost                     float64 `json:"cost"`
		} `json:"models"`
	} `json:"daily"`
}

func ingestCcusage(s *store.Store, since string) string {
	sinceArg := since
	if sinceArg == "" {
		sinceArg = isoDaysAgo(90)
	}

	res := run.Run("ccusage", []string{"--json", "--since", sinceArg})
	if res.ExitCode != 0 {
		res = run.Run("npx", []string{"--yes", "ccusage", "--json", "--since", sinceArg})
	}
	if res.ExitCode != 0 {
		detail := strings.TrimSpace(res.Stderr)
		if detail == "" {
			detail = "unknown"
		}
		return "ccusage failed. Install with: npm i -g ccusage  (error: " + detail + ")"
	}

	var parsed ccusageOutput
	if json.Unmarshal([]byte(res.Stdout), &parsed) != nil || parsed.Daily == nil {
		return "ccusage returned no daily data"
	}

	inserted, skipped := 0, 0
	for _, day := range parsed.Daily {
		ts := day.Date + "T12:00:00.000Z"
		for model, data := range day.Models {
			ok := s.AppendAIUsage(store.AIUsageRecord{
				Timestamp:        ts,
				SessionID:        "ccusage-" + day.Date,
				Model:            model,
				Source:           "ccusage",
				InputTokens:      data.InputTokens,
				OutputTokens:     data.OutputTokens,
				CacheWriteTokens: data.CacheCreationInputTokens,
				CacheReadTokens:  data.CacheReadInputTokens,
				CostUSD:          data.Cost,
			})
			if ok {
				inserted++
			} else {
				skipped++
			}
		}
	}
	return fmt.Sprintf("Ingested %s new entries from ccusage. Skipped %s already imported.",
		util.FormatNumber(inserted), util.FormatNumber(skipped))
}

func RunUsageLog(s *store.Store, args ManualLogArgs) string {
	s.AppendAIUsage(store.AIUsageRecord{
		Timestamp:        util.NowIso(),
		SessionID:        uuidV4(),
		Model:            args.Model,
		Source:           "manual",
		InputTokens:      args.Input,
		OutputTokens:     args.Output,
		CacheWriteTokens: args.CacheWrite,
		CacheReadTokens:  args.CacheRead,
		CostUSD:          args.Cost,
	})
	return fmt.Sprintf("Logged: %s | %s in + %s out | %s",
		args.Model, util.FormatNumber(args.Input), util.FormatNumber(args.Output), util.Dollar(args.Cost))
}

func RunUsageModels(s *store.Store) string {
	type agg struct {
		n      int
		tokens int
		cost   float64
	}
	byModel := map[string]*agg{}
	var order []string
	for _, r := range s.ReadAIUsage() {
		g, ok := byModel[r.Model]
		if !ok {
			g = &agg{}
			byModel[r.Model] = g
			order = append(order, r.Model)
		}
		g.n++
		g.tokens += r.InputTokens + r.OutputTokens
		g.cost += r.CostUSD
	}
	if len(order) == 0 {
		return "No models seen yet."
	}
	type row struct {
		model  string
		n      int
		tokens int
		cost   float64
	}
	rows := make([]row, len(order))
	for i, k := range order {
		g := byModel[k]
		rows[i] = row{k, g.n, g.tokens, g.cost}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].tokens > rows[j].tokens })

	lines := []string{"Models seen:", ""}
	for _, r := range rows {
		lines = append(lines, fmt.Sprintf("  %s %s entries  %s tokens  %s",
			util.Pad(r.model, 28, false), util.Pad(fmt.Sprintf("%d", r.n), 6, true),
			util.Pad(util.FormatNumber(r.tokens), 11, true), util.Dollar(r.cost)))
	}
	return strings.Join(lines, "\n")
}

func parseTime(s string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// parseSince accepts an ISO timestamp or a bare YYYY-MM-DD date for the --since filter.
func parseSince(since string) (time.Time, bool) {
	if since == "" {
		return time.Time{}, false
	}
	if t, ok := parseTime(since); ok {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", since); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func isoDaysAgo(days int) string {
	return time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC().Format("2006-01-02")
}

// uuidV4 returns a random RFC 4122 version-4 UUID for tagging manual usage entries.
func uuidV4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "manual-" + util.NowIso()
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
