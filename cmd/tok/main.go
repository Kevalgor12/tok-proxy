// Command tok is a CLI proxy that runs a developer command, compresses its output, and
// records the token savings locally. It also serves the Node-free PreToolUse hook that AI
// coding tools fire to rewrite Bash commands through tok automatically.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Kevalgor12/tok-proxy/internal/analytics"
	"github.com/Kevalgor12/tok-proxy/internal/cache"
	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/constants"
	"github.com/Kevalgor12/tok-proxy/internal/doctor"
	"github.com/Kevalgor12/tok-proxy/internal/filter"
	"github.com/Kevalgor12/tok-proxy/internal/handlers"
	"github.com/Kevalgor12/tok-proxy/internal/hook"
	"github.com/Kevalgor12/tok-proxy/internal/install"
	"github.com/Kevalgor12/tok-proxy/internal/registry"
	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/store"
	"github.com/Kevalgor12/tok-proxy/internal/usage"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

type globalFlags struct {
	ultra       bool
	verbose     int
	noTrack     bool
	noCache     bool
	showHelp    bool
	showVersion bool
}

func parseGlobalFlags(argv []string) (globalFlags, []string) {
	var f globalFlags
	var rest []string
	for _, arg := range argv {
		switch arg {
		case "-u", "--ultra-compact":
			f.ultra = true
		case "-v":
			f.verbose = max(f.verbose, 1)
		case "-vv":
			f.verbose = max(f.verbose, 2)
		case "-vvv":
			f.verbose = max(f.verbose, 3)
		case "--no-track":
			f.noTrack = true
		case "--no-cache":
			f.noCache = true
		case "--version":
			f.showVersion = true
		case "--help", "-h":
			f.showHelp = true
		default:
			rest = append(rest, arg)
		}
	}
	if os.Getenv("TOK_ULTRA_COMPACT") == "1" {
		f.ultra = true
	}
	if os.Getenv("TOK_NO_TRACK") == "1" {
		f.noTrack = true
	}
	return f, rest
}

// readStdin reads the whole hook payload. It returns "" when stdin is a terminal (no pipe),
// and is bounded by a timeout so `tok hook` can never hang if stdin is left open.
func readStdin() string {
	stat, err := os.Stdin.Stat()
	if err != nil || (stat.Mode()&os.ModeCharDevice) != 0 {
		return ""
	}
	ch := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(os.Stdin)
		ch <- string(b)
	}()
	select {
	case s := <-ch:
		// Strip a leading UTF-8 BOM: some Windows hosts (PowerShell) prepend one when
		// piping to a native command, which would otherwise break JSON parsing.
		return strings.TrimPrefix(s, "\ufeff")
	case <-time.After(2 * time.Second):
		return ""
	}
}

func getKwarg(args []string, name string) (string, bool) {
	flag := "--" + name
	for i, a := range args {
		if a == flag && i < len(args)-1 {
			return args[i+1], true
		}
	}
	prefix := flag + "="
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return a[len(prefix):], true
		}
	}
	return "", false
}

func hasFlag(args []string, name string) bool {
	flag := "--" + name
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func main() {
	run.CleanOldTeeFiles()

	flags, rest := parseGlobalFlags(os.Args[1:])

	if flags.showVersion {
		fmt.Printf("tok %s\n", constants.Version)
		os.Exit(0)
	}
	if flags.showHelp || len(rest) == 0 {
		fmt.Print(helpText())
		os.Exit(0)
	}

	// Hot path: the Node-free PreToolUse hook Claude Code fires on every Bash tool call.
	// Read the tool-call JSON on stdin, print the rewrite decision - no config, no DB.
	if rest[0] == "hook" {
		agent := ""
		if len(rest) > 1 {
			agent = rest[1]
		}
		payload := readStdin()
		// Cursor / Antigravity / Windsurf can't rewrite a command, so these are deny-and-retry
		// guards: block a recognized command and tell the agent to re-run it as tok.
		switch agent {
		case "cursor":
			fmt.Print(hook.BuildCursorHookOutput(payload))
			os.Exit(0)
		case "antigravity":
			fmt.Print(hook.BuildAntigravityHookOutput(payload))
			os.Exit(0)
		case "windsurf":
			if msg, block := hook.BuildWindsurfGuard(payload); block {
				fmt.Fprintln(os.Stderr, msg)
				os.Exit(2)
			}
			os.Exit(0)
		}
		// Claude Code: the transparent rewrite hook.
		out, ok := hook.BuildClaudeHookOutput(payload)
		hook.DebugLog(payload, ok) // no-op unless ~/.tok/hook-debug exists
		if payload != "" && ok {
			fmt.Print(out)
		}
		os.Exit(0)
	}

	// Hot path: the rewrite decision on its own. Exit codes are the protocol.
	if rest[0] == "rewrite" {
		outcome := registry.RewriteCommand(strings.Join(rest[1:], " "))
		switch outcome.Kind {
		case "allow":
			fmt.Print(outcome.Rewritten)
			os.Exit(constants.ExitAllow)
		case "ask":
			fmt.Print(outcome.Rewritten)
			os.Exit(constants.ExitAsk)
		case "deny":
			os.Exit(constants.ExitDeny)
		default:
			os.Exit(constants.ExitNoRewrite)
		}
	}

	cfg := config.Load()
	if cfg.Filters.UltraCompact {
		flags.ultra = true
	}
	db := store.Open()

	command := rest[0]
	cmdArgs := rest[1:]

	// Excluded commands run raw and untouched.
	for _, ex := range cfg.ExcludeCommands {
		if ex == command {
			r := run.Run(command, cmdArgs)
			if r.Stdout != "" {
				fmt.Print(r.Stdout)
			}
			if r.Stderr != "" {
				fmt.Fprint(os.Stderr, r.Stderr)
			}
			os.Exit(r.ExitCode)
		}
	}

	handler, isHandler, plain, plainExit := dispatch(db, cfg, flags, command, cmdArgs)

	if !isHandler {
		printLine(plain)
		os.Exit(plainExit)
	}

	// Output cache: swap an identical idempotent repeat for a compact marker.
	effective := handler.Filtered
	if !flags.noCache && !config.ShouldSkipCache() {
		cwd, _ := os.Getwd()
		effective = cache.Consult(db, cfg, handler.CmdType, cmdArgs, cwd, handler.Filtered, handler.Exit).Output
	}

	inBytes := len(handler.Raw)
	outBytes := len(effective)
	saved := inBytes - outBytes
	if saved < 0 {
		saved = 0
	}
	pct := 0.0
	if inBytes > 0 {
		pct = float64(saved) / float64(inBytes) * 100
	}

	final := run.MaybeTee(handler.CmdType, handler.Exit, effective, handler.Raw)
	printLine(final)

	if flags.verbose >= 1 {
		fmt.Fprintf(os.Stderr, "\n[tok] %s | %.0f%% saved (%d → %d bytes) in %dms\n", handler.CmdType, pct, inBytes, outBytes, handler.ExecMs)
	}
	if flags.verbose >= 2 {
		fmt.Fprintf(os.Stderr, "\n[tok raw output]\n%s\n", handler.Raw)
	}
	if flags.verbose >= 3 {
		fmt.Fprintf(os.Stderr, "\n[tok debug] tokens saved (est): %d\n", util.BytesToTokens(saved))
	}

	if !flags.noTrack && !config.ShouldSkipTracking() {
		db.AppendCommand(store.CommandRow{
			Timestamp:  util.NowIso(),
			CmdType:    handler.CmdType,
			InputBytes: inBytes,
			OutBytes:   outBytes,
			SavedBytes: saved,
			SavingsPct: pct,
			ExecMs:     handler.ExecMs,
		})
	}

	run.CheckHookVersion(db)
	os.Exit(handler.Exit)
}

// dispatch routes a command to its handler. It returns either a handler Result (isHandler
// true - the proxy path that gets cached, teed, and tracked) or a plain string plus exit
// code (analytics, maintenance, and info commands).
func dispatch(db *store.Store, cfg config.Config, flags globalFlags, command string, cmdArgs []string) (handlers.Result, bool, string, int) {
	u := flags.ultra
	h := func(r handlers.Result) (handlers.Result, bool, string, int) { return r, true, "", 0 }
	p := func(s string, code int) (handlers.Result, bool, string, int) {
		return handlers.Result{}, false, s, code
	}

	switch command {
	case "git":
		return h(handlers.HandleGit(cmdArgs, u))
	case "npm", "pnpm", "yarn":
		return h(handlers.HandleNode(command, cmdArgs, u))
	case "tsc":
		return h(handlers.HandleTsc(cmdArgs, u))
	case "jest", "vitest", "mocha":
		return h(handlers.HandleTestRunner(command, cmdArgs, u))
	case "eslint", "biome", "prettier":
		return h(handlers.HandleLint(command, cmdArgs, u))
	case "ls", "dir":
		return h(handlers.HandleLs(cmdArgs, u, cfg))
	case "cat", "read":
		return h(handlers.HandleCat(cmdArgs, u, cfg))
	case "smart":
		return h(handlers.HandleSmart(cmdArgs, u))
	case "grep", "rg":
		return h(handlers.HandleGrep(cmdArgs, u, cfg))
	case "find":
		return h(handlers.HandleFind(cmdArgs, u))
	case "diff":
		return h(handlers.HandleDiff(cmdArgs, u))
	case "json":
		return h(handlers.HandleJson(cmdArgs, u))
	case "docker":
		return h(handlers.HandleDocker(cmdArgs, u))
	case "kubectl":
		return h(handlers.HandleKubectl(cmdArgs, u))
	case "gh":
		return h(handlers.HandleGh(cmdArgs, u))
	case "pytest", "rspec", "rake", "playwright":
		return h(handlers.HandleMoreTests(command, cmdArgs, u))
	case "go":
		return h(handlers.HandleGo(cmdArgs, u))
	case "cargo":
		return h(handlers.HandleCargo(cmdArgs, u))
	case "ruff":
		return h(handlers.HandleRuff(cmdArgs, u))
	case "golangci-lint":
		return h(handlers.HandleGolangciLint(cmdArgs, u))
	case "rubocop":
		return h(handlers.HandleRubocop(cmdArgs, u))
	case "next":
		return h(handlers.HandleNext(cmdArgs, u))
	case "pip", "pip3":
		return h(handlers.HandlePip(cmdArgs, u))
	case "uv":
		return h(handlers.HandleUv(cmdArgs, u))
	case "bundle":
		return h(handlers.HandleBundle(cmdArgs, u))
	case "prisma":
		return h(handlers.HandlePrisma(cmdArgs, u))
	case "gem":
		return h(handlers.HandleGem(cmdArgs, u))
	case "pulumi":
		return h(handlers.HandlePulumi(cmdArgs, u))
	case "terraform":
		return h(handlers.HandleTerraform(cmdArgs, u))
	case "curl":
		return h(handlers.HandleCurl(cmdArgs, u))
	case "wget":
		return h(handlers.HandleWget(cmdArgs, u))
	case "env", "printenv":
		envArgs := cmdArgs
		if command == "printenv" {
			envArgs = append([]string{"__printenv__"}, cmdArgs...)
		}
		return h(handlers.HandleEnv(envArgs, u))

	case "err":
		if len(cmdArgs) == 0 {
			return p("usage: tok err <cmd> [args]", 2)
		}
		r := run.Run(cmdArgs[0], cmdArgs[1:])
		return h(handlers.Result{Filtered: r.Stderr, Exit: r.ExitCode, Raw: rawOf(r), CmdType: "err:" + cmdArgs[0], ExecMs: r.ExecMs})
	case "proxy":
		if len(cmdArgs) == 0 {
			return p("usage: tok proxy <cmd> [args]", 2)
		}
		r := run.Run(cmdArgs[0], cmdArgs[1:])
		raw := rawOf(r)
		return h(handlers.Result{Filtered: raw, Exit: r.ExitCode, Raw: raw, CmdType: "proxy:" + cmdArgs[0], ExecMs: r.ExecMs})
	case "summary":
		if len(cmdArgs) == 0 {
			return p("usage: tok summary <cmd> [args]", 2)
		}
		r := run.Run(cmdArgs[0], cmdArgs[1:])
		raw := rawOf(r)
		filtered := util.Truncate(util.StripAnsi(filter.DeduplicateLines(raw)), 30)
		return h(handlers.Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: "summary:" + cmdArgs[0], ExecMs: r.ExecMs})

	case "gain":
		fmtVal, _ := getKwarg(cmdArgs, "format")
		return p(analytics.RunGain(db, cfg, analytics.GainArgs{
			Graph: hasFlag(cmdArgs, "graph"), History: hasFlag(cmdArgs, "history"),
			Daily: hasFlag(cmdArgs, "daily"), Format: fmtVal,
		}), 0)
	case "stats":
		model, _ := getKwarg(cmdArgs, "model")
		return p(analytics.RunStats(db, cfg, analytics.StatsArgs{
			Model: model, Daily: hasFlag(cmdArgs, "daily"), Weekly: hasFlag(cmdArgs, "weekly"),
			Monthly: hasFlag(cmdArgs, "monthly"), Graph: hasFlag(cmdArgs, "graph"), Export: exportKind(cmdArgs),
		}), 0)
	case "econ":
		return p(analytics.RunEcon(db, cfg, analytics.EconArgs{
			Daily: hasFlag(cmdArgs, "daily"), Weekly: hasFlag(cmdArgs, "weekly"),
			Monthly: hasFlag(cmdArgs, "monthly"), Export: exportKind(cmdArgs),
		}), 0)
	case "session":
		return p(analytics.RunSession(db), 0)
	case "discover":
		return p(analytics.RunDiscover(db, cfg), 0)
	case "cache":
		return p(cache.RunCache(db, cfg, hasFlag(cmdArgs, "clear"), hasFlag(cmdArgs, "list")), 0)

	case "usage":
		return dispatchUsage(db, cfg, cmdArgs)

	case "doctor":
		return p(doctor.RunDoctor(db, cfg), 0)
	case "verify":
		return p(doctor.RunVerify(db), 0)
	case "hook-test":
		out, code := doctor.RunHookTest("")
		return p(out, code)
	case "init":
		return p(install.RunInit(db, install.InitOptions{
			Claude: hasFlag(cmdArgs, "claude"), Cursor: hasFlag(cmdArgs, "cursor"),
			Copilot: hasFlag(cmdArgs, "copilot"), Gemini: hasFlag(cmdArgs, "gemini"),
			Windsurf: hasFlag(cmdArgs, "windsurf"), Cline: hasFlag(cmdArgs, "cline"),
			Antigravity: hasFlag(cmdArgs, "antigravity"),
			Enforce:     hasFlag(cmdArgs, "enforce"),
			Uninstall:   hasFlag(cmdArgs, "uninstall"), Show: hasFlag(cmdArgs, "show"),
		}), 0)
	case "version":
		c := db.RowCounts()
		return p(strings.Join([]string{
			"tok " + constants.Version,
			fmt.Sprintf("Commands logged:    %d", c.Commands),
			fmt.Sprintf("AI usage records:   %d", c.AIUsage),
		}, "\n"), 0)

	default:
		// Unknown command: run it and apply a generic dedup + truncate filter.
		r := run.Run(command, cmdArgs)
		raw := rawOf(r)
		filtered := util.Truncate(util.StripAnsi(filter.DeduplicateLines(raw)), cfg.Filters.MaxOutputLines)
		if filtered == "" {
			filtered = raw
		}
		return h(handlers.Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: command, ExecMs: r.ExecMs})
	}
}

func dispatchUsage(db *store.Store, cfg config.Config, cmdArgs []string) (handlers.Result, bool, string, int) {
	p := func(s string, code int) (handlers.Result, bool, string, int) {
		return handlers.Result{}, false, s, code
	}
	sub := ""
	if len(cmdArgs) > 0 {
		sub = cmdArgs[0]
	}
	switch sub {
	case "ingest":
		source := ""
		if hasFlag(cmdArgs, "claude-code") {
			source = "claude-code"
		} else if hasFlag(cmdArgs, "ccusage") {
			source = "ccusage"
		}
		if source == "" {
			return p("usage: tok usage ingest --claude-code|--ccusage [--since YYYY-MM-DD]", 2)
		}
		since, _ := getKwarg(cmdArgs, "since")
		return p(usage.RunUsageIngest(db, cfg, usage.IngestArgs{Source: source, Since: since}), 0)
	case "log":
		model, _ := getKwarg(cmdArgs, "model")
		if model == "" {
			return p("usage: tok usage log --model NAME --input N --output N [--cache-write N] [--cache-read N] [--cost USD]", 2)
		}
		return p(usage.RunUsageLog(db, usage.ManualLogArgs{
			Model:      model,
			Input:      kwargInt(cmdArgs, "input"),
			Output:     kwargInt(cmdArgs, "output"),
			CacheWrite: kwargInt(cmdArgs, "cache-write"),
			CacheRead:  kwargInt(cmdArgs, "cache-read"),
			Cost:       kwargFloat(cmdArgs, "cost"),
		}), 0)
	case "models":
		return p(usage.RunUsageModels(db), 0)
	default:
		return p("usage: tok usage ingest|log|models", 2)
	}
}

func exportKind(args []string) string {
	if v, ok := getKwarg(args, "export"); ok && (v == "json" || v == "csv") {
		return v
	}
	return ""
}

func kwargInt(args []string, name string) int {
	if v, ok := getKwarg(args, name); ok {
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

func kwargFloat(args []string, name string) float64 {
	if v, ok := getKwarg(args, name); ok {
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}

func rawOf(r run.Result) string {
	if r.Stderr != "" {
		return r.Stdout + "\n" + r.Stderr
	}
	return r.Stdout
}

// printLine writes s to stdout, ensuring a single trailing newline, and prints nothing for
// an empty string.
func printLine(s string) {
	if s == "" {
		return
	}
	if strings.HasSuffix(s, "\n") {
		fmt.Print(s)
	} else {
		fmt.Println(s)
	}
}

func helpText() string {
	return "tok " + constants.Version + ` - CLI proxy that reduces LLM token consumption

USAGE
  tok <command> [args...]

GLOBAL FLAGS
  -u, --ultra-compact     Maximum compression (icons + single-line)
  -v, -vv, -vvv           Verbose output (filter info, raw, debug)
  --no-track              Skip the local savings write for this invocation
  --no-cache              Bypass the output cache; always emit full output
  --version               Print version and exit
  --help                  Print this help and exit

PROXY COMMANDS
  git <args>              Compressed git output
  npm | pnpm | yarn       Compressed package manager output
  pip | uv | bundle | gem | prisma  Package managers / codegen
  tsc                     Grouped TypeScript errors
  jest | vitest | mocha   Failure-focused JS test results
  pytest | rspec | rake test | playwright test  Other test runners
  go <test|build|vet>     Go toolchain
  cargo <test|build|clippy>  Rust toolchain
  eslint | biome | prettier  Grouped JS lint violations
  ruff | golangci-lint | rubocop | next build  Other linters/builds
  gh <pr|issue|run> ...   GitHub CLI, collapsed to counts
  docker | kubectl        Docker / Kubernetes
  pulumi | terraform      Infra plans → change summary
  curl | wget             HTTP fetch (large bodies compressed)
  env                     Variable names only (values redacted)
  ls | cat | grep | find | diff | json | smart  File commands
  err <cmd> [args]        Run command, return stderr only
  proxy <cmd> [args]      Run raw, no filter, track only
  summary <cmd> [args]    Run command, generic summary

ANALYTICS COMMANDS
  gain [--graph|--history|--daily] [--format json]
  stats [--model NAME] [--daily|--weekly|--monthly|--graph] [--export json|csv]
  econ [--daily|--weekly|--monthly] [--export json|csv]
  cache [--list|--clear]  Output-cache stats (unchanged-detection)
  session                 Session adoption %
  discover                Find unoptimized commands
  doctor                  Full self-diagnosis (env, PATH, hooks, DB, live probe)
  verify                  Hook installation + live probe report
  hook-test               Pipe fake payloads through the installed hook + assert protocol

INTERNAL (invoked by the AI tool's hook; not for direct use)
  hook claude             Read a PreToolUse payload on stdin, print the rewrite JSON
  hook cursor|antigravity|windsurf   deny-and-retry guard for that IDE (--enforce only)
  rewrite "<cmd>"         Print rewritten command, exit 0/1/2/3 per registry

USAGE INGESTION
  usage ingest --claude-code [--since YYYY-MM-DD]
  usage ingest --ccusage [--since YYYY-MM-DD]
  usage log --model NAME --input N --output N [--cache-write N] [--cache-read N] [--cost USD]
  usage models

MAINTENANCE
  init [--claude|--cursor|--copilot|--gemini|--windsurf|--cline|--antigravity]
  init [--cursor|--antigravity|--windsurf] --enforce   deny-and-retry guard (experimental)
  init --uninstall
  init --show
  version
`
}
