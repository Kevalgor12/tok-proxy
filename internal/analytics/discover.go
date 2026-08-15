package analytics

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/store"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// knownFiltered are the command types tok already compresses, so they never count as a
// missed opportunity.
var knownFiltered = map[string]bool{
	"git status": true, "git st": true, "git diff": true, "git log": true, "git push": true,
	"git pull": true, "git add": true, "git commit": true, "git branch": true, "git fetch": true,
	"npm install": true, "npm i": true, "npm add": true, "npm list": true, "npm ls": true,
	"npm outdated": true, "npm run": true,
	"pnpm install": true, "pnpm add": true, "pnpm list": true, "pnpm outdated": true,
	"yarn": true, "yarn add": true, "yarn list": true, "yarn outdated": true,
	"tsc":  true,
	"jest": true, "vitest": true, "mocha": true,
	"eslint": true, "biome": true, "prettier": true,
	"ls": true, "cat": true, "grep": true, "find": true, "diff": true, "json": true, "smart": true,
	"docker ps": true, "docker images": true, "docker logs": true, "docker compose": true,
	"kubectl get": true, "kubectl logs": true,
}

// potentialReductions maps a command fragment to the fraction of output tok could plausibly
// cut. Ordered because the first substring match wins (matching JS object iteration order).
var potentialReductions = []struct {
	key   string
	value float64
}{
	{"docker logs", 0.85},
	{"docker compose", 0.7},
	{"kubectl logs", 0.85},
	{"npm run dev", 0.7},
	{"npm run start", 0.7},
	{"tail", 0.85},
	{"find", 0.6},
	{"curl", 0.5},
}

// RunDiscover surfaces frequently-run, currently-unoptimized commands and the tokens tok
// could save by proxying them.
func RunDiscover(s *store.Store, cfg config.Config) string {
	type agg struct {
		runs    int
		totalIn int
	}
	grouped := newGroups[agg]()
	for _, r := range s.ReadCommands() {
		if !util.WithinDays(r.Timestamp, 7) || r.SavingsPct != 0 {
			continue
		}
		g := grouped.get(r.CmdType)
		g.runs++
		g.totalIn += r.InputBytes
	}
	type row struct {
		cmdType string
		runs    int
		avgIn   float64
	}
	var rows []row
	for _, e := range grouped.entries() {
		avg := 0.0
		if e.val.runs > 0 {
			avg = float64(e.val.totalIn) / float64(e.val.runs)
		}
		rows = append(rows, row{e.key, e.val.runs, avg})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].runs > rows[j].runs })

	var candidates []row
	for _, r := range rows {
		if !knownFiltered[r.cmdType] {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return "No missed optimizations detected this week."
	}

	lines := []string{"Missed optimization opportunities (last 7 days):"}
	totalPotential := 0.0
	for _, c := range head(candidates, 10) {
		pct := guessReduction(c.cmdType)
		potentialBytes := float64(c.runs) * c.avgIn * pct
		totalPotential += potentialBytes
		tokens := util.BytesToTokens(int(math.Floor(potentialBytes)))
		lines = append(lines, fmt.Sprintf("  %s %s runs × ~%.0f%% savings = ~%s tokens/week potential",
			util.Pad(c.cmdType, 18, false), util.Pad(strconv.Itoa(c.runs), 3, true), pct*100, util.FormatNumber(tokens)))
	}
	totalTokens := util.BytesToTokens(int(math.Floor(totalPotential)))
	totalUsd := float64(totalTokens) / 1000 * cfg.TokenPricePer1k
	lines = append(lines,
		"",
		fmt.Sprintf("Total potential:  ~%s tokens/week (~%s/week at current pricing)", util.FormatNumber(totalTokens), util.Dollar(totalUsd)),
		"Fix with: tok init (auto-rewrites these via hooks)",
		"Or run manually: tok <cmd> <args>")
	return strings.Join(lines, "\n")
}

func guessReduction(cmdType string) float64 {
	for _, r := range potentialReductions {
		if strings.Contains(cmdType, r.key) {
			return r.value
		}
	}
	return 0.6
}
