package analytics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/store"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

type EconArgs struct {
	Daily   bool
	Weekly  bool
	Monthly bool
	Export  string // "json" | "csv"
}

type periodStats struct {
	in, out, cw, cr int
	cost            float64
	saved           int
}

// RunEcon renders the cost dashboard: what tok's savings are worth against actual AI spend.
func RunEcon(s *store.Store, cfg config.Config, args EconArgs) string {
	switch {
	case args.Export == "json":
		return econExportJSON(s, cfg)
	case args.Export == "csv":
		return econExportCSV(s)
	case args.Daily:
		return econPeriodView(s, cfg, "day")
	case args.Weekly:
		return econPeriodView(s, cfg, "week")
	case args.Monthly:
		return econPeriodView(s, cfg, "month")
	default:
		return econSummaryView(s, cfg)
	}
}

// econGetStats sums AI usage plus filter savings within the last sinceDays.
func econGetStats(s *store.Store, sinceDays int) periodStats {
	var p periodStats
	for _, r := range s.ReadAIUsage() {
		if !util.WithinDays(r.Timestamp, sinceDays) {
			continue
		}
		p.in += r.InputTokens
		p.out += r.OutputTokens
		p.cw += r.CacheWriteTokens
		p.cr += r.CacheReadTokens
		p.cost += r.CostUSD
	}
	for _, r := range s.ReadCommands() {
		if !util.WithinDays(r.Timestamp, sinceDays) {
			continue
		}
		p.saved += r.SavedBytes
	}
	return p
}

// calcWeightedCpt derives an effective cost-per-token from the real bill, weighting output
// and cache tokens by their relative price; it falls back to the configured rate when there
// is no verified cost to divide.
func calcWeightedCpt(s periodStats, fallbackPer1k float64) (inputCpt float64, estimated bool) {
	weightedUnits := float64(s.in) + 5.0*float64(s.out) + 1.25*float64(s.cw) + 0.1*float64(s.cr)
	if s.cost > 0 && weightedUnits > 0 {
		return s.cost / weightedUnits, false
	}
	return fallbackPer1k / 1000, true
}

func priceModel(s periodStats, p config.ModelPricing) float64 {
	return float64(s.in)/1000*p.InputPer1k +
		float64(s.out)/1000*p.OutputPer1k +
		float64(s.cw)/1000*p.CacheWritePer1k +
		float64(s.cr)/1000*p.CacheReadPer1k
}

func econSummaryView(s *store.Store, cfg config.Config) string {
	month := econGetStats(s, 30)
	savedTokens := util.BytesToTokens(month.saved)

	inputCpt, estimated := calcWeightedCpt(month, cfg.TokenPricePer1k)
	savedValueUsd := float64(savedTokens) * inputCpt
	withoutTok := month.cost + savedValueUsd
	savedPctOfBill := 0.0
	if withoutTok > 0 {
		savedPctOfBill = savedValueUsd / withoutTok * 100
	}

	costNote := "(verified)"
	if estimated {
		costNote = "(estimated - run tok usage ingest --ccusage for actuals)"
	}

	lines := []string{
		"tok economics dashboard",
		strings.Repeat("═", 63),
		"",
		"LAST 30 DAYS",
		strings.Repeat("─", 63),
		fmt.Sprintf("AI tokens consumed    %s input  +  %s output", util.Pad(util.FormatNumber(month.in), 11, true), util.FormatNumber(month.out)),
		fmt.Sprintf("Cache tokens          %s reads  +  %s writes", util.Pad(util.FormatNumber(month.cr), 11, true), util.FormatNumber(month.cw)),
		fmt.Sprintf("Actual cost %s  %s", util.Pad(costNote, 36, true), util.Dollar(month.cost)),
		fmt.Sprintf("Effective input CPT                 %s / token", util.Pad(util.Dollar(inputCpt), 7, true)),
		"",
		fmt.Sprintf("tok filter savings    %s tokens prevented", util.Pad(util.FormatNumber(savedTokens), 11, true)),
		fmt.Sprintf("Cost avoided (weighted)             %s", util.Pad(util.Dollar(savedValueUsd), 7, true)),
		strings.Repeat("─", 63),
		fmt.Sprintf("Net cost WITH tok                   %s", util.Pad(util.Dollar(month.cost), 7, true)),
		fmt.Sprintf("Estimated cost WITHOUT tok          %s", util.Pad(util.Dollar(withoutTok), 7, true)),
		fmt.Sprintf("tok saved you                       %s of your AI bill", util.Pad(util.Percent(savedPctOfBill, 1), 7, true)),
	}

	// Context window health.
	info := computeSessionStats(s)
	lines = append(lines, "", "CONTEXT WINDOW HEALTH", strings.Repeat("─", 63))
	lines = append(lines, fmt.Sprintf("Avg tokens per session       %s", util.Pad(util.FormatNumber(info.avgTokens), 11, true)))
	if info.hasLargest {
		lines = append(lines, fmt.Sprintf("Largest session              %s tokens   (%s)",
			util.Pad(util.FormatNumber(info.largestTokens), 11, true), info.largestDay))
	}
	warn := "✓ comfortable"
	if info.over100k > 0 {
		warn = "⚠ approaching limit"
	}
	lines = append(lines, fmt.Sprintf("Sessions > 100K tokens       %s          %s", util.Pad(strconv.Itoa(info.over100k), 11, true), warn))
	cacheHit := 0.0
	if month.cr+month.cw > 0 {
		cacheHit = float64(month.cr) / float64(month.cr+month.cw) * 100
	}
	hitTag := "⚠ low cache reuse"
	if cacheHit >= 50 {
		hitTag = "✓ good caching"
	}
	lines = append(lines, fmt.Sprintf("Cache hit rate               %s          %s", util.Pad(util.Percent(cacheHit, 1), 11, true), hitTag))

	// Model cost comparison (this session = last day).
	lines = append(lines, "", "MODEL COST COMPARISON (this session)", strings.Repeat("─", 63))
	compareModels := []string{"claude-opus-4-5", "claude-sonnet-4-5", "claude-haiku-4-5"}
	session := econGetStats(s, 1)
	if session.in+session.out == 0 {
		lines = append(lines, "  (no AI usage today to compare)")
	} else {
		opusCost := priceModel(session, cfg.ModelPricing["claude-opus-4-5"])
		for _, m := range compareModels {
			p, ok := cfg.ModelPricing[m]
			if !ok {
				continue
			}
			tag := "(actual)"
			if m != "claude-opus-4-5" {
				tag = fmt.Sprintf("(estimated at %s pricing)", strings.Split(m, "-")[1])
			}
			lines = append(lines, fmt.Sprintf("%s %s   %s", util.Pad(m, 20, false), util.Pad(util.Dollar(priceModel(session, p)), 7, true), tag))
		}
		sonnet := priceModel(session, cfg.ModelPricing["claude-sonnet-4-5"])
		if opusCost > 0 {
			reduction := (opusCost - sonnet) / opusCost * 100
			lines = append(lines, fmt.Sprintf("→ Switch to Sonnet → save ~%.0f%% on cost with comparable quality", reduction))
		}
	}

	return strings.Join(lines, "\n")
}

type sessionInfo struct {
	avgTokens     int
	hasLargest    bool
	largestDay    string
	largestTokens int
	over100k      int
}

// computeSessionStats groups AI usage by session id, tracking each session's total tokens
// and earliest day, for the context-window health panel.
func computeSessionStats(s *store.Store) sessionInfo {
	type sess struct {
		tokens int
		day    string
	}
	bySession := map[string]*sess{}
	var order []string
	for _, r := range s.ReadAIUsage() {
		day := day10(r.Timestamp)
		g, ok := bySession[r.SessionID]
		if !ok {
			g = &sess{day: day}
			bySession[r.SessionID] = g
			order = append(order, r.SessionID)
		}
		g.tokens += r.InputTokens + r.OutputTokens
		if day < g.day {
			g.day = day
		}
	}
	if len(order) == 0 {
		return sessionInfo{}
	}
	total := 0
	largestTokens := bySession[order[0]].tokens
	largestDay := bySession[order[0]].day
	over := 0
	for _, k := range order {
		g := bySession[k]
		total += g.tokens
		if g.tokens > largestTokens {
			largestTokens = g.tokens
			largestDay = g.day
		}
		if g.tokens > 100000 {
			over++
		}
	}
	return sessionInfo{
		avgTokens:     total / len(order),
		hasLargest:    true,
		largestDay:    largestDay,
		largestTokens: largestTokens,
		over100k:      over,
	}
}

func econPeriodView(s *store.Store, cfg config.Config, period string) string {
	buckets := newGroups[periodStats]()
	for _, r := range s.ReadAIUsage() {
		g := buckets.get(periodKey(r.Timestamp, period))
		g.in += r.InputTokens
		g.out += r.OutputTokens
		g.cw += r.CacheWriteTokens
		g.cr += r.CacheReadTokens
		g.cost += r.CostUSD
	}
	for _, r := range s.ReadCommands() {
		buckets.get(periodKey(r.Timestamp, period)).saved += r.SavedBytes
	}
	entries := buckets.entries()
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].key > entries[j].key })

	lines := []string{
		"Economics by " + period,
		strings.Repeat("─", 75),
		fmt.Sprintf("%s    Cost      Saved$    ROI%%   Tokens(in+out)   Saved tokens", util.Pad(period, 10, false)),
		strings.Repeat("─", 75),
	}
	for _, e := range head(entries, 30) {
		v := e.val
		inputCpt, _ := calcWeightedCpt(*v, cfg.TokenPricePer1k)
		savedTokens := util.BytesToTokens(v.saved)
		savedUsd := float64(savedTokens) * inputCpt
		roi := 0.0
		if v.cost > 0 {
			roi = savedUsd / v.cost * 100
		}
		lines = append(lines, fmt.Sprintf("%s  %s %s %s  %s %s",
			util.Pad(e.key, 10, false),
			util.Pad(util.Dollar(v.cost), 8, true),
			util.Pad(util.Dollar(savedUsd), 9, true),
			util.Pad(util.Percent(roi, 0), 6, true),
			util.Pad(util.FormatNumber(v.in+v.out), 13, true),
			util.Pad(util.FormatNumber(savedTokens), 13, true)))
	}
	return strings.Join(lines, "\n")
}

func econExportJSON(s *store.Store, cfg config.Config) string {
	month := econGetStats(s, 30)
	savedTokens := util.BytesToTokens(month.saved)
	inputCpt, estimated := calcWeightedCpt(month, cfg.TokenPricePer1k)
	return jsonPretty(map[string]any{
		"period":              "last_30_days",
		"input_tokens":        month.in,
		"output_tokens":       month.out,
		"cache_write_tokens":  month.cw,
		"cache_read_tokens":   month.cr,
		"cost_usd":            month.cost,
		"saved_bytes":         month.saved,
		"saved_tokens":        savedTokens,
		"weighted_input_cpt":  inputCpt,
		"saved_usd_estimated": float64(savedTokens) * inputCpt,
		"cost_estimated":      estimated,
	})
}

func econExportCSV(s *store.Store) string {
	cmdsByDay := newGroups[int]()
	for _, c := range s.ReadCommands() {
		*cmdsByDay.get(util.IsoDay(c.Timestamp)) += c.SavedBytes
	}
	savedForDay := func(day string) int {
		if v, ok := cmdsByDay.m[day]; ok {
			return *v
		}
		return 0
	}

	lines := []string{strings.Join([]string{
		"timestamp", "day", "week", "month", "model", "source",
		"input_tokens", "output_tokens", "cache_write_tokens", "cache_read_tokens",
		"cost_usd", "saved_bytes_day", "saved_tokens_day",
	}, ",")}
	for _, r := range s.ReadAIUsage() {
		day := util.IsoDay(r.Timestamp)
		savedB := savedForDay(day)
		lines = append(lines, strings.Join([]string{
			r.Timestamp,
			day,
			util.IsoWeek(r.Timestamp),
			util.IsoMonth(r.Timestamp),
			util.EscapeCsv(r.Model),
			util.EscapeCsv(r.Source),
			strconv.Itoa(r.InputTokens),
			strconv.Itoa(r.OutputTokens),
			strconv.Itoa(r.CacheWriteTokens),
			strconv.Itoa(r.CacheReadTokens),
			jsNumber(r.CostUSD),
			strconv.Itoa(savedB),
			strconv.Itoa(util.BytesToTokens(savedB)),
		}, ","))
	}
	return strings.Join(lines, "\n")
}
