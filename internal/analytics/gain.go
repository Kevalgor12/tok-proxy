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

type GainArgs struct {
	Graph   bool
	History bool
	Daily   bool
	Format  string
}

type dayBucket struct {
	inBytes  int
	outBytes int
	saved    int
	count    int
}

// RunGain reports filter-compression savings. Views: summary (default), graph, history,
// daily table, and a JSON export.
func RunGain(s *store.Store, cfg config.Config, args GainArgs) string {
	switch {
	case args.Format == "json":
		return gainJSONExport(s)
	case args.Graph:
		return gainGraphView(s)
	case args.History:
		return gainHistoryView(s)
	case args.Daily:
		return gainDailyView(s)
	default:
		return gainSummaryView(s, cfg)
	}
}

func gainAggregate(cmds []store.CommandRow, days int) dayBucket {
	var b dayBucket
	for _, r := range cmds {
		if !util.WithinDays(r.Timestamp, days) {
			continue
		}
		b.inBytes += r.InputBytes
		b.outBytes += r.OutBytes
		b.saved += r.SavedBytes
		b.count++
	}
	return b
}

func gainTokRow(b dayBucket, cfg config.Config) string {
	inT := util.BytesToTokens(b.inBytes)
	outT := util.BytesToTokens(b.outBytes)
	savedT := util.BytesToTokens(b.saved)
	pct := 0.0
	if b.inBytes > 0 {
		pct = float64(b.saved) / float64(b.inBytes) * 100
	}
	cost := float64(savedT) / 1000 * cfg.TokenPricePer1k
	return fmt.Sprintf("%s → %s tokens   saved %.0f%%  (~%s)",
		util.FormatNumber(inT), util.FormatNumber(outT), pct, util.Dollar(cost))
}

func gainSummaryView(s *store.Store, cfg config.Config) string {
	cmds := s.ReadCommands()

	lines := []string{
		"tok savings - filter compression",
		strings.Repeat("═", 58),
		"Today:        " + gainTokRow(gainAggregate(cmds, 1), cfg),
		"Last 7 days:  " + gainTokRow(gainAggregate(cmds, 7), cfg),
		"All time:     " + gainTokRow(gainAggregate(cmds, 36500), cfg),
		"",
		"Top commands today:",
	}

	type todayGroup struct {
		runs   int
		sumPct float64
	}
	groups := newGroups[todayGroup]()
	for _, r := range cmds {
		if !util.WithinDays(r.Timestamp, 1) {
			continue
		}
		g := groups.get(r.CmdType)
		g.runs++
		g.sumPct += r.SavingsPct
	}
	type topRow struct {
		cmdType string
		runs    int
		avgPct  float64
	}
	var topCmds []topRow
	for _, e := range groups.entries() {
		avg := 0.0
		if e.val.runs > 0 {
			avg = e.val.sumPct / float64(e.val.runs)
		}
		topCmds = append(topCmds, topRow{e.key, e.val.runs, avg})
	}
	sort.SliceStable(topCmds, func(i, j int) bool { return topCmds[i].runs > topCmds[j].runs })
	topCmds = head(topCmds, 8)
	if len(topCmds) == 0 {
		lines = append(lines, "  (no data yet)")
	} else {
		for _, row := range topCmds {
			lines = append(lines, fmt.Sprintf("  %s %s %d runs",
				util.Pad(row.cmdType, 14, false), util.Pad(util.Percent(row.avgPct, 0), 5, false), row.runs))
		}
	}

	unopt := newGroups[int]()
	for _, r := range cmds {
		if r.SavingsPct < 1 && util.WithinDays(r.Timestamp, 7) {
			*unopt.get(r.CmdType) += 1
		}
	}
	type unoptRow struct {
		cmdType string
		runs    int
	}
	var unoptRows []unoptRow
	for _, e := range unopt.entries() {
		unoptRows = append(unoptRows, unoptRow{e.key, *e.val})
	}
	sort.SliceStable(unoptRows, func(i, j int) bool { return unoptRows[i].runs > unoptRows[j].runs })
	unoptRows = head(unoptRows, 5)
	if len(unoptRows) > 0 {
		var compact []string
		for _, r := range unoptRows {
			compact = append(compact, fmt.Sprintf("%s (×%d)", r.cmdType, r.runs))
		}
		lines = append(lines,
			"",
			"Not yet optimized (0% savings):",
			"  "+strings.Join(compact, "  "),
			"  → Run: tok discover for optimization suggestions")
	}
	return strings.Join(lines, "\n")
}

func gainGraphView(s *store.Store) string {
	byDay := newGroups[int]()
	for _, r := range s.ReadCommands() {
		if !util.WithinDays(r.Timestamp, 30) {
			continue
		}
		*byDay.get(day10(r.Timestamp)) += r.SavedBytes
	}
	type row struct {
		day string
		sB  int
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
		vals[i] = r.sB
	}
	max := maxInt(vals)
	lines := []string{"Daily savings (last 30 days, bytes):", ""}
	for _, r := range rows {
		lines = append(lines, fmt.Sprintf("  %s %s %s", r.day, bar(r.sB, max, 40), util.FormatNumber(r.sB)))
	}
	return strings.Join(lines, "\n")
}

func gainHistoryView(s *store.Store) string {
	rows := reversed(tail(s.ReadCommands(), 20))
	if len(rows) == 0 {
		return "No history yet."
	}
	lines := []string{"Last 20 commands:", ""}
	for _, r := range rows {
		ts := strings.ReplaceAll(r.Timestamp, "T", " ")
		if len(ts) > 19 {
			ts = ts[:19]
		}
		lines = append(lines, fmt.Sprintf("  %s %s %s → %s  %s",
			ts,
			util.Pad(r.CmdType, 14, false),
			util.Pad(util.FormatNumber(r.InputBytes), 8, true),
			util.Pad(util.FormatNumber(r.OutBytes), 8, true),
			util.Pad(util.Percent(r.SavingsPct, 0), 5, true)))
	}
	return strings.Join(lines, "\n")
}

func gainDailyView(s *store.Store) string {
	type g struct{ runs, inB, outB, sB int }
	byDay := newGroups[g]()
	for _, r := range s.ReadCommands() {
		if !util.WithinDays(r.Timestamp, 30) {
			continue
		}
		x := byDay.get(day10(r.Timestamp))
		x.runs++
		x.inB += r.InputBytes
		x.outB += r.OutBytes
		x.sB += r.SavedBytes
	}
	type row struct {
		day                 string
		runs, inB, outB, sB int
	}
	var rows []row
	for _, e := range byDay.entries() {
		rows = append(rows, row{e.key, e.val.runs, e.val.inB, e.val.outB, e.val.sB})
	}
	if len(rows) == 0 {
		return "No data yet."
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].day > rows[j].day })
	lines := []string{
		"Daily savings (last 30 days)",
		strings.Repeat("─", 70),
		"Day         Runs      Input         Output          Saved      %",
		strings.Repeat("─", 70),
	}
	for _, r := range rows {
		pct := 0.0
		if r.inB > 0 {
			pct = float64(r.sB) / float64(r.inB) * 100
		}
		lines = append(lines, fmt.Sprintf("%s  %s %s %s %s %s",
			r.day,
			util.Pad(strconv.Itoa(r.runs), 5, true),
			util.Pad(util.FormatNumber(r.inB), 11, true),
			util.Pad(util.FormatNumber(r.outB), 13, true),
			util.Pad(util.FormatNumber(r.sB), 13, true),
			util.Pad(util.Percent(pct, 0), 5, true)))
	}
	return strings.Join(lines, "\n")
}

func gainJSONExport(s *store.Store) string {
	return jsonPretty(map[string]any{"rows": reversed(s.ReadCommands())})
}
