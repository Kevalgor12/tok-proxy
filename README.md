# tok

Save tokens when your AI coding assistant runs shell commands. tok compresses noisy command output (test runs, installs, builds, git, logs) into short summaries before it reaches the model, so you pay for less. Everything runs on your machine. No account, no server, no telemetry.

## Contents

- [What it is](#what-it-is)
- [Why use it](#why-use-it)
- [How it saves tokens](#how-it-saves-tokens)
- [Supported AI tools](#supported-ai-tools)
- [Supported operating systems](#supported-operating-systems)
- [Download and install](#download-and-install)
- [Quick start](#quick-start)
- [Commands](#commands)
- [Configuration](#configuration)
- [Privacy](#privacy)
- [Uninstall](#uninstall)

## What it is

AI coding assistants run shell commands for you and send the output back to the model so it can decide what to do next. That output is often long and repetitive, and every line of it costs tokens. tok sits between the assistant and your shell. It runs the real command, keeps the part that matters, and hands back a compact version.

Your own terminal is not affected. tok only touches commands the assistant runs inside its own shell.

## Why use it

- Long command output is mostly noise. A passing test suite, a clean install, or a full `git status` can be hundreds or thousands of tokens that add nothing useful.
- Tokens cost money and fill the context window. Shrinking command output lowers the bill and leaves more room for real work.
- It is fully local. tok never sends your commands or output anywhere.

## How it saves tokens

Example with `git status`:

```
Without tok
  assistant runs: git status
  full output (about 2,000 tokens) goes to the model

With tok
  assistant runs: tok git status
  tok runs the real git status on your machine
  tok returns: "3 modified, 1 untracked" (about 15 tokens)
```

tok always runs the real command and keeps its exit code. It only changes what the model sees.

Typical reductions:

| Command | What tok returns | Reduction |
|---|---|---|
| git status, diff, log | change counts and stats | 90 to 97 percent |
| jest, vitest, pytest | failures only, summary when green | 95 to 99 percent |
| npm, pnpm, yarn install | installed count and new packages | 85 to 98 percent |
| tsc, eslint, ruff | issues grouped by file and rule | 75 to 90 percent |
| ls, cat, grep, find | tree view, code aware, grouped matches | 50 to 95 percent |
| docker, kubectl logs | repeated lines removed | 70 to 99 percent |

tok also caches read-only commands. If the assistant runs the same `git status` or `cat` again and the result has not changed, tok returns a small "unchanged" marker instead of the whole output.

## Supported AI tools

tok works in two modes:

- **Automatic**: tok intercepts the command and compresses the output on its own.
- **Guided**: tok adds a usage rule that asks the assistant to run commands through tok. Savings depend on the assistant following the rule.

| Tool | Mode |
|---|---|
| Claude Code | Automatic |
| Antigravity | Automatic |
| Cursor | Guided (optional enforced mode) |
| Windsurf | Guided (optional enforced mode) |
| GitHub Copilot (VS Code) | Guided |
| Gemini CLI | Guided |
| Cline / Roo Code | Guided |

`tok init` detects the tools you have installed and sets up each one for you.

## Supported operating systems

| OS | Architectures |
|---|---|
| Linux | x64, arm64 |
| macOS | Intel (x64), Apple Silicon (arm64) |
| Windows | x64 |

## Download and install

tok is a single self-contained binary. No Node, no runtime, nothing else to install.

### One-line install (recommended)

Linux and macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/Kevalgor12/tok-proxy/main/scripts/install.sh | sh
```

Windows (PowerShell):

```powershell
iwr -useb https://raw.githubusercontent.com/Kevalgor12/tok-proxy/main/scripts/install.ps1 | iex
```

The installer downloads the right binary for your system, puts it in `~/.local/bin`, adds that folder to your PATH, and sets up the hooks. Open a new terminal afterward so the PATH change takes effect.

### Direct download

Prefer to grab the file yourself? Download the build for your system:

| OS | Download |
|---|---|
| Linux x64 | [tok-linux-x64](https://github.com/Kevalgor12/tok-proxy/releases/latest/download/tok-linux-x64) |
| Linux arm64 | [tok-linux-arm64](https://github.com/Kevalgor12/tok-proxy/releases/latest/download/tok-linux-arm64) |
| macOS Intel | [tok-macos-x64](https://github.com/Kevalgor12/tok-proxy/releases/latest/download/tok-macos-x64) |
| macOS Apple Silicon | [tok-macos-arm64](https://github.com/Kevalgor12/tok-proxy/releases/latest/download/tok-macos-arm64) |
| Windows x64 | [tok-windows-x64.exe](https://github.com/Kevalgor12/tok-proxy/releases/latest/download/tok-windows-x64.exe) |

Every version is listed on the [Releases page](https://github.com/Kevalgor12/tok-proxy/releases).

On Linux and macOS, rename it to `tok`, make it executable, move it onto your PATH, and set up the hooks:

```bash
mv tok-linux-x64 tok
chmod +x tok
mv tok ~/.local/bin/
tok init
```

On macOS the binary is unsigned, so the first run may be blocked. Clear the flag once:

```bash
xattr -dr com.apple.quarantine ~/.local/bin/tok
```

On Windows, rename the file to `tok.exe`, move it to a folder on your PATH (for example `%USERPROFILE%\.local\bin`), then run:

```powershell
tok init
```

### Build from source

Needs Go 1.23 or newer. tok uses only the Go standard library, so a checkout builds with just the Go toolchain.

```bash
git clone https://github.com/Kevalgor12/tok-proxy.git
cd tok-proxy
go build -o tok ./cmd/tok
./tok init
```

## Quick start

1. Install tok using any method above.
2. Restart your AI tool so it loads the hooks.
3. Confirm everything is wired up:

```bash
tok doctor
```

`tok doctor` checks the binary, your PATH, the data files, and each hook, and tells you what to fix if anything is off.

After a few sessions, see what you saved:

```bash
tok gain
```

## Commands

Run `tok --help` at any time for the complete list. tok reads the first word of the command and applies the matching compression.

### Proxy commands

| Command | What it does |
|---|---|
| `tok git <args>` | Compressed git output |
| `tok npm / pnpm / yarn <args>` | Compressed package manager output |
| `tok pip / uv / bundle / gem / prisma <args>` | Installs become counts, codegen becomes one line |
| `tok tsc <args>` | TypeScript errors grouped by file and code |
| `tok jest / vitest / mocha <args>` | Failures when red, summary when green |
| `tok pytest / rspec <args>` | Python and Ruby test results |
| `tok rake test`, `tok playwright test` | Minitest and Playwright results |
| `tok go <test, build, vet>` | Go tests and build output |
| `tok cargo <test, build, clippy>` | Rust tests and compiler output |
| `tok eslint / biome / prettier <args>` | Lint issues grouped by rule and file |
| `tok ruff / golangci-lint / rubocop <args>` | Diagnostics grouped by code |
| `tok next build` | Build output, route table dropped |
| `tok gh <pr, issue, run> ...` | GitHub CLI tables become counts |
| `tok ls [path]` | Tree view, noise folders hidden |
| `tok cat <file>` | Code-aware filtering |
| `tok grep <pattern> [path]` | Matches grouped by file |
| `tok find [args]` | Compact list |
| `tok diff <a> <b>` | Stats only |
| `tok json <file>` | Keys and types only |
| `tok smart <file>` | Short file summary |
| `tok docker <args>` | Deduplicated, error focused |
| `tok kubectl <args>` | Same as docker |
| `tok pulumi / terraform <args>` | Infra plans become a change summary |
| `tok curl / wget <args>` | Large bodies compressed |
| `tok env` | Variable names only, values hidden |
| `tok err <cmd> [args]` | Run a command, return only its errors |
| `tok proxy <cmd> [args]` | Run without filtering, record the run only |
| `tok summary <cmd> [args]` | Run any command, generic summary |

### Flags

| Flag | Effect |
|---|---|
| `-u`, `--ultra-compact` | Maximum compression |
| `-v`, `-vv`, `-vvv` | Verbose, very verbose, debug |
| `--no-track` | Do not record this run |
| `--no-cache` | Bypass the output cache |
| `--version` | Print version and exit |
| `--help` | Print help and exit |

### Reports

| Command | What it shows |
|---|---|
| `tok gain` | How many tokens tok saved |
| `tok gain --graph / --history / --daily` | Chart, history, and per-day views |
| `tok gain --format json` | Export as JSON |
| `tok stats` | AI token consumption |
| `tok stats --model NAME` | Filter by model name |
| `tok stats --daily / --weekly / --monthly / --graph` | Period views |
| `tok stats --export json / csv` | Export |
| `tok econ` | Cost, savings, and return on spend |
| `tok econ --daily / --weekly / --monthly` | Period breakdown |
| `tok cache` | Output cache stats |
| `tok cache --list / --clear` | List reused commands, or empty the cache |
| `tok session` | Adoption per conversation |
| `tok discover` | Commands that could be optimized |
| `tok doctor` | Full self-check |
| `tok verify` | Hook status per tool |

### Usage data

These feed `tok stats` and `tok econ`.

| Command | What it does |
|---|---|
| `tok usage ingest --claude-code [--since DATE]` | Read token usage from Claude Code logs |
| `tok usage ingest --ccusage [--since DATE]` | Read usage from the ccusage tool |
| `tok usage log --model NAME --input N --output N [...]` | Add one usage row by hand |
| `tok usage models` | List models seen in the local store |

### Setup and maintenance

| Command | What it does |
|---|---|
| `tok init` | Detect all tools and set up hooks |
| `tok init --claude / --cursor / --copilot / --gemini / --windsurf / --cline / --antigravity` | Set up one tool |
| `tok init --uninstall` | Remove all hooks |
| `tok init --show` | Show hook status |
| `tok version` | Version and local row counts |

## Configuration

tok keeps its data and settings in `~/.tok` (`C:\Users\<you>\.tok` on Windows). The config file `~/.tok/config.json` is created with sensible defaults on first run. You can adjust compression limits, token pricing, which read-only commands to cache, and which folders to treat as noise. A missing or invalid config falls back to defaults, so tok never breaks on a bad file.

Common environment variables:

| Variable | Effect |
|---|---|
| `TOK_HOME` | Data and config directory (default `~/.tok`) |
| `TOK_NO_TRACK=1` | Do not record this run |
| `TOK_NO_CACHE=1` | Bypass the output cache |
| `TOK_ULTRA_COMPACT=1` | Force maximum compression |
| `TOK_PRICE` | Override the token price used in reports |

## Privacy

tok runs entirely on your machine. It makes no network calls and has no account, server, or telemetry. All data stays in plain files under `~/.tok`.

tok records only:

- the command category (`git`, `npm`, `tsc`), never the full command
- byte and token counts, savings percentage, and run time
- AI model names and token counts, and only if you run `tok usage ingest`

tok never records command text or arguments, command output, file or folder names, usernames, or anything from your source code.

## Uninstall

You can remove tok completely from any OS. Remove the hooks first, then delete the data and the binary.

**1. Remove all hooks:**

```bash
tok init --uninstall
```

**2. Delete the local data and config.**

Linux and macOS:

```bash
rm -rf ~/.tok
```

Windows (PowerShell):

```powershell
Remove-Item -Recurse -Force "$HOME\.tok"
```

**3. Delete the binary.**

Linux and macOS:

```bash
rm ~/.local/bin/tok
```

Windows (PowerShell):

```powershell
Remove-Item "$HOME\.local\bin\tok.exe"
```

If you added `~/.local/bin` to your PATH only for tok, you can remove that entry as well. On Windows the PATH is under System Properties, Environment Variables.
