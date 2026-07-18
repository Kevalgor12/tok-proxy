import { run } from '../core/runner';
import { stripAnsi } from '../core/utils';
import { HandlerResult } from './git';

// Compilers and non-JS linters. They all emit diagnostics of the shape
// "error[...]" / "warning: ..." / "path:line:col: message". We count them,
// group by code/rule, and surface the first handful of real errors.

export function handleRuff(args: string[], ultra: boolean): HandlerResult {
  return diagnosticHandler('ruff', 'ruff', args, ultra);
}
export function handleGolangciLint(args: string[], ultra: boolean): HandlerResult {
  return diagnosticHandler('golangci-lint', 'golangci-lint', args, ultra);
}
export function handleRubocop(args: string[], ultra: boolean): HandlerResult {
  return diagnosticHandler('rubocop', 'rubocop', args, ultra);
}
export function handleNext(args: string[], ultra: boolean): HandlerResult {
  return diagnosticHandler('next', 'next', args, ultra);
}

function diagnosticHandler(bin: string, cmdType: string, args: string[], ultra: boolean): HandlerResult {
  const result = run(bin, args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    filtered = summarizeDiagnostics(raw, cmdType, ultra, result.exitCode);
  } catch {
    filtered = raw;
  }
  return {
    filteredOutput: filtered,
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType,
    execMs: result.execMs,
  };
}

interface Diag {
  code: string;
  file: string;
  message: string;
}

// Shared diagnostic summarizer, reused by Go/Rust handlers too.
export function summarizeDiagnostics(raw: string, tool: string, ultra: boolean, exitCode = 0): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n');
  const errors: Diag[] = [];
  let warnings = 0;

  for (const line of lines) {
    // rustc / cargo: "error[E0308]: mismatched types" or "error: ..."
    let m = /^(error|warning)(?:\[([A-Z]\d+)\])?:\s+(.+)/.exec(line);
    if (m) {
      if (m[1] === 'warning') { warnings++; continue; }
      errors.push({ code: m[2] || 'error', file: '', message: m[3].trim() });
      continue;
    }
    // path:line:col: message  (ruff / golangci-lint / go build / rubocop)
    m = /^(.+?):(\d+):(\d+):?\s+(?:([A-Z]\d+|[A-Z]\/\w+)\s+)?(.+)/.exec(line);
    if (m && /\.\w+$/.test(m[1])) {
      const isWarn = /\bwarn/i.test(m[5]);
      if (isWarn) { warnings++; continue; }
      errors.push({ code: m[4] || 'lint', file: m[1], message: m[5].trim().slice(0, 100) });
    }
  }

  // Fall back to the tool's own summary line if we parsed nothing structured.
  if (errors.length === 0 && warnings === 0) {
    const okRe = /(compiled successfully|no issues|0 offenses|Finished|Found 0 errors|test result: ok)/i;
    if (exitCode === 0 || okRe.test(clean)) return ultra ? '✓' : `✓ ${tool}: clean`;
    // Unknown failure shape - surface the tail so the error isn't lost.
    const tail = lines.filter((l) => l.trim()).slice(-8).join('\n');
    return ultra ? `✗ ${tool}` : tail || `✗ ${tool} failed`;
  }

  if (ultra) return `${errors.length}E${warnings ? `/${warnings}W` : ''}`;

  const byCode = new Map<string, number>();
  for (const e of errors) byCode.set(e.code, (byCode.get(e.code) || 0) + 1);

  const out = [`${errors.length} error${errors.length === 1 ? '' : 's'}${warnings ? `, ${warnings} warning${warnings === 1 ? '' : 's'}` : ''} (${tool}):`];
  const grouped = Array.from(byCode.entries()).sort((a, b) => b[1] - a[1]);
  if (grouped.length > 1) {
    out.push('');
    out.push('By code:');
    for (const [code, n] of grouped.slice(0, 8)) out.push(`  ${code}: ${n}`);
  }
  out.push('');
  for (const e of errors.slice(0, 12)) {
    out.push(`  ${e.file ? `${e.file}: ` : ''}[${e.code}] ${e.message}`);
  }
  if (errors.length > 12) out.push(`  … +${errors.length - 12} more`);
  return out.join('\n');
}
