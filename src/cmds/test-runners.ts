import { run } from '../core/runner';
import { stripAnsi, safeJsonParse } from '../core/utils';
import { HandlerResult } from './git';

type Runner = 'jest' | 'vitest' | 'mocha';

interface JestJson {
  numTotalTests?: number;
  numPassedTests?: number;
  numFailedTests?: number;
  startTime?: number;
  testResults?: Array<{
    testFilePath?: string;
    name?: string;
    assertionResults?: Array<{
      ancestorTitles?: string[];
      title?: string;
      fullName?: string;
      status?: string;
      failureMessages?: string[];
    }>;
  }>;
}

export function handleTestRunner(runner: Runner, args: string[], ultra: boolean): HandlerResult {
  let cmd = runner;
  let cmdArgs = args.slice();

  if (runner === 'jest' && !cmdArgs.includes('--json')) {
    cmdArgs = ['--json', ...cmdArgs];
  } else if (runner === 'vitest' && !cmdArgs.includes('--reporter=json')) {
    cmdArgs = ['run', '--reporter=json', ...cmdArgs.filter((a) => a !== 'run')];
  }

  const result = run(cmd, cmdArgs);
  const rawCombined = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';

  try {
    const parsed = tryParseJson(result.stdout);
    if (parsed) {
      filtered = formatFromJson(parsed, ultra, result.execMs);
    } else {
      filtered = formatFromRegex(rawCombined, ultra, result.execMs);
    }
  } catch {
    filtered = rawCombined;
  }

  if (!filtered) {
    filtered = result.exitCode === 0 ? (ultra ? '✓' : '✓ tests passed') : rawCombined;
  }

  return {
    filteredOutput: filtered,
    exitCode: result.exitCode,
    rawOutput: rawCombined,
    cmdType: runner,
    execMs: result.execMs,
  };
}

function tryParseJson(stdout: string): JestJson | null {
  const trimmed = stdout.trim();
  if (!trimmed) return null;
  // Some runners prefix log lines before JSON; find the first { ... } block
  const start = trimmed.indexOf('{');
  if (start === -1) return null;
  const jsonText = trimmed.slice(start);
  const parsed = safeJsonParse<JestJson>(jsonText);
  return parsed && typeof parsed === 'object' ? parsed : null;
}

function formatFromJson(j: JestJson, ultra: boolean, execMs: number): string {
  const total = j.numTotalTests || 0;
  const passed = j.numPassedTests || 0;
  const failed = j.numFailedTests || 0;

  if (failed === 0 && total > 0) {
    return ultra ? `✓${total}` : `✓ All ${total} tests passed (${execMs}ms)`;
  }

  if (ultra) return `✗${failed}/${total}`;

  const failures: string[] = [];
  for (const tr of j.testResults || []) {
    const file = tr.testFilePath || tr.name || 'unknown';
    for (const a of tr.assertionResults || []) {
      if (a.status !== 'failed') continue;
      const ancestors = (a.ancestorTitles || []).join(' > ');
      const title = a.title || a.fullName || '';
      const path = ancestors ? `${ancestors} > ${title}` : title;
      const msg = (a.failureMessages || []).join('\n').split('\n').slice(0, 4).join('\n');
      failures.push(`${file} > ${path}\n  ${msg.replace(/\n/g, '\n  ')}`);
    }
  }

  const out: string[] = [];
  out.push(`${failed} failed test${failed === 1 ? '' : 's'}:`);
  out.push('');
  for (const f of failures.slice(0, 20)) {
    out.push(f);
    out.push('');
  }
  out.push(`Summary: ${passed} passed, ${failed} failed (${total} total)`);
  return out.join('\n').trim();
}

function formatFromRegex(raw: string, ultra: boolean, execMs: number): string {
  const clean = stripAnsi(raw);
  const totalMatch = /Tests?:\s*(?:(\d+)\s+failed,\s*)?(\d+)\s+passed,\s*(\d+)\s+total/i.exec(
    clean,
  );
  let failed = 0;
  let passed = 0;
  let total = 0;
  if (totalMatch) {
    failed = parseInt(totalMatch[1] || '0', 10);
    passed = parseInt(totalMatch[2] || '0', 10);
    total = parseInt(totalMatch[3] || '0', 10);
  }

  // FAIL blocks
  const failBlocks: string[] = [];
  const failRe = /^(FAIL|×|✗)\s+(.+)$/gm;
  let m: RegExpExecArray | null;
  while ((m = failRe.exec(clean)) !== null) {
    failBlocks.push(m[0]);
  }

  if (failed === 0 && failBlocks.length === 0) {
    if (total > 0) return ultra ? `✓${total}` : `✓ All ${total} tests passed (${execMs}ms)`;
    return ultra ? '✓' : '✓ tests passed';
  }

  if (ultra) return `✗${failed || failBlocks.length}/${total || '?'}`;

  const out: string[] = [];
  out.push(`${failed || failBlocks.length} failed test${failed === 1 ? '' : 's'}:`);
  out.push('');
  for (const b of failBlocks.slice(0, 10)) out.push(b);
  out.push('');
  out.push(`Summary: ${passed} passed, ${failed || failBlocks.length} failed (${total} total)`);
  return out.join('\n').trim();
}
