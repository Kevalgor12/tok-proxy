import { run } from '../core/runner';
import { stripAnsi } from '../core/utils';
import { HandlerResult } from './git';

export interface Violation {
  file: string;
  line: number;
  rule: string;
  message: string;
}

export function handleLint(linter: 'eslint' | 'biome' | 'prettier', args: string[], ultra: boolean): HandlerResult {
  const result = run(linter, args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    filtered = groupViolations(raw, ultra);
  } catch {
    filtered = raw;
  }
  if (!filtered) {
    filtered = result.exitCode === 0 ? (ultra ? '✓' : '✓ no issues') : raw;
  }
  return {
    filteredOutput: filtered,
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: linter,
    execMs: result.execMs,
  };
}

export function parseViolations(raw: string): Violation[] {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n');
  const out: Violation[] = [];
  let currentFile = '';

  for (const line of lines) {
    // Header: file path
    const fileMatch = /^(\/|[A-Za-z]:\\|\.\/|\.\.\/|[\w./-]+\.[\w]+)$/.exec(line.trim());
    if (fileMatch && /\.[a-z]+$/i.test(line.trim()) && !/:\d+:\d+/.test(line)) {
      currentFile = line.trim();
      continue;
    }

    // ESLint compact format
    let m = /^\s*(\d+):(\d+)\s+(?:error|warning)\s+(.+?)\s+([@\w/-]+)\s*$/.exec(line);
    if (m && currentFile) {
      out.push({ file: currentFile, line: parseInt(m[1], 10), rule: m[4], message: m[3].trim() });
      continue;
    }

    // Inline: file:line:col error/warning message rule
    m = /^(.+?):(\d+):(\d+)\s+(?:error|warning)\s+(.+?)\s+([@\w/-]+)\s*$/.exec(line);
    if (m) {
      out.push({ file: m[1], line: parseInt(m[2], 10), rule: m[5], message: m[4].trim() });
      continue;
    }

    // Stylish-ish: error with rule trailing
    m = /^(.+?):(\d+):(\d+):\s+(.+?)\s+\[([@\w/-]+)\]\s*$/.exec(line);
    if (m) {
      out.push({ file: m[1], line: parseInt(m[2], 10), rule: m[5], message: m[4] });
    }
  }
  return out;
}

export function groupViolations(raw: string, ultra = false): string {
  const violations = parseViolations(raw);
  const total = violations.length;
  if (total === 0) {
    return ultra ? '✓' : '✓ no issues';
  }

  const byRule = new Map<string, Violation[]>();
  const byFile = new Map<string, Violation[]>();
  for (const v of violations) {
    const r = byRule.get(v.rule) || [];
    r.push(v);
    byRule.set(v.rule, r);
    const f = byFile.get(v.file) || [];
    f.push(v);
    byFile.set(v.file, f);
  }

  const sortedRules = Array.from(byRule.entries()).sort((a, b) => b[1].length - a[1].length);
  const sortedFiles = Array.from(byFile.entries()).sort((a, b) => b[1].length - a[1].length);

  if (ultra) {
    const topRules = sortedRules
      .slice(0, 3)
      .map(([rule, vs]) => `${shortRule(rule)}:${vs.length}`)
      .join(' ');
    return `${total}V ${byFile.size}F | ${topRules}`;
  }

  const out: string[] = [];
  out.push(`${total} violation${total === 1 ? '' : 's'} in ${byFile.size} file${byFile.size === 1 ? '' : 's'}`);
  out.push('');
  out.push('By rule:');
  for (const [rule, vs] of sortedRules.slice(0, 10)) {
    const pct = ((vs.length / total) * 100).toFixed(1);
    out.push(`  ${rule}: ${vs.length} (${pct}%)`);
  }
  out.push('');
  out.push('Top files:');
  for (const [file, vs] of sortedFiles.slice(0, 5)) {
    out.push(`  ${file}: ${vs.length} violation${vs.length === 1 ? '' : 's'}`);
  }
  return out.join('\n');
}

function shortRule(rule: string): string {
  const parts = rule.split('/');
  return parts[parts.length - 1];
}
