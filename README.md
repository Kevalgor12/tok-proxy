# tok

A CLI proxy that reduces LLM token consumption by 60–99% on common developer commands. tok sits transparently between your AI coding tool and your shell, compresses verbose command output into compact summaries, and saves the difference off your token bill. **100% local — nothing ever leaves your machine.**

---

## What tok does

When an AI coding tool (Claude Code, Cursor, Copilot, Gemini CLI, Windsurf, Cline) runs a shell command internally, a `PreToolUse` hook rewrites it to the tok equivalent before execution. tok runs the real command on your local machine, applies one of its filtering strategies, and returns a compact result back to the AI. The model sees a tiny payload; you pay for the tiny payload; your CI/CD pipelines never see a difference.

```
WITHOUT tok:
  AI tool runs: git status
  Raw output (2,000 tokens) → sent to AI model → you pay for 2,000 tokens

WITH tok:
  AI tool tries to run: git status
  PreToolUse hook fires → rewrites to: tok git status (AI never sees this)
  tok runs real git status on your machine
  tok compresses output → "3 modified, 1 untracked" (15 tokens)
  AI model receives 15 tokens → you pay for 15 tokens
  tok logs the savings to local files — no network, no telemetry
```

Your real terminal is completely untouched. tok only intercepts commands run inside the AI tool's internal bash executor.

---

## How it works — 9 filtering strategies

| # | Strategy | Used by | Reduction |
|---|---|---|---|
| 1 | Stats extraction | `git status`, `git diff`, `git log` | 92–97% |
| 2 | Error only | `tok err <cmd>` | 60–100% |
| 3 | Grouping by pattern | `eslint`, `tsc`, `grep` | 75–90% |
| 4 | Deduplication | `docker logs`, generic fallback | 70–99% |
| 5 | Structure only | `tok json <file>` | 80–99% |
| 6 | Code filtering | `tok cat <file>` | 0–90% |
| 7 | Failure focus | `jest`, `vitest`, `mocha` | 94–99.5% |
| 8 | Tree compression | `tok ls` | 50–95% |
| 9 | Progress filtering | `npm install`, `pnpm install` | 85–98% |

**Examples:**

| Command | Standard | Ultra (`-u`) |
|---|---|---|
| `git status` | `3 modified, 1 untracked` | `3M 1U` |
| `tsc` | `4 errors in 2 files` (grouped) | `4E/2F` |
| `jest` | `2 failed, 98 passed (100 total)` | `✗2/100` |
| `npm install` | `✓ Installed 234 packages` | `✓234pkg` |
| `ls` | Tree view, noise dirs hidden | `src/32 tests/15 docs/3` |

---

## Advanced features

Two capabilities go beyond one-shot output compression.

### Output cache — unchanged-detection

AI agents re-run the same idempotent reads constantly: `git status`, `ls`, `cat <file>`, `grep`. The first time tok runs one of these it filters and returns the compact output as usual and remembers a hash of the underlying command output, keyed by **command + working directory + arguments**. On the next identical run, if the underlying output is byte-for-byte unchanged, tok returns a ~15-token marker instead of the whole payload:

```
◇ unchanged 3× (cat, 12s ago) — ~1381 tok saved; already in context. --no-cache to force.
```

- The real command **always executes** — exit codes and side effects are never skipped. tok only shrinks what the model sees, and only for the read-only commands on the `cache.commands` allowlist.
- Identity is decided on the *filtered* output — exactly what the model would see — so the "unchanged" claim is always literally true. Volatile derived values (a relative timestamp in `git log`) only ever cause a harmless cache *miss*, never a false "unchanged".
- The marker is only served when it is actually smaller than the filtered output, so already-tiny results (`3 modified`) are never made larger.
- Bypass per-call with `--no-cache` / `TOK_NO_CACHE=1`, or globally with `"cache": { "enabled": false }`.
- Inspect it with `tok cache` / `tok cache --list`; empty it with `tok cache --clear`.

On a repeated `cat` of a 5 KB source file this is a ~1,400-token saving — on top of the initial filter.

### `tok doctor` — self-diagnosis

`tok doctor` runs an end-to-end health check and tells you exactly what to fix:

- **Runtime** — Node and Bash availability (hooks need both).
- **PATH** — resolves every `tok` on PATH through symlinks/shims and warns only on genuinely distinct binaries that would shadow each other.
- **Data store** — opens the local JSON/NDJSON files and reports row counts.
- **Config** — validates the JSON, falls back to defaults on error.
- **Hooks** — for each AI tool: script present, version current, registered in settings, and a **live probe** that pipes a fake `Bash` tool-call through the installed hook and asserts it rewrites to a `tok` command — the same path the AI tool takes.

```
  OK    Claude Code live probe
        sent {command:"git status"} → hook rewrote to "tok git status"
```

---

## Token savings table

| Command type | Typical reduction | Scenario |
|---|---|---|
| `tok git status` | 95% | clean repo or small change set |
| `tok git diff` | 97% | feature branch with many file changes |
| `tok git log` | 90% | last 20 commits |
| `tok tsc` | 85% | grouped errors instead of full TS output |
| `tok jest` / `tok vitest` | 99% on green, 95% on red | failure focus |
| `tok eslint` | 80% | grouped by rule + top files |
| `tok npm install` | 95% | installed count + new deps |
| `tok docker logs` | 90% | deduplicated repeated lines |
| `tok ls` | 60–95% | noise dirs skipped, deep dirs collapsed |

---

## Installation

> Replace `OWNER/REPO` below with the GitHub repo you host tok in.

### Option 1 — standalone binary (recommended · no Node, no repo)

Download a single self-contained executable — nothing else required. The binary bundles its own runtime, so it works like a native tool (à la a Go/Rust binary).

**macOS / Linux**
```
curl -fsSL https://raw.githubusercontent.com/OWNER/REPO/main/scripts/install.sh | TOK_REPO=OWNER/REPO sh
```

**Windows (PowerShell)**
```
$env:TOK_REPO='OWNER/REPO'; iwr -useb https://raw.githubusercontent.com/OWNER/REPO/main/scripts/install.ps1 | iex
```

The installer downloads the right binary to `~/.local/bin`, adds it to your PATH, and runs `tok init` to wire up hooks. Or grab the file for your platform straight from the [Releases](https://github.com/OWNER/REPO/releases) page — `tok-windows-x64.exe`, `tok-macos-arm64`, `tok-macos-x64`, `tok-linux-x64`, `tok-linux-arm64` — put it on your PATH as `tok`, and run `tok init`.

The Claude Code hook is a single `tok hook claude` command, so **the binary alone is enough — no Node, npm, or repo checkout on the end user's machine.**

### Option 2 — from source (Node ≥ 16)

For development or customization. No native dependencies and no compiler step — storage is plain JSON/NDJSON files — so `npm install` can't fail on a build toolchain.

```
git clone https://github.com/OWNER/REPO tok
cd tok
npm install          # builds dist/ and runs `tok init` automatically
npm link             # optional: a global `tok` command
```

Re-run setup any time with `npm run setup`; skip it during install with `TOK_SKIP_SETUP=1` (also skipped automatically in CI).

### Building the binaries yourself

```
npm run build:binaries          # → build/tok-windows-x64.exe, tok-linux-x64, tok-macos-x64, …
```

pkg cross-compiles the **x64** targets from any machine. The two **arm64** builds (`tok-linux-arm64`, `tok-macos-arm64`) must run on a native arm host. The included **`.github/workflows/release.yml`** handles this: push a `v*` tag and it builds all five on native runners and attaches them to the GitHub Release — the installers then download them by name.

After any install method, **restart your AI tool**, then run `tok doctor` to confirm every hook is wired up.

---

## Supported AI tools

| Tool | Hook type | Mechanism |
|---|---|---|
| **Claude Code** | Transparent | `PreToolUse` hook runs `tok hook claude` (no script/node) |
| **Cursor** | Transparent | `~/.cursor/hooks.json` `preToolUse` entry |
| **VS Code Copilot** | Transparent | `settings.json` `tokWrap` entry |
| **Gemini CLI** | Transparent | `~/.gemini/settings.json` `BeforeTool` |
| **Windsurf** | Instruction | `.windsurfrules` block |
| **Cline / Roo Code** | Instruction | `.clinerules` block |

`tok init` with no flags auto-detects every installed tool and installs the right hook for each.

---

## Command reference

### Proxy commands

| Command | Behaviour |
|---|---|
| `tok git <args>` | Compressed git output |
| `tok npm`/`pnpm`/`yarn <args>` | Compressed package manager output |
| `tok pip`/`uv`/`bundle`/`gem`/`prisma <args>` | Installs → counts; codegen → one line |
| `tok tsc <args>` | TypeScript errors grouped by file + code |
| `tok jest`/`vitest`/`mocha <args>` | Failures only on red, summary on green |
| `tok pytest`/`rspec <args>` | Python / Ruby test failures + summary |
| `tok rake test` / `tok playwright test` | Minitest / Playwright failures + summary |
| `tok go <test\|build\|vet>` | Go tests + build diagnostics |
| `tok cargo <test\|build\|clippy>` | Rust tests + compiler diagnostics |
| `tok eslint`/`biome`/`prettier <args>` | Violations grouped by rule + file |
| `tok ruff`/`golangci-lint`/`rubocop <args>` | Diagnostics grouped by code |
| `tok next build` | Build diagnostics, route table dropped |
| `tok gh <pr\|issue\|run> ...` | GitHub CLI tables → counts + identifiers |
| `tok ls [path]` | Tree view, noise dirs hidden |
| `tok cat <file>` | Code-aware filtering (comments, bodies) |
| `tok grep <pattern> [path]` | Matches grouped by file |
| `tok find [args]` | Compact list with overflow indicator |
| `tok diff <a> <b>` | Stats only |
| `tok json <file>` | JSON keys + types only |
| `tok smart <file>` | Two-line file summary |
| `tok docker <args>` | Deduplicated, error-focused |
| `tok kubectl <args>` | Same as docker |
| `tok pulumi`/`terraform <args>` | Infra plans → change summary (+/~/-) |
| `tok curl`/`wget <args>` | Large bodies compressed (JSON→structure) |
| `tok env` | Variable **names** only — values redacted |
| `tok err <cmd> [args]` | Run command, return stderr only |
| `tok proxy <cmd> [args]` | Run raw, no filter, track only |
| `tok summary <cmd> [args]` | Run command, generic dedup + summary |

### Global flags

| Flag | Behaviour |
|---|---|
| `-u`, `--ultra-compact` | Maximum compression |
| `-v`, `-vv`, `-vvv` | Verbose / very verbose / debug |
| `--no-track` | Skip the local write for this invocation |
| `--no-cache` | Bypass the output cache; always emit full output |
| `--version` | Print version and exit |
| `--help` | Print help and exit |

### Analytics

| Command | Behaviour |
|---|---|
| `tok gain` | Filter savings summary |
| `tok gain --graph` / `--history` / `--daily` | Variants |
| `tok gain --format json` | Export as JSON |
| `tok stats` | AI consumption summary |
| `tok stats --model NAME` | Filter by model name (partial match) |
| `tok stats --daily` / `--weekly` / `--monthly` / `--graph` | Variants |
| `tok stats --export json` / `--export csv` | Exports |
| `tok econ` | Combined: cost + savings + weighted CPT + ROI |
| `tok econ --daily` / `--weekly` / `--monthly` | Per-period breakdown |
| `tok econ --export json` / `--export csv` | Exports |
| `tok cache` | Output-cache stats (unchanged-detection) |
| `tok cache --list` / `--clear` | List most-reused commands / empty the cache |
| `tok session` | Adoption % per AI conversation session |
| `tok discover` | Find unoptimized commands + savings potential |
| `tok doctor` | Full self-diagnosis (env, PATH, hooks, DB, live probe) |
| `tok verify` | Hook installation status per AI tool |

### Usage ingestion

| Command | Behaviour |
|---|---|
| `tok usage ingest --claude-code [--since YYYY-MM-DD]` | Parse Claude Code JSONL logs |
| `tok usage ingest --ccusage [--since YYYY-MM-DD]` | Use `ccusage` CLI (binary, then `npx` fallback) |
| `tok usage log --model NAME --input N --output N [--cache-write N] [--cache-read N] [--cost USD]` | Manual entry |
| `tok usage models` | List all models seen in the local store |

### Setup & maintenance

| Command | Behaviour |
|---|---|
| `tok init` | Auto-detect all AI tools, install hooks |
| `tok init --claude` / `--cursor` / `--copilot` / `--gemini` / `--windsurf` / `--cline` | Install for one specific tool |
| `tok init --uninstall` | Remove all hooks cleanly |
| `tok init --show` | Print hook status |
| `npm run setup` | Rebuild + reinstall hooks (run after `git pull`) |
| `tok version` | Version + local row counts |

---

## Privacy

**tok is fully local. It makes no network calls and sends no telemetry — there is no server, account, or device id.** Everything lives in plain files under **`~/.tok/`** (`C:\Users\<you>\.tok` on Windows): `commands.ndjson`, `ai_usage.ndjson`, `meta.json`, `cache.json`, and `config.json`.

> **Why `~/.tok` and not `%APPDATA%`?** Store/MSIX-packaged apps like Claude Desktop run sandboxed, and Windows silently redirects `%APPDATA%`/`%LOCALAPPDATA%` into the app's private folder. Storing under your profile root keeps one shared directory for both the hook (run by Claude) and your terminal — otherwise `tok gain` would look empty even while tok is saving. Override the location with `TOK_HOME` if needed.

What tok stores locally:

- `cmd_type` — category only (`git`, `npm`, `tsc`), never the full command
- `input_bytes`, `out_bytes`, `saved_bytes`, `savings_pct`, `exec_ms`
- AI model names (`claude-opus-4-5`, etc.) and token counts, when you run `tok usage ingest`
- `cost_usd` from ccusage when available

What tok **never** stores:

- Full command strings or arguments
- Command output or filtered output content
- File names, directory names, project names
- Usernames, email addresses, real names
- IP addresses, hostnames, machine names
- Source code, secrets, API keys, environment variables

The output cache stores filtered outputs (to detect unchanged repeats), also locally; clear it any time with `tok cache --clear`.

---

## Configuration

Config file:

- All platforms: `~/.tok/config.json` (`C:\Users\<you>\.tok\config.json` on Windows)

The file is created with defaults on first run. Missing or malformed configs are merged silently with defaults — tok never crashes on a bad config.

```json
{
  "version": "0.3.0",
  "tokenPricePer1k": 0.015,
  "tee": { "enabled": true, "mode": "failures" },
  "filters": {
    "maxOutputLines": 150,
    "ultraCompact": false,
    "git":  { "diffMaxLines": 100 },
    "cat":  { "maxLines": 200, "defaultLevel": "minimal" },
    "grep": { "maxMatches": 100 },
    "ls":   { "maxDepth": 4 }
  },
  "cache": {
    "enabled": true,
    "maxEntries": 5000,
    "maxOutputBytes": 65536,
    "commands": ["git status", "git diff", "git log", "ls", "cat", "grep", "find", "json", "docker ps"]
  },
  "excludeCommands": ["ssh", "vim", "nano", "less", "psql", "mysql"],
  "noiseDirectories": [
    "node_modules", ".git", "dist", "build", ".next", "target",
    "__pycache__", ".cache", "coverage", ".turbo", "vendor",
    ".svn", ".hg", "out", "tmp", ".tmp"
  ],
  "claudeCodeDataDir": "~/.claude/projects",
  "modelPricing": {
    "claude-opus-4-5":   { "inputPer1k": 0.015,   "outputPer1k": 0.075,   "cacheWritePer1k": 0.01875,  "cacheReadPer1k": 0.0015 },
    "claude-sonnet-4-5": { "inputPer1k": 0.003,   "outputPer1k": 0.015,   "cacheWritePer1k": 0.00375,  "cacheReadPer1k": 0.0003 },
    "claude-haiku-4-5":  { "inputPer1k": 0.00025, "outputPer1k": 0.00125, "cacheWritePer1k": 0.0003,   "cacheReadPer1k": 0.00003 }
  }
}
```

### Environment variable overrides

| Variable | Effect |
|---|---|
| `TOK_PRICE` | Override `tokenPricePer1k` |
| `TOK_NO_TRACK=1` | Skip the local write for this invocation |
| `TOK_NO_CACHE=1` | Bypass the output cache |
| `TOK_ULTRA_COMPACT=1` | Force ultra-compact mode |
| `TOK_SKIP_SETUP=1` | Skip the auto build+init during `npm install` |

---

## Analytics

### `tok gain` — filter savings

Reads the local command log (`commands.ndjson`). Works offline. Shows total bytes/tokens saved over time, top filtered commands, and any commands that aren't being optimized.

### `tok stats` — AI token consumption

Reads `tok_ai_usage`. Shows input/output/cache tokens by period, models used, cache hit rate, and an estimated cache savings figure.

### `tok econ` — combined economics

Calculates a weighted input cost-per-token (CPT) by reverse-engineering Anthropic's pricing ratios:

```
weightedUnits = input + 5×output + 1.25×cacheWrite + 0.1×cacheRead
inputCpt      = totalCost / weightedUnits
savedUsd      = savedTokens × inputCpt
roi           = savedUsd / totalCost × 100
```

When `cost_usd = 0` for the period (no ccusage data ingested), tok falls back to the configured flat rate and marks the figure `(estimated — run tok usage ingest --ccusage for actuals)`.

`tok econ` also shows: context window health (avg session size, sessions over 100K), and a model-cost comparison estimating what the same workload would cost on Sonnet vs Haiku.

---

## AI usage ingestion

tok needs raw AI usage data to compute `tok stats` and `tok econ`. There are three sources:

1. **Claude Code logs** — `tok usage ingest --claude-code`
   Walks `~/.claude/projects/<hash>/*.jsonl` and parses every assistant message that includes a `usage` block.

2. **ccusage CLI** — `tok usage ingest --ccusage`
   Calls `ccusage --json --since <date>` (falls back to `npx --yes ccusage`). Each day × model becomes one usage row including the `cost_usd` field.

3. **Manual log** — `tok usage log --model NAME --input N --output N [...]`
   Insert one row directly. Useful for non-Claude models or quick experiments.

---

## Troubleshooting

**`tok verify` shows hooks installed but no activity in 7 days**
Restart the AI tool — hooks are loaded at startup. If still no activity, run `tok init --show` and confirm the hook file path matches what the AI tool reads.

**`tok stats` shows "No AI usage data yet"**
Run `tok usage ingest --claude-code` (or `--ccusage`). The PostToolUse hook only captures new activity — historical data has to be backfilled once.

**Builds in CI suddenly fail**
tok always preserves the real exit code. If a CI step started failing right after install, run with `-vv` (or set `TOK_NO_TRACK=1` to bypass) and check the raw output. File a bug if the filter dropped meaningful error context — tok's TEE system should write the full output to `~/.tok/tee/...` whenever a failure has a short filtered output.

**Something misbehaving?**
Run `tok doctor` — it checks Node/Bash, PATH collisions, the data store, config, and the hook logic. Detailed errors are logged to `~/.tok/errors.log`.

**I want to remove tok entirely**
```
tok init --uninstall            # or: node dist/main.js init --uninstall
npm rm -g tok-proxy             # only if you ran `npm link` / `npm install -g`
rm -rf ~/.tok                   # all local data + config
```

On Windows that's `rmdir /s "%USERPROFILE%\.tok"`. Then delete the binary (`~/.local/bin/tok.exe`) or the cloned folder.
