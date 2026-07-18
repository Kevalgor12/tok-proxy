import { run } from '../core/runner';
import { stripAnsi } from '../core/utils';
import { HandlerResult } from './git';

// Test runners that emit human-readable output (no stable JSON): pytest, rspec,
// minitest (rake test), playwright. Each is collapsed to a pass/fail summary plus
// the failing test names - the only part an agent needs to act on.

type MoreRunner = 'pytest' | 'rspec' | 'rake' | 'playwright';

export function handleMoreTests(runner: MoreRunner, args: string[], ultra: boolean): HandlerResult {
  const bin = runner === 'rake' ? 'rake' : runner;
  const result = run(bin, args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    if (runner === 'pytest') filtered = summarizePytest(raw, ultra, result.execMs);
    else if (runner === 'rspec') filtered = summarizeRspec(raw, ultra, result.execMs);
    else if (runner === 'rake') filtered = summarizeMinitest(raw, ultra, result.execMs);
    else filtered = summarizePlaywright(raw, ultra, result.execMs);
  } catch {
    filtered = raw;
  }
  if (!filtered) filtered = result.exitCode === 0 ? (ultra ? '✓' : '✓ tests passed') : raw;
  return {
    filteredOutput: filtered,
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: runner === 'rake' ? 'rake test' : runner,
    execMs: result.execMs,
  };
}

function passLine(passed: number, ultra: boolean, execMs: number): string {
  return ultra ? `✓${passed}` : `✓ All ${passed} tests passed (${execMs}ms)`;
}

function failBlock(failed: number, passed: number, total: number, names: string[], ultra: boolean): string {
  if (ultra) return `✗${failed}/${total || passed + failed}`;
  const out = [`${failed} failed test${failed === 1 ? '' : 's'}:`, ''];
  for (const n of names.slice(0, 20)) out.push(`  ✗ ${n}`);
  out.push('', `Summary: ${passed} passed, ${failed} failed${total ? ` (${total} total)` : ''}`);
  return out.join('\n').trim();
}

function summarizePytest(raw: string, ultra: boolean, execMs: number): string {
  const clean = stripAnsi(raw);
  const failed = num(/(\d+)\s+failed/.exec(clean));
  const passed = num(/(\d+)\s+passed/.exec(clean));
  const errors = num(/(\d+)\s+error/.exec(clean));
  const total = failed + passed + errors;
  if (failed === 0 && errors === 0) return passLine(passed || total, ultra, execMs);
  const names: string[] = [];
  const re = /^FAILED\s+(\S+)/gm;
  let m: RegExpExecArray | null;
  while ((m = re.exec(clean)) !== null) names.push(m[1]);
  return failBlock(failed + errors, passed, total, names, ultra);
}

function summarizeRspec(raw: string, ultra: boolean, execMs: number): string {
  const clean = stripAnsi(raw);
  const m = /(\d+)\s+examples?,\s+(\d+)\s+failures?/.exec(clean);
  const examples = m ? parseInt(m[1], 10) : 0;
  const failures = m ? parseInt(m[2], 10) : 0;
  if (failures === 0) return passLine(examples, ultra, execMs);
  const names: string[] = [];
  const re = /^rspec\s+(\S+)\s+#\s+(.+)$/gm;
  let mm: RegExpExecArray | null;
  while ((mm = re.exec(clean)) !== null) names.push(`${mm[2].trim()} (${mm[1]})`);
  return failBlock(failures, examples - failures, examples, names, ultra);
}

function summarizeMinitest(raw: string, ultra: boolean, execMs: number): string {
  const clean = stripAnsi(raw);
  const m = /(\d+)\s+runs?,\s+\d+\s+assertions?,\s+(\d+)\s+failures?,\s+(\d+)\s+errors?/.exec(clean);
  const runs = m ? parseInt(m[1], 10) : 0;
  const failures = m ? parseInt(m[2], 10) : 0;
  const errors = m ? parseInt(m[3], 10) : 0;
  if (failures === 0 && errors === 0) return passLine(runs, ultra, execMs);
  const names: string[] = [];
  const re = /^\s*\d+\)\s+(Failure|Error):\s*\n\s*(.+?)(?:\s|$)/gm;
  let mm: RegExpExecArray | null;
  while ((mm = re.exec(clean)) !== null) names.push(mm[2].trim());
  return failBlock(failures + errors, runs - failures - errors, runs, names, ultra);
}

function summarizePlaywright(raw: string, ultra: boolean, execMs: number): string {
  const clean = stripAnsi(raw);
  const failed = num(/(\d+)\s+failed/.exec(clean));
  const passed = num(/(\d+)\s+passed/.exec(clean));
  const flaky = num(/(\d+)\s+flaky/.exec(clean));
  const total = failed + passed + flaky;
  if (failed === 0) {
    if (ultra) return flaky ? `✓${passed}~${flaky}` : `✓${passed}`;
    return `✓ ${passed} passed${flaky ? `, ${flaky} flaky` : ''} (${execMs}ms)`;
  }
  const names: string[] = [];
  const re = /^\s*[✘✗×]\s+(?:\d+\s+)?(.+?)(?:\s+\([\d.]+m?s\))?$/gm;
  let m: RegExpExecArray | null;
  while ((m = re.exec(clean)) !== null) names.push(m[1].trim());
  return failBlock(failed, passed, total, names, ultra);
}

function num(m: RegExpExecArray | null): number {
  return m ? parseInt(m[1], 10) : 0;
}
