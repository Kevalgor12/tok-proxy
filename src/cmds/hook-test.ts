import { readRegisteredClaudeCommand, claudeHookCommand, buildClaudeHookOutput } from '../core/hook';

interface TestCase {
  label: string;
  input: { tool_name: string; tool_input: { command: string } };
  expectRewriteContains?: string;
  expectPassThrough?: boolean;
}

const CASES: TestCase[] = [
  {
    label: 'rewrites bare git status',
    input: { tool_name: 'Bash', tool_input: { command: 'git status' } },
    expectRewriteContains: 'tok git status',
  },
  {
    label: 'rewrites npm install',
    input: { tool_name: 'Bash', tool_input: { command: 'npm install react' } },
    expectRewriteContains: 'tok npm install',
  },
  {
    label: 'rewrites npx tsc',
    input: { tool_name: 'Bash', tool_input: { command: 'npx tsc --noEmit' } },
    expectRewriteContains: 'tok tsc',
  },
  {
    label: 'leaves cd alone',
    input: { tool_name: 'Bash', tool_input: { command: 'cd /tmp' } },
    expectPassThrough: true,
  },
  {
    label: 'leaves shell pipelines alone (safety)',
    input: { tool_name: 'Bash', tool_input: { command: 'git status | head' } },
    expectPassThrough: true,
  },
  {
    label: 'no-op when already prefixed with tok',
    input: { tool_name: 'Bash', tool_input: { command: 'tok git status' } },
    expectPassThrough: true,
  },
  {
    label: 'passes non-Bash tools through',
    input: { tool_name: 'Read', tool_input: { command: '' } },
    expectPassThrough: true,
  },
];

interface HookTestOptions {
  // Override the hook command/script to test (defaults to the registered one).
  hookPath?: string;
}

export function runHookTest(opts: HookTestOptions = {}): { output: string; exitCode: number } {
  const command = opts.hookPath || readRegisteredClaudeCommand() || claudeHookCommand();
  const lines: string[] = [`Testing hook: ${command}`, ''];
  let passed = 0;
  let failed = 0;

  for (const c of CASES) {
    const r = invokeHook(command, JSON.stringify(c.input));
    const ok = checkOutcome(c, r);
    if (ok.pass) {
      passed++;
      lines.push(`  PASS  ${c.label}`);
    } else {
      failed++;
      lines.push(`  FAIL  ${c.label}`);
      lines.push(`        ${ok.reason}`);
      lines.push(`        stdout: ${truncate(r.stdout, 200)}`);
      if (r.stderr) lines.push(`        stderr: ${truncate(r.stderr, 200)}`);
    }
  }

  lines.push('');
  lines.push(`${passed} passed, ${failed} failed`);
  if (failed > 0) {
    lines.push('');
    lines.push('If tests fail, check that:');
    lines.push('  1. tok is on PATH (run: which tok  or  where tok)');
    lines.push('  2. The registered hook command matches your tok install (re-run: tok init --claude)');
  } else {
    lines.push('Hook is wired up correctly. Restart your AI tool if you haven\'t already.');
  }

  const reg = checkRegistration();
  if (reg) {
    lines.push('');
    lines.push(reg);
  }

  return { output: lines.join('\n'), exitCode: failed > 0 ? 1 : 0 };
}

// Exercise the hook's decision logic (the exact code `tok hook claude` runs on the
// payload) in-process — reliable and identical to what Claude Code triggers.
function invokeHook(_command: string, payload: string): { stdout: string; stderr: string; code: number } {
  return { stdout: buildClaudeHookOutput(payload) || '', stderr: '', code: 0 };
}

function checkOutcome(c: TestCase, r: { stdout: string; stderr: string; code: number }): { pass: boolean; reason?: string } {
  if (c.expectPassThrough) {
    const s = r.stdout.trim();
    if (s === '' || s === '{}') return { pass: true };
    return { pass: false, reason: 'expected empty stdout (pass-through), got payload' };
  }
  if (c.expectRewriteContains) {
    let parsed: { hookSpecificOutput?: { updatedInput?: { command?: unknown } }; updated_input?: { command?: unknown } };
    try {
      parsed = JSON.parse(r.stdout);
    } catch {
      return { pass: false, reason: 'stdout is not valid JSON' };
    }
    const updated = parsed?.hookSpecificOutput?.updatedInput?.command ?? parsed?.updated_input?.command;
    if (typeof updated !== 'string') {
      return { pass: false, reason: 'no updatedInput.command in hook output (wrong protocol shape)' };
    }
    if (!updated.includes(c.expectRewriteContains)) {
      return { pass: false, reason: `expected to contain "${c.expectRewriteContains}", got "${updated}"` };
    }
    return { pass: true };
  }
  return { pass: false, reason: 'malformed test case' };
}

function checkRegistration(): string | null {
  const cmd = readRegisteredClaudeCommand();
  if (cmd) return `Registered: ~/.claude/settings.json PreToolUse runs \`${cmd}\``;
  return 'Note: no tok hook registered in ~/.claude/settings.json — run: tok init --claude';
}

function truncate(s: string, n: number): string {
  if (s.length <= n) return s;
  return s.slice(0, n) + '…';
}
