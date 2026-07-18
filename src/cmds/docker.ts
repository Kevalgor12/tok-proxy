import { deduplicateLines } from '../core/filter';
import { run } from '../core/runner';
import { stripAnsi } from '../core/utils';
import { HandlerResult } from './git';

export function handleDocker(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const result = run('docker', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  let cmdType = `docker ${sub || 'cmd'}`;

  try {
    if (sub === 'ps') filtered = formatPs(raw, ultra);
    else if (sub === 'images') filtered = formatImages(raw, ultra);
    else if (sub === 'logs') filtered = formatLogs(raw, ultra);
    else if (sub === 'compose') filtered = formatCompose(raw, ultra);
    else filtered = stripAnsi(raw).trim();
  } catch {
    filtered = raw;
  }

  return {
    filteredOutput: filtered || (result.exitCode === 0 ? 'ok' : raw),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType,
    execMs: result.execMs,
  };
}

export function handleKubectl(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const result = run('kubectl', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';

  try {
    if (sub === 'logs') {
      filtered = formatLogs(raw, ultra);
    } else if (sub === 'get') {
      filtered = formatKubectlGet(raw, ultra);
    } else {
      filtered = stripAnsi(raw).trim();
    }
  } catch {
    filtered = raw;
  }

  return {
    filteredOutput: filtered || (result.exitCode === 0 ? 'ok' : raw),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: `kubectl ${sub || 'cmd'}`,
    execMs: result.execMs,
  };
}

function formatPs(raw: string, ultra: boolean): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n').filter((l) => l.trim());
  const data = lines.slice(1);
  const running = data.filter((l) => /\bUp\b/.test(l)).length;
  const stopped = data.length - running;
  if (ultra) return `${running}↑/${stopped}↓`;
  if (data.length === 0) return 'no containers';
  const out = [`${data.length} container${data.length === 1 ? '' : 's'}: ${running} running, ${stopped} stopped`];
  for (const line of data.slice(0, 10)) {
    const tokens = line.split(/\s{2,}/);
    if (tokens.length >= 2) {
      const id = tokens[0].slice(0, 12);
      const image = tokens[1];
      const status = tokens.find((t) => /\b(Up|Exited|Created)\b/.test(t)) || '';
      out.push(`  ${id} ${image} ${status}`);
    }
  }
  return out.join('\n');
}

function formatImages(raw: string, ultra: boolean): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n').filter((l) => l.trim());
  const data = lines.slice(1);
  if (ultra) return `${data.length} images`;
  if (data.length === 0) return 'no images';
  const out = [`${data.length} image${data.length === 1 ? '' : 's'}:`];
  for (const line of data.slice(0, 15)) {
    const tokens = line.split(/\s{2,}/);
    if (tokens.length >= 2) {
      out.push(`  ${tokens[0]}:${tokens[1] || 'latest'}`);
    }
  }
  return out.join('\n');
}

function formatLogs(raw: string, ultra: boolean): string {
  const dedup = deduplicateLines(raw);
  const lines = dedup.split('\n').filter((l) => l.trim());
  const errors = lines.filter((l) => /\b(error|err|failed|exception|panic)\b/i.test(l));
  if (ultra) {
    const top = lines.slice(0, 3).map((l) => l.slice(0, 60)).join(' | ');
    return `${lines.length}L ${errors.length}E | ${top}`;
  }
  if (lines.length === 0) return 'no logs';
  const out = [`${lines.length} unique log line${lines.length === 1 ? '' : 's'} (${errors.length} errors)`];
  if (errors.length > 0) {
    out.push('');
    out.push('Errors:');
    for (const e of errors.slice(0, 10)) out.push(`  ${e}`);
  }
  out.push('');
  out.push('Top messages:');
  for (const l of lines.slice(0, 10)) out.push(`  ${l}`);
  return out.join('\n');
}

function formatCompose(raw: string, ultra: boolean): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n').filter((l) => l.trim());
  if (ultra) return `compose: ${lines.length}L`;
  return lines.slice(-30).join('\n');
}

function formatKubectlGet(raw: string, ultra: boolean): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n').filter((l) => l.trim());
  const data = lines.slice(1);
  if (ultra) return `${data.length} resources`;
  return `${data.length} resource${data.length === 1 ? '' : 's'}\n${data.slice(0, 20).join('\n')}`;
}
