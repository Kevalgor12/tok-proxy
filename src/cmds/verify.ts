import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';
import { spawnSync } from 'child_process';
import { DB, getMeta } from '../core/local-db';
import { TOK_VERSION, fileExistsSync, readFileIfExists } from '../core/utils';
import { readRegisteredClaudeCommand, probeClaudeHook } from '../core/hook';

interface ToolStatus {
  name: string;
  installed: boolean | 'not-detected';
  version?: string;
  hookPath?: string;
  registered?: boolean;
  hookProbe?: 'pass' | 'fail' | 'skipped';
  mode: 'transparent' | 'instruction' | 'unknown';
  note?: string;
}

const TOK_AWARENESS_FILENAME = 'tok-awareness.md';

export function runVerify(db: DB): string {
  const tools: ToolStatus[] = [];

  // Claude Code — transparent (settings.json command hook)
  const claudeHome = path.join(os.homedir(), '.claude');
  const claudeCmd = readRegisteredClaudeCommand();
  if (claudeCmd) {
    const probe = probeClaudeHook(claudeCmd);
    tools.push({
      name: 'Claude Code',
      installed: true,
      hookPath: claudeCmd,
      registered: true,
      hookProbe: probe.pass ? 'pass' : 'fail',
      mode: 'transparent',
    });
  } else if (fileExistsSync(claudeHome)) {
    tools.push({ name: 'Claude Code', installed: false, mode: 'transparent', note: 'detected but hook not registered — run: tok init --claude' });
  } else {
    tools.push({ name: 'Claude Code', installed: 'not-detected', mode: 'unknown' });
  }

  // Cursor — transparent (script in ~/.cursor/hooks/)
  const cursorHome = path.join(os.homedir(), '.cursor');
  const cursorHook = path.join(cursorHome, 'hooks', 'tok-rewrite.sh');
  const cursorCfg = path.join(cursorHome, 'hooks.json');
  if (fileExistsSync(cursorHook)) {
    tools.push({
      name: 'Cursor',
      installed: true,
      version: readHookVersion(cursorHook),
      hookPath: '~/.cursor/hooks/tok-rewrite.sh',
      registered: isCursorRegistered(cursorCfg),
      hookProbe: probeHook(cursorHook, 'cursor'),
      mode: 'transparent',
    });
  } else if (fileExistsSync(cursorHome)) {
    tools.push({ name: 'Cursor', installed: false, mode: 'transparent', note: 'detected but hook missing — run: tok init --cursor' });
  } else {
    tools.push({ name: 'Cursor', installed: 'not-detected', mode: 'unknown' });
  }

  // Awareness-based (instruction mode) — VS Code Copilot, Gemini, Windsurf, Cline
  pushInstructionStatus(tools, 'Copilot (VS Code)', vscodeAwarenessPath());
  pushInstructionStatus(tools, 'Gemini CLI', path.join(os.homedir(), '.gemini', TOK_AWARENESS_FILENAME));
  pushInstructionStatus(tools, 'Windsurf', path.join(os.homedir(), '.codeium', 'windsurf', TOK_AWARENESS_FILENAME));
  pushInstructionStatus(tools, 'Cline / Roo Code', path.join(os.homedir(), '.cline', TOK_AWARENESS_FILENAME));

  const lines: string[] = ['Hook status:'];
  for (const t of tools) {
    if (t.installed === 'not-detected') {
      lines.push(`  -    ${t.name.padEnd(20)} not detected on this system`);
      continue;
    }
    if (t.installed === false) {
      lines.push(`  FAIL ${t.name.padEnd(20)} ${t.note || 'not installed'}`);
      continue;
    }
    const v = t.version ? ` v${t.version}` : '';
    const modeTag = t.mode === 'transparent' ? '[transparent]' : t.mode === 'instruction' ? '[instruction]' : '';
    lines.push(`  OK   ${t.name.padEnd(20)} ${modeTag}${v}   ${t.hookPath || ''}`);
    if (t.registered === false) {
      lines.push(`         WARN: hook script exists but not registered with the AI tool — re-run tok init`);
    }
    if (t.hookProbe === 'pass') {
      lines.push(`         Probe:   PASS (hook produced expected rewrite)`);
    } else if (t.hookProbe === 'fail') {
      lines.push(`         Probe:   FAIL (hook output does not match expected protocol — run: tok hook-test)`);
    }
  }

  const hookV = getMeta(db, 'hook_version');
  lines.push('');
  if (hookV && hookV !== TOK_VERSION) {
    lines.push(`WARN: hooks recorded as v${hookV} but tok is v${TOK_VERSION}. Run: tok init  to refresh.`);
  } else if (hookV) {
    lines.push(`Hooks are current (v${hookV}).`);
  } else {
    lines.push('No hook version recorded yet — run: tok init');
  }
  return lines.join('\n');
}

function readHookVersion(p: string): string | undefined {
  try {
    const content = fs.readFileSync(p, 'utf8');
    const m = /tok-hook-version:\s*([\w.-]+)/.exec(content);
    return m?.[1];
  } catch {
    return undefined;
  }
}

function isCursorRegistered(cfgPath: string): boolean {
  const raw = readFileIfExists(cfgPath);
  if (!raw) return false;
  try {
    const cfg = JSON.parse(raw) as Record<string, unknown>;
    const list1 = (cfg.preToolUse as Array<Record<string, unknown>> | undefined) || [];
    const hooksObj = cfg.hooks as Record<string, unknown> | undefined;
    const list2 = (hooksObj?.preToolUse as Array<Record<string, unknown>> | undefined) || [];
    return [...list1, ...list2].some((h) => h.id === 'tok-rewrite' || /tok-rewrite\.sh/.test(String(h.command || '')));
  } catch {
    return false;
  }
}

function probeHook(hookPath: string, kind: 'claude' | 'cursor'): 'pass' | 'fail' | 'skipped' {
  try {
    const payload = JSON.stringify({ tool_name: 'Bash', tool_input: { command: 'git status' } });
    const r = spawnSync('bash', [hookPath], { input: payload, encoding: 'utf8' });
    if (r.error || r.status !== 0) return 'fail';
    const stdout = (r.stdout || '').trim();
    if (!stdout) return 'fail';
    let parsed: any;
    try { parsed = JSON.parse(stdout); } catch { return 'fail'; }
    const cmd = kind === 'claude'
      ? parsed?.hookSpecificOutput?.updatedInput?.command
      : parsed?.updated_input?.command;
    return typeof cmd === 'string' && cmd.startsWith('tok ') ? 'pass' : 'fail';
  } catch {
    return 'skipped';
  }
}

function vscodeAwarenessPath(): string {
  const candidates = [
    path.join(process.env.APPDATA || '', 'Code', 'User', TOK_AWARENESS_FILENAME),
    path.join(os.homedir(), 'Library', 'Application Support', 'Code', 'User', TOK_AWARENESS_FILENAME),
    path.join(os.homedir(), '.config', 'Code', 'User', TOK_AWARENESS_FILENAME),
  ];
  return candidates.find(fileExistsSync) || candidates[0];
}

function pushInstructionStatus(tools: ToolStatus[], name: string, mdPath: string): void {
  if (fileExistsSync(mdPath)) {
    tools.push({
      name,
      installed: true,
      version: readHookVersion(mdPath),
      hookPath: tildify(mdPath),
      mode: 'instruction',
    });
  } else {
    tools.push({ name, installed: 'not-detected', mode: 'instruction' });
  }
}

function tildify(p: string): string {
  const home = os.homedir();
  return p.startsWith(home) ? p.replace(home, '~') : p;
}
