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

type StatsArgs struct {
	Model   string
	Daily   bool
	Weekly  bool
	Monthly bool
	Graph   bool
	Export  string // "json" | "csv"
}

// RunStats reports AI token consumption. Views: summary (default), per-period tables,
// a daily graph, and JSON/CSV exports.
func RunStats(s *store.Store, cfg config.Config, args StatsArgs) string {
	switch {
	case args.Export == "json":
		return statsExportJSON(s, args)
	case args.Export == "csv":
		return statsExportCSV(s, args)
	case args.Graph:
		return statsGraphView(s)
	case args.Daily:
		return statsPeriodView(s, args, "day")
	case args.Weekly:
		return statsPeriodView(s, args, "week")
	case args.Monthly:
		return statsPeriodView(s, args, "month")
	default:
		return statsSummaryView(s, cfg, args)
	}
}

// statsSelectRows applies the model filter and optional rolling-day window (sinceDays <= 0
// means no window), then sorts newest-first.
func statsSelectRows(s *store.Store, args StatsArgs, sinceDays int) []store.AIUsageRecord {
	needle := strings.ToLower(args.Model)
	out := []store.AIUsageRecord{} // non-nil so the JSON export renders [] not null
	for _, r := range s.ReadAIUsage() {
		if args.Model != "" && !strings.Contains(strings.ToLower(r.Model), needle) {
			continue
		}
		if sinceDays > 0 && !util.WithinDays(r.Timestamp, sinceDays) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out
}

type usageAgg struct {
	input, output, cacheWrite, cacheRead int
	cost                                 float64
}

func statsAggregate(rows []store.AIUsageRecord) usageAgg {
	var a usageAgg
	for _, r := range rows {
		a.input += r.InputTokens
		a.output += r.OutputTokens
		a.cacheWrite += r.CacheWriteTokens
		a.cacheRead += r.CacheReadTokens
		a.cost += r.CostUSD
	}
	return a
}

func statsSummaryView(s *store.Store, cfg config.Config, args StatsArgs) string {
	if len(s.ReadAIUsage()) == 0 {
		return strings.Join([]string{
			"No AI usage data yet.",
			"",
			"Get started by ingesting your existing usage:",
			"  tok usage ingest --claude-code     (parse local Claude Code logs)",
			"  tok usage ingest --ccusage         (use ccusage CLI)",
			"  tok usage log --model <m> --input N --output N   (manual entry)",
		}, "\n")
	}

	today := statsAggregate(statsSelectRows(s, args, 1))
	week := statsAggregate(statsSelectRows(s, args, 7))
	month := statsAggregate(statsSelectRows(s, args, 30))

	row := func(label string, a usageAgg) string {
		return fmt.Sprintf("%s%s %s %s %s %s",
			util.Pad(label, 13, false),
			util.Pad(util.FormatNumber(a.input), 9, true),
			util.Pad(util.FormatNumber(a.output), 10, true),
			util.Pad(util.FormatNumber(a.cacheRead), 9, true),
			util.Pad(util.FormatNumber(a.cacheWrite), 9, true),
			util.Pad(util.Dollar(a.cost), 8, true))
	}

	lines := []string{
		"AI token consumption",
		strings.Repeat("═", 63),
		"",
		"Period          Input       Output    Cache↓    Cache↑    Cost",
		strings.Repeat("─", 63),
		row("Today", today),
		row("Last 7 days", week),
		row("Last 30 days", month),
	}

	// Models used (last 30 days).
	type modelAgg struct {
		tokens int
		cost   float64
	}
	byModel := newGroups[modelAgg]()
	for _, r := range s.ReadAIUsage() {
		if !util.WithinDays(r.Timestamp, 30) {
			continue
		}
		g := byModel.get(r.Model)
		g.tokens += r.InputTokens + r.OutputTokens
		g.cost += r.CostUSD
	}
	type modelRow struct {
		model  string
		tokens int
		cost   float64
	}
	var modelRows []modelRow
	totalTokens := 0
	for _, e := range byModel.entries() {
		modelRows = append(modelRows, modelRow{e.key, e.val.tokens, e.val.cost})
		totalTokens += e.val.tokens
	}
	sort.SliceStable(modelRows, func(i, j int) bool { return modelRows[i].tokens > modelRows[j].tokens })
	if len(modelRows) > 0 {
		lines = append(lines, "", "Models used (last 30 days):")
		for _, r := range head(modelRows, 10) {
			pct := 0.0
			if totalTokens > 0 {
				pct = float64(r.tokens) / float64(totalTokens) * 100
			}
			lines = append(lines, fmt.Sprintf("  %s %s tokens  %s%%  %s",
				util.Pad(r.model, 24, false), util.Pad(util.FormatNumber(r.tokens), 11, true),
				util.Pad(fmt.Sprintf("%.0f", pct), 3, true), util.Dollar(r.cost)))
		}
	}

	// Cache efficiency.
	allInput := month.input + month.cacheRead
	cacheReadPct := 0.0
	if allInput > 0 {
		cacheReadPct = float64(month.cacheRead) / float64(allInput) * 100
	}
	hitRate := 0.0
	if month.cacheRead+month.cacheWrite > 0 {
		hitRate = float64(month.cacheRead) / float64(month.cacheRead+month.cacheWrite) * 100
	}
	cacheSavingsUsd := float64(month.cacheRead) / 1000 * (cfg.TokenPricePer1k * 0.9)
	lines = append(lines,
		"",
		"Cache efficiency (last 30 days):",
		fmt.Sprintf("  Cache reads:    %s tokens (%.0f%% of all input)", util.Pad(util.FormatNumber(month.cacheRead), 11, true), cacheReadPct),
		fmt.Sprintf("  Cache writes:   %s tokens", util.Pad(util.FormatNumber(month.cacheWrite), 11, true)),
		fmt.Sprintf("  Cache hit rate: %.1f%%", hitRate),
		fmt.Sprintf("  Est. cache savings: %s", util.Dollar(cacheSavingsUsd)))

	// Source attribution - most recently ingested source.
	lastBySource := newGroups[string]()
	for _, r := range s.ReadAIUsage() {
		p := lastBySource.get(r.Source)
		if *p == "" || r.Timestamp > *p {
			*p = r.Timestamp
		}
	}
	type sourceRow struct {
		source string
		last   string
	}
	var sources []sourceRow
	for _, e := range lastBySource.entries() {
		sources = append(sources, sourceRow{e.key, *e.val})
	}
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].last > sources[j].last })
	if len(sources) > 0 {
		lines = append(lines, "",
			fmt.Sprintf("Data source: %s (last ingested: %s)", sources[0].source, util.RelativeTime(sources[0].last)))
	}
	lines = append(lines, "No data yet? Run: tok usage ingest --ccusage")
	return strings.Join(lines, "\n")
}

func statsPeriodView(s *store.Store, args StatsArgs, period string) string {
	rows := statsSelectRows(s, args, 0)
	if len(rows) == 0 {
		return "No AI usage data yet."
	}
	buckets := newGroups[usageAgg]()
	for _, r := range rows {
		g := buckets.get(periodKey(r.Timestamp, period))
		g.input += r.InputTokens
		g.output += r.OutputTokens
		g.cacheWrite += r.CacheWriteTokens
		g.cacheRead += r.CacheReadTokens
		g.cost += r.CostUSD
	}
	entries := buckets.entries()
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].key > entries[j].key })

	lines := []string{
		"AI usage by " + period,
		strings.Repeat("─", 70),
		fmt.Sprintf("%s    Input        Output       Cache↓        Cache↑       Cost", util.Pad(period, 10, false)),
		strings.Repeat("─", 70),
	}
	for _, e := range head(entries, 30) {
		v := e.val
		lines = append(lines, fmt.Sprintf("%s  %s %s %s %s %s",
			util.Pad(e.key, 10, false),
			util.Pad(util.FormatNumber(v.input), 9, true),
			util.Pad(util.FormatNumber(v.output), 11, true),
			util.Pad(util.FormatNumber(v.cacheRead), 11, true),
			util.Pad(util.FormatNumber(v.cacheWrite), 11, true),
			util.Pad(util.Dollar(v.cost), 9, true)))
	}
	return strings.Join(lines, "\n")
}

func statsGraphView(s *store.Store) string {
	byDay := newGroups[int]()
	for _, r := range s.ReadAIUsage() {
		if !util.WithinDays(r.Timestamp, 30) {
			continue
		}
		*byDay.get(day10(r.Timestamp)) += r.InputTokens + r.OutputTokens
	}
	type row struct {
		day    string
		tokens int
	}
	var rows []row
	for _, e := range byDay.entries() {
		rows = append(rows, row{e.key, *e.val})
	}
	if len(rows) == 0 {
		return "No data yet."
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].day < rows[j].day })
	vals := make([]int, len(rows))
	for i, r := range rows {
		vals[i] = r.tokens
	}
	max := maxInt(vals)
	lines := []string{"Daily AI token consumption (last 30 days):", ""}
	for _, r := range rows {
		lines = append(lines, fmt.Sprintf("  %s %s %s", r.day, bar(r.tokens, max, 40), util.FormatNumber(r.tokens)))
	}
	return strings.Join(lines, "\n")
}

func statsExportJSON(s *store.Store, args StatsArgs) string {
	return jsonPretty(statsSelectRows(s, args, 0))
}

func statsExportCSV(s *store.Store, args StatsArgs) string {
	rows := statsSelectRows(s, args, 0)
	lines := []string{strings.Join([]string{
		"timestamp", "session_id", "model", "source",
		"input_tokens", "output_tokens", "cache_write_tokens", "cache_read_tokens",
		"cost_usd", "day", "week", "month", "total_tokens",
	}, ",")}
	for _, r := range rows {
		total := r.InputTokens + r.OutputTokens + r.CacheWriteTokens + r.CacheReadTokens
		lines = append(lines, strings.Join([]string{
			r.Timestamp,
			util.EscapeCsv(r.SessionID),
			util.EscapeCsv(r.Model),
			util.EscapeCsv(r.Source),
			strconv.Itoa(r.InputTokens),
			strconv.Itoa(r.OutputTokens),
			strconv.Itoa(r.CacheWriteTokens),
			strconv.Itoa(r.CacheReadTokens),
			jsNumber(r.CostUSD),
			util.IsoDay(r.Timestamp),
			util.IsoWeek(r.Timestamp),
			util.IsoMonth(r.Timestamp),
			strconv.Itoa(total),
		}, ","))
	}
	return strings.Join(lines, "\n")
}

// periodKey buckets a timestamp by day, ISO week, or month.
func periodKey(ts, period string) string {
	switch period {
	case "day":
		return util.IsoDay(ts)
	case "week":
		return util.IsoWeek(ts)
	default:
		return util.IsoMonth(ts)
	}
}
