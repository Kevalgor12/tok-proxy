package analytics

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Kevalgor12/tok-proxy/internal/store"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

type sessionGroup struct {
	start string
	end   string
	rows  []store.CommandRow
}

// RunSession groups logged commands into activity sessions (a gap over 30 minutes starts a
// new one) and reports how much of each was proxied.
func RunSession(s *store.Store) string {
	rows := s.ReadCommands()
	sorted := append([]store.CommandRow(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	if len(sorted) == 0 {
		return "No commands logged yet."
	}

	var sessions []sessionGroup
	var current []store.CommandRow
	var lastT time.Time
	flush := func() {
		if len(current) == 0 {
			return
		}
		sessions = append(sessions, sessionGroup{
			start: current[0].Timestamp,
			end:   current[len(current)-1].Timestamp,
			rows:  append([]store.CommandRow(nil), current...),
		})
	}
	for _, r := range sorted {
		t, _ := parseTS(r.Timestamp)
		if len(current) == 0 {
			current = []store.CommandRow{r}
		} else if t.Sub(lastT) > 30*time.Minute {
			flush()
			current = []store.CommandRow{r}
		} else {
			current = append(current, r)
		}
		lastT = t
	}
	flush()

	lines := []string{"Recent sessions:"}
	for _, sess := range reversed(tail(sessions, 10)) {
		total := len(sess.rows)
		proxied, saved := 0, 0
		for _, r := range sess.rows {
			if r.SavingsPct > 0 {
				proxied++
			}
			saved += r.SavedBytes
		}
		pct := 0.0
		if total > 0 {
			pct = float64(proxied) / float64(total) * 100
		}
		lines = append(lines, fmt.Sprintf("  %s   %s proxied (%d/%d commands)   saved %s bytes",
			formatRange(sess.start, sess.end), util.Pad(util.Percent(pct, 0), 4, true), proxied, total, util.FormatNumber(saved)))
	}

	// Unproxied commands this week.
	unprox := newGroups[int]()
	for _, r := range sorted {
		if r.SavingsPct == 0 && util.WithinDays(r.Timestamp, 7) {
			*unprox.get(r.CmdType) += 1
		}
	}
	type row struct {
		cmdType string
		runs    int
	}
	var rows2 []row
	for _, e := range unprox.entries() {
		rows2 = append(rows2, row{e.key, *e.val})
	}
	sort.SliceStable(rows2, func(i, j int) bool { return rows2[i].runs > rows2[j].runs })
	rows2 = head(rows2, 6)
	if len(rows2) > 0 {
		var parts []string
		for _, r := range rows2 {
			parts = append(parts, fmt.Sprintf("%s (×%d)", r.cmdType, r.runs))
		}
		lines = append(lines, "", "Unproxied commands this week:", "  "+strings.Join(parts, "   "), "  → Run: tok discover for suggestions")
	}
	return strings.Join(lines, "\n")
}

func formatRange(start, end string) string {
	s, ok1 := parseTS(start)
	e, ok2 := parseTS(end)
	if !ok1 || !ok2 {
		return start
	}
	return fmt.Sprintf("%s %s-%s", s.Local().Format("Jan 2"), s.Local().Format("15:04"), e.Local().Format("15:04"))
}

func parseTS(s string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}
