import { spawnSync } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import { dataDir, ensureDir, nowIso, TOK_VERSION, appendErrorLog } from './utils';
import { DB, getMeta, setMeta } from './local-db';

export interface RunResult {
  stdout: string;
  stderr: string;
  exitCode: number;
  execMs: number;
}

export function run(cmd: string, args: string[]): RunResult {
  const start = Date.now();
  const isWin = process.platform === 'win32';
  let result;
  try {
    if (isWin) {
      const commandLine = [cmd, ...args.map(quoteWinArg)].join(' ');
      result = spawnSync(commandLine, [], {
        encoding: 'utf8',
        shell: true,
        maxBuffer: 50 * 1024 * 1024,
      });
    } else {
      result = spawnSync(cmd, args, {
        encoding: 'utf8',
        shell: false,
        maxBuffer: 50 * 1024 * 1024,
      });
    }
  } catch (err) {
    appendErrorLog('runner.spawn', err);
    return { stdout: '', stderr: String(err), exitCode: 1, execMs: Date.now() - start };
  }
  const execMs = Date.now() - start;
  const stdout = (result.stdout || '').toString();
  const stderr = (result.stderr || '').toString();
  let exitCode = typeof result.status === 'number' ? result.status : 1;
  if (result.error) {
    appendErrorLog('runner.error', result.error);
    if (exitCode === 0) exitCode = 1;
  }
  return { stdout, stderr, exitCode, execMs };
}

function quoteWinArg(arg: string): string {
  if (arg === '') return '""';
  if (!/[\s"&<>|^()]/.test(arg)) return arg;
  return `"${arg.replace(/"/g, '""')}"`;
}

export function teeDir(): string {
  return path.join(dataDir(), 'tee');
}

export function maybeTee(
  cmdType: string,
  exitCode: number,
  filteredOutput: string,
  rawCombined: string,
): string {
  try {
    if (exitCode === 0) return filteredOutput;
    if (filteredOutput.length >= 500) return filteredOutput;

    ensureDir(teeDir());
    const ts = Math.floor(Date.now() / 1000);
    const filename = `${ts}_${cmdType}.log`;
    const teePath = path.join(teeDir(), filename);
    fs.writeFileSync(teePath, rawCombined);
    return `${filteredOutput}\n[Full output: ${teePath}]`;
  } catch (err) {
    appendErrorLog('maybeTee', err);
    return filteredOutput;
  }
}

export function cleanOldTeeFiles(): void {
  try {
    const dir = teeDir();
    if (!fs.existsSync(dir)) return;
    const cutoff = Date.now() - 24 * 3600 * 1000;
    const entries = fs.readdirSync(dir);
    for (const name of entries) {
      try {
        const full = path.join(dir, name);
        const stat = fs.statSync(full);
        if (stat.mtimeMs < cutoff) {
          fs.unlinkSync(full);
        }
      } catch {
        // ignore
      }
    }
  } catch {
    // ignore
  }
}

export function checkHookVersion(db: DB): void {
  try {
    const last = getMeta(db, 'last_hook_check');
    if (last) {
      const ms = Date.now() - new Date(last).getTime();
      if (ms < 24 * 3600 * 1000) return;
    }
    setMeta(db, 'last_hook_check', nowIso());

    const hookV = getMeta(db, 'hook_version');
    if (!hookV) return;
    if (hookV !== TOK_VERSION) {
      process.stderr.write(`⚠ Hooks are outdated (v${hookV} → v${TOK_VERSION}). Run: tok init\n`);
    }
  } catch (err) {
    appendErrorLog('checkHookVersion', err);
  }
}

