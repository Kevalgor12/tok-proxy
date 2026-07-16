import { run } from '../core/runner';
import { stripAnsi, truncate, formatBytes } from '../core/utils';
import { deduplicateLines, extractStructure } from '../core/filter';
import { safeJsonParse } from '../core/utils';
import { HandlerResult } from './git';

// HTTP fetchers (curl/wget) and `env`. curl/wget bodies are only compressed when
// large (JSON → structure, otherwise dedup+truncate); `env` is redacted to keys
// only so secrets never reach the model.

export function handleCurl(args: string[], ultra: boolean): HandlerResult {
  const result = run('curl', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    filtered = summarizeBody(result.stdout || raw, ultra);
  } catch {
    filtered = raw;
  }
  return {
    filteredOutput: filtered || (result.exitCode === 0 ? 'ok' : raw),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: 'curl',
    execMs: result.execMs,
  };
}

export function handleWget(args: string[], ultra: boolean): HandlerResult {
  const result = run('wget', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    const clean = stripAnsi(raw);
    // wget prints progress + a "saved [N]" line to stderr.
    const saved = /saved\s+\[(\d+)(?:\/\d+)?\]/.exec(clean);
    const status = /HTTP request sent.*?\s(\d{3})\b/.exec(clean);
    if (saved) filtered = ultra ? `✓${formatBytes(parseInt(saved[1], 10))}` : `✓ downloaded ${formatBytes(parseInt(saved[1], 10))}${status ? ` (HTTP ${status[1]})` : ''}`;
    else if (result.exitCode !== 0) filtered = clean.split('\n').filter((l) => /error|failed|unable/i.test(l)).slice(0, 5).join('\n') || truncate(clean, 8);
    else filtered = ultra ? '✓' : '✓ ok';
  } catch {
    filtered = raw;
  }
  return {
    filteredOutput: filtered || (result.exitCode === 0 ? 'ok' : raw),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: 'wget',
    execMs: result.execMs,
  };
}

// `env` / `printenv` — show variable NAMES only. Values commonly hold tokens and
// secrets, so we never echo them to the model.
export function handleEnv(args: string[], ultra: boolean): HandlerResult {
  const bin = args[0] === '__printenv__' ? 'printenv' : 'env';
  const realArgs = args[0] === '__printenv__' ? args.slice(1) : args;
  const result = run(bin, realArgs);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    const keys = stripAnsi(result.stdout)
      .split('\n')
      .map((l) => /^([A-Za-z_][A-Za-z0-9_]*)=/.exec(l)?.[1])
      .filter((k): k is string => !!k)
      .sort();
    if (keys.length === 0) filtered = truncate(stripAnsi(raw).trim(), 10);
    else if (ultra) filtered = `${keys.length} vars`;
    else filtered = `${keys.length} environment variables (values redacted):\n${keys.join(', ')}`;
  } catch {
    filtered = raw;
  }
  return {
    filteredOutput: filtered || 'ok',
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: 'env',
    execMs: result.execMs,
  };
}

function summarizeBody(body: string, ultra: boolean): string {
  const clean = stripAnsi(body).trim();
  if (clean === '') return ultra ? '(empty)' : '(empty response)';

  // JSON → collapse to a key/type skeleton when large.
  const parsed = safeJsonParse(clean);
  if (parsed && typeof parsed === 'object') {
    const bytes = Buffer.byteLength(clean, 'utf8');
    if (bytes <= 800) return clean; // small JSON: pass through
    const structure = JSON.stringify(extractStructure(parsed), null, ultra ? 0 : 2);
    return `JSON response (${formatBytes(bytes)}), structure:\n${truncate(structure, ultra ? 20 : 60)}`;
  }

  // HTML → strip tags, note size.
  if (/^\s*<(!doctype|html)/i.test(clean)) {
    const text = clean.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();
    const title = /<title>([^<]+)<\/title>/i.exec(clean)?.[1]?.trim();
    return `HTML (${formatBytes(Buffer.byteLength(clean, 'utf8'))})${title ? ` — "${title}"` : ''}: ${truncate(text, ultra ? 3 : 8)}`;
  }

  const lines = clean.split('\n');
  if (lines.length <= (ultra ? 8 : 40)) return clean;
  return truncate(deduplicateLines(clean), ultra ? 8 : 40);
}
