import { run } from '../core/runner';
import { stripAnsi } from '../core/utils';
import { HandlerResult } from './git';

// Non-Node package managers and codegen: pip, uv, bundle, prisma, gem.
// Installs collapse to "N installed" + any errors; listings to counts.

export function handlePip(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const bin = 'pip';
  const result = run(bin, args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    if (sub === 'install') filtered = summarizePipInstall(raw, ultra);
    else if (sub === 'list' || sub === 'freeze') filtered = summarizeList(raw, ultra, 'packages');
    else filtered = stripAnsi(raw).trim(); // pip show/uninstall/…: full output
  } catch {
    filtered = raw;
  }
  return result0(filtered, result, `pip ${sub || 'cmd'}`, raw);
}

export function handleUv(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const result = run('uv', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    if (sub === 'sync' || sub === 'pip' || sub === 'add' || sub === 'install') {
      filtered = summarizeUvSync(raw, ultra, result.exitCode);
    } else {
      // uv run <cmd> and everything else: pass the wrapped output through in full.
      filtered = stripAnsi(raw).trim();
    }
  } catch {
    filtered = raw;
  }
  return result0(filtered, result, `uv ${sub || 'cmd'}`, raw);
}

export function handleBundle(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || 'install';
  const result = run('bundle', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    const m = /Bundle complete!\s+(\d+)\s+Gemfile dependencies,\s+(\d+)\s+gems now installed/.exec(stripAnsi(raw));
    if (m) filtered = ultra ? `✓${m[2]}gems` : `✓ Bundle complete: ${m[2]} gems (${m[1]} deps)`;
    else if (result.exitCode !== 0) filtered = errorTail(raw);
    else filtered = stripAnsi(raw).trim(); // e.g. `bundle exec <cmd>`: pass wrapped output through
  } catch {
    filtered = raw;
  }
  return result0(filtered, result, `bundle ${sub}`, raw);
}

export function handlePrisma(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const result = run('prisma', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    const clean = stripAnsi(raw);
    const gen = /Generated Prisma Client\s+\(([^)]+)\)/.exec(clean);
    const migrate = /(Your database is now in sync|already in sync|migration.+applied)/i.exec(clean);
    if (gen) filtered = ultra ? '✓gen' : `✓ Generated Prisma Client (${gen[1]})`;
    else if (migrate) filtered = ultra ? '✓migrate' : `✓ ${migrate[1]}`;
    else if (result.exitCode !== 0) filtered = errorTail(raw);
    else filtered = stripAnsi(raw).trim(); // prisma db pull/studio/…: full output
  } catch {
    filtered = raw;
  }
  return result0(filtered, result, `prisma ${sub || 'cmd'}`, raw);
}

export function handleGem(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const result = run('gem', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    const m = /(\d+)\s+gems? installed/.exec(stripAnsi(raw));
    if (m) filtered = ultra ? `✓${m[1]}gems` : `✓ ${m[1]} gems installed`;
    else if (sub === 'list') filtered = summarizeList(raw, ultra, 'gems');
    else if (result.exitCode !== 0) filtered = errorTail(raw);
    else filtered = stripAnsi(raw).trim(); // other gem subcommands: full output
  } catch {
    filtered = raw;
  }
  return result0(filtered, result, `gem ${sub || 'cmd'}`, raw);
}

function summarizePipInstall(raw: string, ultra: boolean): string {
  const clean = stripAnsi(raw);
  const errors = clean.split('\n').filter((l) => /^ERROR:/.test(l));
  if (errors.length > 0) return ultra ? `✗${errors.length}err` : `✗ Install failed\n${errors.slice(0, 5).join('\n')}`;
  const m = /Successfully installed\s+(.+)/.exec(clean);
  if (m) {
    const pkgs = m[1].trim().split(/\s+/);
    if (ultra) return `✓${pkgs.length}pkg`;
    return `✓ Installed ${pkgs.length} package${pkgs.length === 1 ? '' : 's'}: ${pkgs.slice(0, 10).join(', ')}`;
  }
  if (/already satisfied/i.test(clean)) return ultra ? '✓cached' : '✓ requirements already satisfied';
  return ultra ? '✓' : '✓ ok';
}

function summarizeUvSync(raw: string, ultra: boolean, exitCode: number): string {
  const clean = stripAnsi(raw);
  const installed = (clean.match(/^\s*[+]\s+\S+/gm) || []).length;
  const removed = (clean.match(/^\s*[-]\s+\S+/gm) || []).length;
  if (exitCode !== 0) return errorTail(raw);
  if (installed === 0 && removed === 0) return ultra ? '✓' : '✓ up-to-date';
  if (ultra) return `✓+${installed}-${removed}`;
  return `✓ synced: +${installed} / -${removed} packages`;
}

function summarizeList(raw: string, ultra: boolean, noun: string): string {
  const lines = stripAnsi(raw).split('\n').filter((l) => l.trim() && !/^-+\s|^Package\s/i.test(l));
  if (ultra) return `${lines.length} ${noun}`;
  return `${lines.length} ${noun} installed\n${lines.slice(0, 25).join('\n')}`;
}

function errorTail(raw: string): string {
  const lines = stripAnsi(raw).split('\n').filter((l) => l.trim());
  const errs = lines.filter((l) => /error|failed|cannot|not found/i.test(l));
  return (errs.length ? errs : lines.slice(-8)).slice(0, 8).join('\n');
}

function result0(filtered: string, result: { exitCode: number; execMs: number }, cmdType: string, raw: string): HandlerResult {
  return {
    filteredOutput: filtered || (result.exitCode === 0 ? 'ok' : raw),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType,
    execMs: result.execMs,
  };
}
