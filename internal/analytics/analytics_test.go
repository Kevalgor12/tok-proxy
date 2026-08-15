package analytics

import (
	"math"
	"strings"
	"testing"

	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/store"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

func TestPureHelpers(t *testing.T) {
	if got := guessReduction("docker logs -f app"); got != 0.85 {
		t.Errorf("guessReduction docker logs = %v", got)
	}
	if got := guessReduction("mystery-cmd"); got != 0.6 {
		t.Errorf("guessReduction default = %v", got)
	}
	if cpt, est := calcWeightedCpt(periodStats{in: 1000, out: 100, cost: 0}, 0.015); !est || math.Abs(cpt-0.000015) > 1e-12 {
		t.Errorf("calcWeightedCpt fallback = %v est=%v", cpt, est)
	}
	if cpt, est := calcWeightedCpt(periodStats{in: 1000, cost: 1.5}, 0.015); est || math.Abs(cpt-0.0015) > 1e-12 {
		t.Errorf("calcWeightedCpt verified = %v est=%v", cpt, est)
	}
	if got := bar(5, 10, 40); got != strings.Repeat("█", 20) {
		t.Errorf("bar half = %q", got)
	}
	if bar(1, 0, 40) != "" {
		t.Error("bar with zero max should be empty")
	}
	if maxInt([]int{3, 9, 4}) != 9 {
		t.Error("maxInt")
	}
	if jsNumber(0) != "0" || jsNumber(1.5) != "1.5" {
		t.Errorf("jsNumber = %q %q", jsNumber(0), jsNumber(1.5))
	}
}

func TestReportsSmoke(t *testing.T) {
	t.Setenv("TOK_HOME", t.TempDir())
	s := store.Open()
	now := util.NowIso()
	s.AppendCommand(store.CommandRow{Timestamp: now, CmdType: "git status", InputBytes: 1000, OutBytes: 200, SavedBytes: 800, SavingsPct: 80})
	s.AppendCommand(store.CommandRow{Timestamp: now, CmdType: "tail", InputBytes: 500, OutBytes: 500, SavedBytes: 0, SavingsPct: 0})
	s.AppendAIUsage(store.AIUsageRecord{Timestamp: now, SessionID: "sess1", Model: "claude-opus-4-5", Source: "manual", InputTokens: 1000, OutputTokens: 200, CostUSD: 0.05})

	cfg := config.Defaults()
	if g := RunGain(s, cfg, GainArgs{}); !strings.Contains(g, "tok savings") || !strings.Contains(g, "git status") {
		t.Errorf("gain summary = %q", g)
	}
	if j := RunGain(s, cfg, GainArgs{Format: "json"}); !strings.Contains(j, `"rows"`) || !strings.Contains(j, "git status") {
		t.Errorf("gain json = %q", j)
	}
	if st := RunStats(s, cfg, StatsArgs{}); !strings.Contains(st, "AI token consumption") || !strings.Contains(st, "claude-opus-4-5") {
		t.Errorf("stats = %q", st)
	}
	if e := RunEcon(s, cfg, EconArgs{}); !strings.Contains(e, "tok economics dashboard") {
		t.Errorf("econ = %q", e)
	}
	if se := RunSession(s); !strings.Contains(se, "Recent sessions") {
		t.Errorf("session = %q", se)
	}
	if d := RunDiscover(s, cfg); !strings.Contains(d, "Missed optimization") || !strings.Contains(d, "tail") {
		t.Errorf("discover = %q", d)
	}
}
