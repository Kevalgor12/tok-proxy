import * as os from 'os';
import * as fs from 'fs';
import * as path from 'path';
import { spawnSync } from 'child_process';
import { DB, rowCounts, cacheStats, dbPath } from '../core/local-db';
import { TokConfig, configPath } from '../core/config';
import { TOK_VERSION, fileExistsSync, readFileIfExists, dataDir } from '../core/utils';
import { readRegisteredClaudeCommand, probeClaudeHook } from '../core/hook';

type Level = 'ok' | 'warn' | 'fail';

interface Check {
  level: Level;
  label: string;
  detail: string;
  fix?: string;
}

// `tok doctor` — end-to-end health check. Goes beyond `verify`/`hook-test` by also
// checking the runtime environment, PATH collisions (multiple `tok` binaries), the
// local database and the config file, and by running a live probe THROUGH the
// installed hook exactly as the AI tool would.
export function runDoctor(db: DB, config: TokConfig): string {
  const checks: Check[] = [];

  checks.push(checkNode());
  checks.push(checkBash());
  checks.push(...checkTokOnPath());
  checks.push(checkDatabase(db));
  checks.push(checkConfig(config));
  checks.push(checkCache(db, config));
  checks.push(...checkClaudeHook(db));
  checks.push(checkCursorHook());

  const fails = checks.filter((c) => c.level === 'fail').length;
  const warns = checks.filter((c) => c.level === 'warn').length;

  const lines: string[] = [];
  lines.push(`tok doctor — v${TOK_VERSION}`);
  lines.push('══════════════════════════════════════════════════════════');
  for (const c of checks) {
    const tag = c.level === 'ok' ? 'OK  ' : c.level === 'warn' ? 'WARN' : 'FAIL';
    lines.push(`  ${tag}  ${c.label}`);
    if (c.detail) lines.push(`        ${c.detail}`);
    if (c.fix && c.level !== 'ok') lines.push(`        → fix: ${c.fix}`);
  }
  lines.push('');
  if (fails === 0 && warns === 0) {
    lines.push('All checks passed. tok is wired up and healthy.');
  } else {
    lines.push(`${fails} failing, ${warns} warning${warns === 1 ? '' : 's'}. Address the items above.`);
  }
  return lines.join('\n');
}

function checkNode(): Check {
  const major = parseInt(process.versions.node.split('.')[0], 10);
  if (major >= 16) {
    return { level: 'ok', label: 'Node runtime', detail: `node ${process.version} (hook JSON parsing works)` };
  }
  return {
    level: 'warn',
    label: 'Node runtime',
    detail: `node ${process.version} is old`,
    fix: 'upgrade to Node 16+ — the hook uses node for JSON parsing',
  };
}

function checkBash(): Check {
  const r = spawnSync('bash', ['--version'], { encoding: 'utf8' });
  if (!r.error && r.status === 0) {
    const v = /version\s+([\d.]+)/.exec(r.stdout || '')?.[1] || '?';
    return { level: 'ok', label: 'Bash shell', detail: `bash ${v} available` };
  }
  // Not on THIS shell's PATH (e.g. running doctor from cmd.exe). The `tok hook claude`
  // hook is a plain command — bash isn't required — but Claude Code on Windows uses
  // Git Bash, so look for it in the usual spots before raising anything.
  if (process.platform === 'win32') {
    const gitBash = [
      'C:/Program Files/Git/bin/bash.exe',
      'C:/Program Files/Git/usr/bin/bash.exe',
      'C:/Program Files (x86)/Git/bin/bash.exe',
      path.join(os.homedir(), 'AppData', 'Local', 'Programs', 'Git', 'bin', 'bash.exe'),
    ].find(fileExistsSync);
    if (gitBash) {
      return {
        level: 'ok',
        label: 'Bash shell',
        detail: `Git Bash at ${gitBash} (not on this shell's PATH, but Claude Code finds it)`,
      };
    }
  }
  return {
    level: 'warn',
    label: 'Bash shell',
    detail: 'bash not found — the `tok hook claude` hook usually runs without it, but Claude Code on Windows may want Git Bash',
    fix: 'install Git for Windows if hooks don\'t fire after a restart',
  };
}

function checkTokOnPath(): Check[] {
  const isWin = process.platform === 'win32';
  const r = spawnSync(isWin ? 'where' : 'which', isWin ? ['tok'] : ['-a', 'tok'], {
    encoding: 'utf8',
    shell: isWin,
  });
  if (r.status !== 0 || !(r.stdout || '').trim()) {
    return [{
      level: 'ok',
      label: 'tok on PATH',
      detail: 'not on PATH — fine, hooks call tok by its full path. `npm link` adds a global `tok`.',
    }];
  }
  const found = (r.stdout || '').split('\n').map((l) => l.trim()).filter(Boolean);
  const out: Check[] = [];
  out.push({
    level: 'ok',
    label: 'tok on PATH',
    detail: found[0],
  });
  // Collapse the shims that npm/nvm legitimately create for one install: the
  // `tok`/`tok.cmd`/`tok.ps1` trio, and junction/symlink duplicates that resolve
  // to the same real file. Only warn when genuinely distinct binaries remain.
  const distinct = new Set(
    found.map((f) => {
      let real = f;
      try { real = fs.realpathSync(f); } catch { /* keep original */ }
      return real.replace(/\.(cmd|exe|ps1|bat)$/i, '').toLowerCase();
    }),
  );
  if (distinct.size > 1) {
    out.push({
      level: 'warn',
      label: 'PATH collision',
      detail: `${distinct.size} distinct "tok" binaries on PATH — the first one wins:\n        ${Array.from(distinct).join('\n        ')}`,
      fix: 'remove the shadowing binaries so the intended tok is invoked',
    });
  }
  return out;
}

function checkDatabase(db: DB): Check {
  try {
    const counts = rowCounts(db);
    return {
      level: 'ok',
      label: 'Local data store',
      detail: `${dbPath()} — ${counts.commands} commands, ${counts.aiUsage} usage rows (JSON/NDJSON, zero native deps)`,
    };
  } catch (err) {
    return {
      level: 'fail',
      label: 'Local data store',
      detail: `cannot read ${dbPath()}: ${(err as Error).message}`,
      fix: `remove the data dir to reset: rm -rf "${dataDir()}"`,
    };
  }
}

function checkConfig(config: TokConfig): Check {
  const p = configPath();
  if (!fileExistsSync(p)) {
    return { level: 'ok', label: 'Config', detail: 'using built-in defaults (no config file yet — created on first run)' };
  }
  const raw = readFileIfExists(p);
  try {
    JSON.parse(raw || '{}');
    return { level: 'ok', label: 'Config', detail: `${p} (valid, ${config.excludeCommands.length} excluded commands)` };
  } catch {
    return {
      level: 'warn',
      label: 'Config',
      detail: `${p} is not valid JSON — defaults are being used instead`,
      fix: 'fix the JSON syntax or delete the file to regenerate defaults',
    };
  }
}

function checkCache(db: DB, config: TokConfig): Check {
  if (!config.cache.enabled) {
    return { level: 'ok', label: 'Output cache', detail: 'disabled in config' };
  }
  const s = cacheStats(db);
  return {
    level: 'ok',
    label: 'Output cache',
    detail: `enabled — ${s.entries} entries, ${s.hits} hits served as markers`,
  };
}

function checkClaudeHook(_db: DB): Check[] {
  const claudeHome = path.join(os.homedir(), '.claude');
  if (!fileExistsSync(claudeHome)) {
    return [{ level: 'ok', label: 'Claude Code', detail: 'not detected on this system (skipped)' }];
  }

  const registered = readRegisteredClaudeCommand();
  const out: Check[] = [];
  out.push({
    level: registered ? 'ok' : 'fail',
    label: 'Claude Code hook',
    detail: registered
      ? `registered in settings.json: \`${registered}\``
      : 'Claude Code is present but the tok hook is NOT registered (it will never fire)',
    fix: registered ? undefined : 'tok init --claude',
  });

  if (registered) {
    // Live probe: run the exact registered command with a fake Bash tool-call.
    const probe = probeClaudeHook(registered);
    out.push({
      level: probe.pass ? 'ok' : 'fail',
      label: 'Claude Code hook logic',
      detail: probe.pass
        ? `rewrites "git status" → "${probe.rewrite}"`
        : `hook did not produce a valid rewrite: ${probe.reason}`,
      fix: probe.pass ? undefined : 'tok hook-test  for details',
    });
  }
  return out;
}

function checkCursorHook(): Check {
  const cursorHook = path.join(os.homedir(), '.cursor', 'hooks', 'tok-rewrite.sh');
  if (!fileExistsSync(path.join(os.homedir(), '.cursor'))) {
    return { level: 'ok', label: 'Cursor', detail: 'not detected on this system (skipped)' };
  }
  if (!fileExistsSync(cursorHook)) {
    return { level: 'warn', label: 'Cursor hook', detail: 'Cursor present but hook not installed', fix: 'tok init --cursor' };
  }
  return { level: 'ok', label: 'Cursor hook', detail: '~/.cursor/hooks/tok-rewrite.sh installed' };
}
