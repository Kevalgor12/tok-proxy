# tok: Node to Go migration

tok is being rewritten from Node/TypeScript to Go. Same behavior, same CLI, same hook
protocol - a single native binary instead of a bundled Node runtime.

## Why

- **Size:** the Node build was ~55 MB (bundled runtime). A Go binary is ~5-8 MB.
- **No runtime:** Go compiles to a static binary. No Node, no pkg, no `~/.local` PATH games.
- **Cross-compile:** `GOOS`/`GOARCH` builds all five platform binaries from one machine, so
  the release drops pkg, the scarce Intel-mac runner, and the arm64 cross-build limits.
- **Zero dependencies:** the whole tool fits the standard library. `go.mod` has no requires.

## Layout

Organized by domain package (idiomatic Go), not by layer. `cmd/` wires; `internal/` works.

```
cmd/tok/main.go          entry point + command dispatch (wiring only)
internal/
  constants/             version, exit-code protocol
  config/                typed config: defaults, load/merge, env overrides
  store/                 local NDJSON/JSON store (savings, AI usage, cache, meta)
  run/                   execute real commands, capture output, tee failures
  filter/                compression primitives (dedup, strip-ansi, truncate, code)
  registry/              command-rewrite rules + the exit-code protocol
  hook/                  the Node-free `tok hook claude` PreToolUse hook
  handlers/              per-command output compressors (git, docker, gh, files, ...)
  analytics/             gain, stats, econ, session, discover
  usage/                 AI-usage ingestion (Claude Code logs, ccusage)
  install/               init: detect AI tools, wire hooks, awareness files
  doctor/                doctor, verify, hook-test
  util/                  generic helpers: numbers, dates, tokens, paths, files
```

No external frameworks. Stdlib `flag` + `os/exec` + `encoding/json` cover a CLI of this
shape; cobra would add a dependency and binary weight for a flat `tok <cmd> [args]`
surface that doesn't need it.

## Phases

Each phase is one commit and builds green on its own. Bottom-up, so later phases import
earlier ones.

| # | Phase | Packages |
|---|---|---|
| 0 | Scaffold | `go.mod`, `cmd/tok`, `internal/constants`, this doc |
| 1 | Foundation | `internal/util`, `internal/config` |
| 2 | Store | `internal/store` |
| 3 | Runner + filters | `internal/run`, `internal/filter` |
| 4 | Registry + hook | `internal/registry`, `internal/hook` |
| 5 | Core handlers | `internal/handlers`: git, files, node, tsc |
| 6 | More handlers | docker, gh, infra, http, lang, build, pkg, tests, lint |
| 7 | Analytics | `internal/analytics`, `internal/usage` |
| 8 | Install + diagnostics | `internal/install`, `internal/doctor` |
| 9 | CLI wiring | full dispatch + help + global flags in `cmd/tok` |
| 10 | Release + cleanup | Go cross-compile workflow, installers, README; remove Node source |

The Node source stays in place until phase 10 so nothing breaks mid-migration; the last
phase deletes `src/`, `package.json`, `dist/`, and the pkg tooling.
