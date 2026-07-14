import * as os from 'os';
import * as path from 'path';
import { spawnSync } from 'child_process';
import { rewriteCommand } from './registry';
import { readFileIfExists, safeJsonParse } from './utils';

// Node-free Claude Code PreToolUse hook. Registered in settings.json as
// `tok hook claude`: Claude Code pipes the tool-call JSON to stdin and reads our
// decision JSON from stdout. Doing the whole protocol inside tok (no shell script,
// no `node -e`, no `jq`) is what lets tok ship as a single standalone binary.
//
// Returns the JSON string to print, or null to pass the command through untouched.
export function buildClaudeHookOutput(payload: string): string | null {
  let obj: { tool_name?: unknown; tool_input?: { command?: unknown } } | null;
  try {
    obj = JSON.parse(payload);
  } catch {
    return null;
  }
  if (!obj || String(obj.tool_name || '') !== 'Bash') return null;
  const command = String(obj.tool_input?.command || '');
  if (!command) return null;

  const outcome = rewriteCommand(command);
  if (outcome.kind !== 'allow' && outcome.kind !== 'ask') {
    return null; // none / deny → leave the command untouched
  }

  const toolInput = { ...(obj.tool_input || {}), command: outcome.rewritten };
  const hookSpecificOutput: Record<string, unknown> = {
    hookEventName: 'PreToolUse',
    updatedInput: toolInput,
  };
  // "allow" auto-approves the rewritten command; "ask" leaves the user prompt intact.
  if (outcome.kind === 'allow') {
    hookSpecificOutput.permissionDecision = 'allow';
    hookSpecificOutput.permissionDecisionReason = 'tok auto-rewrite';
  }
  return JSON.stringify({ hookSpecificOutput });
}

// How the hook should invoke tok. Preference:
//   1. `tok` on PATH (a global npm link or a binary already on PATH) — clean + portable.
//   2. A packaged single-file binary that isn't on PATH yet (e.g. mid-install): invoke
//      ourselves by absolute path so the hook works before PATH changes take effect.
//   3. A source checkout: `node <abs main.js>`.
export function resolveTokInvocation(): string {
  if (whichTok()) return 'tok';
  if ((process as unknown as { pkg?: unknown }).pkg) {
    const exe = process.execPath.replace(/\\/g, '/');
    return /\s/.test(exe) ? `"${exe}"` : exe;
  }
  const mainJs = path.resolve(__dirname, '..', 'main.js').replace(/\\/g, '/');
  return `node ${mainJs}`;
}

function whichTok(): boolean {
  const isWin = process.platform === 'win32';
  const r = spawnSync(isWin ? 'where' : 'which', ['tok'], { encoding: 'utf8', shell: isWin });
  return r.status === 0;
}

// The command string registered in settings.json (e.g. "tok hook claude").
export function claudeHookCommand(): string {
  return `${resolveTokInvocation()} hook claude`;
}

// Read the tok PreToolUse hook command currently registered in ~/.claude/settings.json
// (matches both the command form and the legacy tok-rewrite.sh script).
export function readRegisteredClaudeCommand(): string | null {
  const p = path.join(os.homedir(), '.claude', 'settings.json');
  const raw = readFileIfExists(p);
  if (!raw) return null;
  const cfg = safeJsonParse<{ hooks?: { PreToolUse?: Array<{ hooks?: Array<{ command?: string }> }> } }>(raw);
  const pre = cfg?.hooks?.PreToolUse;
  if (!Array.isArray(pre)) return null;
  for (const entry of pre) {
    for (const h of entry.hooks || []) {
      const cmd = String(h.command || '');
      if (/hook\s+claude/.test(cmd) || /tok-rewrite\.sh/.test(cmd)) return cmd;
    }
  }
  return null;
}

// Confirm the hook rewrites a fake Bash tool-call to a `tok` command. This runs the
// exact function the registered `tok hook claude` command executes when Claude Code
// fires it — in-process, so the self-check is reliable. (We deliberately do NOT
// re-spawn tok here: a packaged binary spawning itself with piped stdin is unreliable
// under pkg, and Claude Code — regular Node — invokes the hook fine regardless.) The
// wiring around it (command registered, `tok` on PATH) is checked separately.
export function probeClaudeHook(_command?: string): { pass: boolean; rewrite?: string; reason?: string } {
  const payload = JSON.stringify({ tool_name: 'Bash', tool_input: { command: 'git status' } });
  const out = buildClaudeHookOutput(payload);
  if (!out) return { pass: false, reason: 'hook did not rewrite a Bash git command' };
  const parsed = safeJsonParse<{ hookSpecificOutput?: { updatedInput?: { command?: unknown } } }>(out);
  const rewritten = parsed?.hookSpecificOutput?.updatedInput?.command;
  if (typeof rewritten === 'string' && rewritten.startsWith('tok ')) return { pass: true, rewrite: rewritten };
  return { pass: false, reason: 'unexpected hook output' };
}
