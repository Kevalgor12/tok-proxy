#!/usr/bin/env node
import { handleRuff, handleGolangciLint, handleRubocop, handleNext } from './cmds/build';
import { runCache } from './cmds/cache';
import { runDiscover } from './cmds/discover';
import { handleDocker, handleKubectl } from './cmds/docker';
import { runDoctor } from './cmds/doctor';
import { runEcon } from './cmds/econ';
import { handleLs, handleCat, handleSmart, handleGrep, handleFind, handleDiff, handleJson } from './cmds/files';
import { runGain } from './cmds/gain';
import { handleGh } from './cmds/gh';
import { handleGit, HandlerResult } from './cmds/git';
import { runHookTest } from './cmds/hook-test';
import { handleCurl, handleWget, handleEnv } from './cmds/http';
import { handlePulumi, handleTerraform } from './cmds/infra';
import { runInit } from './cmds/init';
import { handleGo, handleCargo } from './cmds/lang';
import { handleLint } from './cmds/lint';
import { handleNode } from './cmds/node';
import { handlePip, handleUv, handleBundle, handlePrisma, handleGem } from './cmds/pkg';
import { runSession } from './cmds/session';
import { runStats } from './cmds/stats';
import { handleTestRunner } from './cmds/test-runners';
import { handleMoreTests } from './cmds/tests-more';
import { handleTsc } from './cmds/typescript';
import { runUsageIngest, runUsageLog, runUsageModels } from './cmds/usage';
import { runVerify } from './cmds/verify';
import { consultCache } from './core/cache';
import { loadConfig, shouldSkipTracking, shouldSkipCache } from './core/config';
import { deduplicateLines } from './core/filter';
import { buildClaudeHookOutput } from './core/hook';
import { openDb, recordCommand, rowCounts } from './core/local-db';
import { rewriteCommand } from './core/registry';
import { run, cleanOldTeeFiles, maybeTee, checkHookVersion } from './core/runner';
import { TOK_VERSION, nowIso, appendErrorLog, estimateTokens, stripAnsi, truncate } from './core/utils';

interface GlobalFlags {
  ultra: boolean;
  verbose: number;
  noTrack: boolean;
  noCache: boolean;
  showHelp: boolean;
  showVersion: boolean;
}

function parseGlobalFlags(argv: string[]): { flags: GlobalFlags; rest: string[] } {
  const flags: GlobalFlags = {
    ultra: false,
    verbose: 0,
    noTrack: false,
    noCache: false,
    showHelp: false,
    showVersion: false,
  };
  const rest: string[] = [];
  for (const arg of argv) {
    if (arg === '-u' || arg === '--ultra-compact') flags.ultra = true;
    else if (arg === '-v') flags.verbose = Math.max(flags.verbose, 1);
    else if (arg === '-vv') flags.verbose = Math.max(flags.verbose, 2);
    else if (arg === '-vvv') flags.verbose = Math.max(flags.verbose, 3);
    else if (arg === '--no-track') flags.noTrack = true;
    else if (arg === '--no-cache') flags.noCache = true;
    else if (arg === '--version') flags.showVersion = true;
    else if (arg === '--help' || arg === '-h') flags.showHelp = true;
    else rest.push(arg);
  }
  if (process.env.TOK_ULTRA_COMPACT === '1') flags.ultra = true;
  if (process.env.TOK_NO_TRACK === '1') flags.noTrack = true;
  return { flags, rest };
}

// Read all of stdin (the hook payload). Resolves to '' when there's no pipe, and is
// guarded by a timeout so `tok hook` can never hang if stdin is left open.
function readStdin(): Promise<string> {
  return new Promise((resolve) => {
    if (process.stdin.isTTY) {
      resolve('');
      return;
    }
    let data = '';
    process.stdin.setEncoding('utf8');
    process.stdin.on('data', (chunk) => { data += chunk; });
    process.stdin.on('end', () => resolve(data));
    process.stdin.on('error', () => resolve(data));
    setTimeout(() => resolve(data), 2000).unref();
  });
}

function getKwarg(args: string[], name: string): string | undefined {
  const idx = args.findIndex((a) => a === `--${name}`);
  if (idx >= 0 && idx < args.length - 1) return args[idx + 1];
  const prefix = `--${name}=`;
  const found = args.find((a) => a.startsWith(prefix));
  if (found) return found.slice(prefix.length);
  return undefined;
}

function hasFlag(args: string[], name: string): boolean {
  return args.includes(`--${name}`);
}

function helpText(): string {
  return `tok ${TOK_VERSION} - CLI proxy that reduces LLM token consumption

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
  rewrite "<cmd>"         Print rewritten command, exit 0/1/2/3 per registry

USAGE INGESTION
  usage ingest --claude-code [--since YYYY-MM-DD]
  usage ingest --ccusage [--since YYYY-MM-DD]
  usage log --model NAME --input N --output N [--cache-write N] [--cache-read N] [--cost USD]
  usage models

MAINTENANCE
  init [--claude|--cursor|--copilot|--gemini|--windsurf|--cline]
  init --uninstall
  init --show
  version
`;
}

async function main(): Promise<void> {
  cleanOldTeeFiles();

  const argv = process.argv.slice(2);
  const { flags, rest } = parseGlobalFlags(argv);

  if (flags.showVersion) {
    process.stdout.write(`tok ${TOK_VERSION}\n`);
    process.exit(0);
  }
  if (flags.showHelp || rest.length === 0) {
    process.stdout.write(helpText());
    process.exit(rest.length === 0 ? 0 : 0);
  }

  // Hot path: `tok hook <agent>` is the Node-free PreToolUse hook Claude Code fires
  // on every Bash tool call. It reads the tool-call JSON on stdin and prints the
  // rewrite decision on stdout - no shell, no node, no jq - so a standalone binary
  // works with zero runtime prerequisites. Skip config + DB to keep latency minimal.
  if (rest[0] === 'hook') {
    const payload = await readStdin();
    if (payload) {
      const out = buildClaudeHookOutput(payload);
      if (out) process.stdout.write(out);
    }
    process.exit(0);
  }

  // Hot path: `tok rewrite "<cmd>"` - the rewrite decision by itself (used by the
  // legacy shell-script hook and by tests). Exit codes are the protocol - see core/registry.ts.
  if (rest[0] === 'rewrite') {
    const inputCmd = rest.slice(1).join(' ');
    const outcome = rewriteCommand(inputCmd);
    switch (outcome.kind) {
      case 'allow':
        process.stdout.write(outcome.rewritten);
        process.exit(0);
      case 'ask':
        process.stdout.write(outcome.rewritten);
        process.exit(3);
      case 'deny':
        process.exit(2);
      case 'none':
        process.exit(1);
    }
  }

  const config = loadConfig();
  if (config.filters.ultraCompact) flags.ultra = true;

  let db;
  try {
    db = openDb();
  } catch (err) {
    appendErrorLog('main.openDb', err);
    process.stderr.write(`tok: failed to open local database: ${(err as Error).message}\n`);
    process.exit(1);
  }

  const command = rest[0];
  const cmdArgs = rest.slice(1);

  if (config.excludeCommands.includes(command)) {
    const result = run(command, cmdArgs);
    if (result.stdout) process.stdout.write(result.stdout);
    if (result.stderr) process.stderr.write(result.stderr);
    process.exit(result.exitCode);
  }

  let result: HandlerResult | { plain: string; exitCode: number } | null = null;

  try {
    if (command === 'git') {
      result = handleGit(cmdArgs, flags.ultra);
    } else if (command === 'npm' || command === 'pnpm' || command === 'yarn') {
      result = handleNode(command, cmdArgs, flags.ultra);
    } else if (command === 'tsc') {
      result = handleTsc(cmdArgs, flags.ultra);
    } else if (command === 'jest' || command === 'vitest' || command === 'mocha') {
      result = handleTestRunner(command, cmdArgs, flags.ultra);
    } else if (command === 'eslint' || command === 'biome' || command === 'prettier') {
      result = handleLint(command, cmdArgs, flags.ultra);
    } else if (command === 'ls' || command === 'dir') {
      result = handleLs(cmdArgs, flags.ultra, config);
    } else if (command === 'cat' || command === 'read') {
      result = handleCat(cmdArgs, flags.ultra, config);
    } else if (command === 'smart') {
      result = handleSmart(cmdArgs, flags.ultra);
    } else if (command === 'grep' || command === 'rg') {
      result = handleGrep(cmdArgs, flags.ultra, config);
    } else if (command === 'find') {
      result = handleFind(cmdArgs, flags.ultra);
    } else if (command === 'diff') {
      result = handleDiff(cmdArgs, flags.ultra);
    } else if (command === 'json') {
      result = handleJson(cmdArgs, flags.ultra);
    } else if (command === 'docker') {
      result = handleDocker(cmdArgs, flags.ultra);
    } else if (command === 'kubectl') {
      result = handleKubectl(cmdArgs, flags.ultra);
    } else if (command === 'gh') {
      result = handleGh(cmdArgs, flags.ultra);
    } else if (command === 'pytest' || command === 'rspec' || command === 'rake' || command === 'playwright') {
      result = handleMoreTests(command, cmdArgs, flags.ultra);
    } else if (command === 'go') {
      result = handleGo(cmdArgs, flags.ultra);
    } else if (command === 'cargo') {
      result = handleCargo(cmdArgs, flags.ultra);
    } else if (command === 'ruff') {
      result = handleRuff(cmdArgs, flags.ultra);
    } else if (command === 'golangci-lint') {
      result = handleGolangciLint(cmdArgs, flags.ultra);
    } else if (command === 'rubocop') {
      result = handleRubocop(cmdArgs, flags.ultra);
    } else if (command === 'next') {
      result = handleNext(cmdArgs, flags.ultra);
    } else if (command === 'pip' || command === 'pip3') {
      result = handlePip(cmdArgs, flags.ultra);
    } else if (command === 'uv') {
      result = handleUv(cmdArgs, flags.ultra);
    } else if (command === 'bundle') {
      result = handleBundle(cmdArgs, flags.ultra);
    } else if (command === 'prisma') {
      result = handlePrisma(cmdArgs, flags.ultra);
    } else if (command === 'gem') {
      result = handleGem(cmdArgs, flags.ultra);
    } else if (command === 'pulumi') {
      result = handlePulumi(cmdArgs, flags.ultra);
    } else if (command === 'terraform') {
      result = handleTerraform(cmdArgs, flags.ultra);
    } else if (command === 'curl') {
      result = handleCurl(cmdArgs, flags.ultra);
    } else if (command === 'wget') {
      result = handleWget(cmdArgs, flags.ultra);
    } else if (command === 'env' || command === 'printenv') {
      result = handleEnv(command === 'printenv' ? ['__printenv__', ...cmdArgs] : cmdArgs, flags.ultra);
    } else if (command === 'err') {
      const subCmd = cmdArgs[0];
      if (!subCmd) {
        result = { plain: 'usage: tok err <cmd> [args]', exitCode: 2 };
      } else {
        const r = run(subCmd, cmdArgs.slice(1));
        result = {
          filteredOutput: r.stderr || '',
          rawOutput: r.stdout + (r.stderr ? `\n${r.stderr}` : ''),
          exitCode: r.exitCode,
          cmdType: `err:${subCmd}`,
          execMs: r.execMs,
        };
      }
    } else if (command === 'proxy') {
      const subCmd = cmdArgs[0];
      if (!subCmd) {
        result = { plain: 'usage: tok proxy <cmd> [args]', exitCode: 2 };
      } else {
        const r = run(subCmd, cmdArgs.slice(1));
        result = {
          filteredOutput: r.stdout + (r.stderr ? `\n${r.stderr}` : ''),
          rawOutput: r.stdout + (r.stderr ? `\n${r.stderr}` : ''),
          exitCode: r.exitCode,
          cmdType: `proxy:${subCmd}`,
          execMs: r.execMs,
        };
      }
    } else if (command === 'summary') {
      const subCmd = cmdArgs[0];
      if (!subCmd) {
        result = { plain: 'usage: tok summary <cmd> [args]', exitCode: 2 };
      } else {
        const r = run(subCmd, cmdArgs.slice(1));
        const raw = r.stdout + (r.stderr ? `\n${r.stderr}` : '');
        const dedup = deduplicateLines(raw);
        const filtered = truncate(stripAnsi(dedup), 30);
        result = {
          filteredOutput: filtered,
          rawOutput: raw,
          exitCode: r.exitCode,
          cmdType: `summary:${subCmd}`,
          execMs: r.execMs,
        };
      }
    } else if (command === 'gain') {
      const out = runGain(db, config, {
        graph: hasFlag(cmdArgs, 'graph'),
        history: hasFlag(cmdArgs, 'history'),
        daily: hasFlag(cmdArgs, 'daily'),
        format: getKwarg(cmdArgs, 'format'),
      });
      result = { plain: out, exitCode: 0 };
    } else if (command === 'stats') {
      const exp = getKwarg(cmdArgs, 'export');
      const out = runStats(db, config, {
        model: getKwarg(cmdArgs, 'model'),
        daily: hasFlag(cmdArgs, 'daily'),
        weekly: hasFlag(cmdArgs, 'weekly'),
        monthly: hasFlag(cmdArgs, 'monthly'),
        graph: hasFlag(cmdArgs, 'graph'),
        export: exp === 'json' || exp === 'csv' ? exp : undefined,
      });
      result = { plain: out, exitCode: 0 };
    } else if (command === 'econ') {
      const exp = getKwarg(cmdArgs, 'export');
      const out = runEcon(db, config, {
        daily: hasFlag(cmdArgs, 'daily'),
        weekly: hasFlag(cmdArgs, 'weekly'),
        monthly: hasFlag(cmdArgs, 'monthly'),
        export: exp === 'json' || exp === 'csv' ? exp : undefined,
      });
      result = { plain: out, exitCode: 0 };
    } else if (command === 'usage') {
      const sub = cmdArgs[0];
      if (sub === 'ingest') {
        let source: 'claude-code' | 'ccusage' | null = null;
        if (hasFlag(cmdArgs, 'claude-code')) source = 'claude-code';
        else if (hasFlag(cmdArgs, 'ccusage')) source = 'ccusage';
        if (!source) {
          result = { plain: 'usage: tok usage ingest --claude-code|--ccusage [--since YYYY-MM-DD]', exitCode: 2 };
        } else {
          const since = getKwarg(cmdArgs, 'since');
          const out = runUsageIngest(db, config, { source, since });
          result = { plain: out, exitCode: 0 };
        }
      } else if (sub === 'log') {
        const model = getKwarg(cmdArgs, 'model');
        const input = parseInt(getKwarg(cmdArgs, 'input') || '0', 10);
        const output = parseInt(getKwarg(cmdArgs, 'output') || '0', 10);
        const cw = getKwarg(cmdArgs, 'cache-write');
        const cr = getKwarg(cmdArgs, 'cache-read');
        const cost = getKwarg(cmdArgs, 'cost');
        if (!model) {
          result = { plain: 'usage: tok usage log --model NAME --input N --output N [--cache-write N] [--cache-read N] [--cost USD]', exitCode: 2 };
        } else {
          const out = runUsageLog(db, {
            model,
            input,
            output,
            cacheWrite: cw ? parseInt(cw, 10) : undefined,
            cacheRead: cr ? parseInt(cr, 10) : undefined,
            cost: cost ? parseFloat(cost) : undefined,
          });
          result = { plain: out, exitCode: 0 };
        }
      } else if (sub === 'models') {
        result = { plain: runUsageModels(db), exitCode: 0 };
      } else {
        result = { plain: 'usage: tok usage ingest|log|models', exitCode: 2 };
      }
    } else if (command === 'session') {
      result = { plain: runSession(db), exitCode: 0 };
    } else if (command === 'discover') {
      result = { plain: runDiscover(db, config), exitCode: 0 };
    } else if (command === 'verify') {
      result = { plain: runVerify(db), exitCode: 0 };
    } else if (command === 'doctor') {
      result = { plain: runDoctor(db, config), exitCode: 0 };
    } else if (command === 'cache') {
      result = {
        plain: runCache(db, config, { clear: hasFlag(cmdArgs, 'clear'), list: hasFlag(cmdArgs, 'list') }),
        exitCode: 0,
      };
    } else if (command === 'hook-test') {
      const r = runHookTest({});
      result = { plain: r.output, exitCode: r.exitCode };
    } else if (command === 'init') {
      const out = runInit(db, {
        claude: hasFlag(cmdArgs, 'claude'),
        cursor: hasFlag(cmdArgs, 'cursor'),
        copilot: hasFlag(cmdArgs, 'copilot'),
        gemini: hasFlag(cmdArgs, 'gemini'),
        windsurf: hasFlag(cmdArgs, 'windsurf'),
        cline: hasFlag(cmdArgs, 'cline'),
        uninstall: hasFlag(cmdArgs, 'uninstall'),
        show: hasFlag(cmdArgs, 'show'),
      });
      result = { plain: out, exitCode: 0 };
    } else if (command === 'version') {
      const counts = rowCounts(db);
      const lines = [
        `tok ${TOK_VERSION}`,
        `Commands logged:    ${counts.commands}`,
        `AI usage records:   ${counts.aiUsage}`,
      ];
      result = { plain: lines.join('\n'), exitCode: 0 };
    } else {
      // Unknown command - generic filter passthrough
      const r = run(command, cmdArgs);
      const raw = r.stdout + (r.stderr ? `\n${r.stderr}` : '');
      let filtered = '';
      try {
        const dedup = deduplicateLines(raw);
        filtered = truncate(stripAnsi(dedup), config.filters.maxOutputLines);
      } catch {
        filtered = raw;
      }
      result = {
        filteredOutput: filtered || raw,
        rawOutput: raw,
        exitCode: r.exitCode,
        cmdType: command,
        execMs: r.execMs,
      };
    }
  } catch (err) {
    appendErrorLog('main.handler', err);
    // Fail-safe: do not crash. Run command raw.
    const r = run(command, cmdArgs);
    const raw = r.stdout + (r.stderr ? `\n${r.stderr}` : '');
    if (raw) process.stdout.write(raw);
    process.exit(r.exitCode);
  }

  if (result === null) {
    process.exit(1);
  }

  if ('plain' in result) {
    if (result.plain) process.stdout.write(result.plain.endsWith('\n') ? result.plain : `${result.plain}\n`);
    process.exit(result.exitCode);
  }

  const handler = result;

  // Output cache: if this idempotent read produced byte-identical output to a prior
  // run, swap the full payload for a tiny "unchanged" marker. The real command has
  // already executed - we only shrink what the model sees.
  let effectiveFiltered = handler.filteredOutput;
  if (!flags.noCache && !shouldSkipCache()) {
    try {
      const decision = consultCache(
        db, config, handler.cmdType, cmdArgs, process.cwd(),
        handler.filteredOutput, handler.exitCode,
      );
      effectiveFiltered = decision.output;
    } catch (err) {
      appendErrorLog('main.cache', err);
    }
  }

  const inBytes = Buffer.byteLength(handler.rawOutput, 'utf8');
  const outBytes = Buffer.byteLength(effectiveFiltered, 'utf8');
  const saved = Math.max(0, inBytes - outBytes);
  const pct = inBytes > 0 ? (saved / inBytes) * 100 : 0;

  // Apply tee
  const finalOutput = maybeTee(handler.cmdType, handler.exitCode, effectiveFiltered, handler.rawOutput);
  if (finalOutput) process.stdout.write(finalOutput.endsWith('\n') ? finalOutput : `${finalOutput}\n`);

  // Verbose info
  if (flags.verbose >= 1) {
    process.stderr.write(`\n[tok] ${handler.cmdType} | ${pct.toFixed(0)}% saved (${inBytes} → ${outBytes} bytes) in ${handler.execMs}ms\n`);
  }
  if (flags.verbose >= 2) {
    process.stderr.write(`\n[tok raw output]\n${handler.rawOutput}\n`);
  }
  if (flags.verbose >= 3) {
    process.stderr.write(`\n[tok debug] tokens saved (est): ${estimateTokens(' '.repeat(saved))}\n`);
  }

  // Record savings locally (everything tok does is local-only - no network).
  if (!flags.noTrack && !shouldSkipTracking()) {
    recordCommand(db, {
      timestamp: nowIso(),
      cmd_type: handler.cmdType,
      input_bytes: inBytes,
      out_bytes: outBytes,
      saved_bytes: saved,
      savings_pct: pct,
      exec_ms: handler.execMs,
    });
  }

  // Cheap, synchronous: warn once a day if the installed hooks are out of date.
  checkHookVersion(db);

  process.exit(handler.exitCode);
}

main().catch((err) => {
  appendErrorLog('main.unhandled', err);
  process.stderr.write(`tok: unexpected error: ${(err as Error).message}\n`);
  process.exit(1);
});
