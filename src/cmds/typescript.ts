import { run } from '../core/runner';
import { stripAnsi } from '../core/utils';
import { HandlerResult } from './git';

export function handleTsc(args: string[], ultra: boolean): HandlerResult {
  const result = run('tsc', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';

  try {
    filtered = groupErrors(raw, ultra);
  } catch {
    filtered = raw;
  }

  return {
    filteredOutput: filtered || (result.exitCode === 0 ? (ultra ? '✓' : '✓ no errors') : raw),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: 'tsc',
    execMs: result.execMs,
  };
}

function groupErrors(raw: string, ultra = false): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n');

  type ErrInfo = { msg: string; count: number };
  const byFile = new Map<string, Map<string, ErrInfo>>();
  let totalErrors = 0;

  for (const line of lines) {
    const m = /^(.+?)\((\d+),(\d+)\):\s+error\s+(TS\d+):\s+(.+)/.exec(line);
    if (!m) continue;
    const file = m[1];
    const code = m[4];
    const msg = m[5];
    totalErrors++;
    let fileMap = byFile.get(file);
    if (!fileMap) {
      fileMap = new Map();
      byFile.set(file, fileMap);
    }
    const existing = fileMap.get(code);
    if (existing) existing.count++;
    else fileMap.set(code, { msg, count: 1 });
  }

  if (totalErrors === 0) {
    return ultra ? '✓' : '✓ no errors';
  }

  if (ultra) {
    const parts: string[] = [];
    for (const [file, codes] of byFile) {
      const fileShort = file.split(/[\/\\]/).pop() || file;
      const codeStrs: string[] = [];
      for (const [code, info] of codes) {
        codeStrs.push(`${code}×${info.count}`);
      }
      parts.push(`${fileShort}:${codeStrs.join(',')}`);
    }
    return `${totalErrors}E/${byFile.size}F: ${parts.slice(0, 5).join(' ')}`;
  }

  const out: string[] = [];
  for (const [file, codes] of byFile) {
    const total = Array.from(codes.values()).reduce((s, e) => s + e.count, 0);
    out.push(`${file}: ${total} error${total === 1 ? '' : 's'}`);
    for (const [code, info] of codes) {
      const xN = info.count > 1 ? `(×${info.count}) ` : `(×${info.count}) `;
      out.push(`  ${code} ${xN}${info.msg}`);
    }
  }
  out.push('');
  out.push(`Total: ${totalErrors} error${totalErrors === 1 ? '' : 's'} in ${byFile.size} file${byFile.size === 1 ? '' : 's'}`);
  return out.join('\n');
}
