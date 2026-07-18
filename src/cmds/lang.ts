import { run } from '../core/runner';
import { stripAnsi } from '../core/utils';
import { summarizeDiagnostics } from './build';
import { HandlerResult } from './git';

// Go and Rust toolchains multiplex several actions behind one binary
// (`go test|build|vet`, `cargo test|build|check|clippy`). We dispatch on the
// sub-command: tests get a pass/fail summary, builds/lints get diagnostics.

export function handleGo(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const result = run('go', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    if (sub === 'test') filtered = summarizeGoTest(raw, ultra, result.execMs);
    else if (sub === 'build' || sub === 'vet' || sub === 'install') {
      filtered = summarizeDiagnostics(raw, `go ${sub}`, ultra, result.exitCode);
    } else filtered = stripAnsi(raw).trim(); // go run/get/mod/…: full output
  } catch {
    filtered = raw;
  }
  return {
    filteredOutput: filtered || (result.exitCode === 0 ? 'ok' : raw),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: `go ${sub || 'cmd'}`,
    execMs: result.execMs,
  };
}

export function handleCargo(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const result = run('cargo', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    if (sub === 'test') filtered = summarizeCargoTest(raw, ultra, result.execMs);
    else if (sub === 'build' || sub === 'check' || sub === 'clippy') {
      filtered = summarizeDiagnostics(raw, `cargo ${sub}`, ultra, result.exitCode);
    } else filtered = stripAnsi(raw).trim(); // cargo run/fmt/…: full output
  } catch {
    filtered = raw;
  }
  return {
    filteredOutput: filtered || (result.exitCode === 0 ? 'ok' : raw),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: `cargo ${sub || 'cmd'}`,
    execMs: result.execMs,
  };
}

function summarizeGoTest(raw: string, ultra: boolean, execMs: number): string {
  const clean = stripAnsi(raw);
  const failNames: string[] = [];
  const re = /^\s*--- FAIL:\s+(\S+)/gm;
  let m: RegExpExecArray | null;
  while ((m = re.exec(clean)) !== null) failNames.push(m[1]);
  const okPkgs = (clean.match(/^ok\s+\S+/gm) || []).length;
  const failPkgs = (clean.match(/^FAIL\s+\S+/gm) || []).length;

  if (failNames.length === 0 && failPkgs === 0) {
    return ultra ? `✓${okPkgs}pkg` : `✓ tests passed (${okPkgs} package${okPkgs === 1 ? '' : 's'}, ${execMs}ms)`;
  }
  if (ultra) return `✗${failNames.length}`;
  const out = [`${failNames.length} failed test${failNames.length === 1 ? '' : 's'} in ${failPkgs} package${failPkgs === 1 ? '' : 's'}:`, ''];
  for (const n of failNames.slice(0, 20)) out.push(`  ✗ ${n}`);
  return out.join('\n');
}

function summarizeCargoTest(raw: string, ultra: boolean, execMs: number): string {
  const clean = stripAnsi(raw);
  let passed = 0;
  let failed = 0;
  const re = /test result:\s+\w+\.\s+(\d+)\s+passed;\s+(\d+)\s+failed/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(clean)) !== null) {
    passed += parseInt(m[1], 10);
    failed += parseInt(m[2], 10);
  }
  if (failed === 0) {
    return ultra ? `✓${passed}` : `✓ All ${passed} tests passed (${execMs}ms)`;
  }
  const names: string[] = [];
  const failuresBlock = /\nfailures:\n([\s\S]*?)(?:\ntest result:|\n\n|$)/.exec(clean);
  if (failuresBlock) {
    for (const l of failuresBlock[1].split('\n')) {
      const t = l.trim();
      if (t && !t.startsWith('----')) names.push(t);
    }
  }
  if (ultra) return `✗${failed}/${passed + failed}`;
  const out = [`${failed} failed test${failed === 1 ? '' : 's'}:`, ''];
  for (const n of names.slice(0, 20)) out.push(`  ✗ ${n}`);
  out.push('', `Summary: ${passed} passed, ${failed} failed`);
  return out.join('\n').trim();
}
