import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { spawnSync } from 'child_process';
import { DB, setMeta } from '../core/local-db';
import { TOK_VERSION, fileExistsSync, readFileIfExists, safeJsonParse, writeFileSafe, ensureDir, chmodIfPosix, appendErrorLog } from '../core/utils';
import { claudeHookCommand, resolveTokInvocation, readRegisteredClaudeCommand } from '../core/hook';
import { generateCursorHook } from '../hooks/cursor.sh';
import { generateAwarenessMd } from '../hooks/awareness-md';

interface InitOptions {
  claude?: boolean;
  cursor?: boolean;
  copilot?: boolean;
  gemini?: boolean;
  windsurf?: boolean;
  cline?: boolean;
  uninstall?: boolean;
  show?: boolean;
}

interface InstallResult {
  tool: string;
  status: 'installed' | 'updated' | 'skipped' | 'not-detected' | 'failed' | 'removed';
  detail?: string;
  mode?: 'transparent' | 'instruction';
}

const HOOK_VERSION_REGEX = /tok-hook-version:\s*([\w.-]+)/;
const TOK_AWARENESS_FILENAME = 'tok-awareness.md';

export function runInit(db: DB, options: InitOptions): string {
  if (options.show) return showHookStatus();
  if (options.uninstall) return uninstallAll();

  const all = !options.claude && !options.cursor && !options.copilot && !options.gemini && !options.windsurf && !options.cline;

  const results: InstallResult[] = [];
  if (all || options.claude) results.push(...installClaudeCode());
  if (all || options.cursor) results.push(installCursor());
  if (all || options.copilot) results.push(installCopilot());
  if (all || options.gemini) results.push(installGemini());
  if (all || options.windsurf) results.push(installWindsurf());
  if (all || options.cline) results.push(installCline());

  setMeta(db, 'hook_version', TOK_VERSION);

  return formatResults(results);
}

function formatResults(results: InstallResult[]): string {
  const lines: string[] = [];
  let anyTransparent = false;
  let anyInstruction = false;
  for (const r of results) {
    if (r.mode === 'transparent') anyTransparent = true;
    if (r.mode === 'instruction') anyInstruction = true;
    const tag =
      r.status === 'installed' ? 'OK' :
      r.status === 'updated' ? 'OK' :
      r.status === 'skipped' ? 'OK' :
      r.status === 'removed' ? 'OK' :
      r.status === 'failed' ? 'FAIL' : '-';
    const note = r.detail ? ` (${r.detail})` : '';
    if (r.status === 'not-detected') {
      lines.push(`  - ${r.tool.padEnd(22)} not detected${note}`);
    } else {
      const modeTag = r.mode === 'instruction' ? ' [instruction-mode]' : r.mode === 'transparent' ? ' [transparent]' : '';
      lines.push(`  ${tag.padEnd(4)} ${r.tool.padEnd(22)} ${r.status}${modeTag}${note}`);
    }
  }
  lines.push('');
  if (anyTransparent) {
    lines.push('Transparent mode: hook intercepts Bash tool calls and rewrites them automatically.');
    lines.push('  → Restart the AI tool, then run: tok hook-test  to confirm.');
  }
  if (anyInstruction) {
    lines.push('Instruction mode: tool reads tok-awareness.md as a system prompt.');
    lines.push('  Compression depends on the model voluntarily prefixing commands with tok.');
  }
  lines.push('  → Then run: tok verify  for a full status report.');
  return lines.join('\n');
}

function installClaudeCode(): InstallResult[] {
  const claudeDir = path.join(os.homedir(), '.claude');
  if (!fileExistsSync(claudeDir)) {
    return [{ tool: 'Claude Code', status: 'not-detected', detail: 'install Claude Code to enable' }];
  }
  const settingsPath = path.join(claudeDir, 'settings.json');
  const hookCommand = claudeHookCommand();

  // Remove the legacy script-based hooks from older tok versions — the hook is now a
  // single self-contained `tok hook claude` command (no shell script, no node needed).
  tryUnlink(path.join(claudeDir, 'hooks', 'tok-rewrite.sh'));
  tryUnlink(path.join(claudeDir, 'hooks', 'tok-usage.sh'));

  const status = mergeClaudeSettings(settingsPath, hookCommand);

  return [{ tool: 'Claude Code', status, detail: `v${TOK_VERSION}`, mode: 'transparent' }];
}

function writeHookIfChanged(p: string, content: string): InstallResult['status'] {
  const existing = readFileIfExists(p);
  if (existing) {
    const m = HOOK_VERSION_REGEX.exec(existing);
    if (m && m[1] === TOK_VERSION && existing === content) return 'skipped';
  }
  const ok = writeFileSafe(p, content);
  if (!ok) return 'failed';
  chmodIfPosix(p, 0o755);
  return existing ? 'updated' : 'installed';
}

function mergeClaudeSettings(settingsPath: string, hookCommand: string): InstallResult['status'] {
  let settings: Record<string, unknown> = {};
  const existing = readFileIfExists(settingsPath);
  if (existing) {
    const parsed = safeJsonParse<Record<string, unknown>>(existing);
    if (parsed) settings = parsed;
  }
  const hooks = (settings.hooks as Record<string, unknown>) || {};
  const preArr = ensureMatcherEntry(hooks, 'PreToolUse');
  const status = upsertHookCommand(preArr, hookCommand);
  // Strip any obsolete PostToolUse entry pointing at the old tok-usage.sh.
  const postArr = hooks.PostToolUse as Array<Record<string, unknown>> | undefined;
  if (Array.isArray(postArr)) {
    for (const entry of postArr) {
      const inner = entry.hooks as Array<Record<string, unknown>> | undefined;
      if (Array.isArray(inner)) {
        entry.hooks = inner.filter((h) => !String(h.command || '').includes('tok-usage.sh'));
      }
    }
  }
  settings.hooks = hooks;
  writeFileSafe(settingsPath, JSON.stringify(settings, null, 2));
  return status;
}

function ensureMatcherEntry(hooks: Record<string, unknown>, eventName: string): Array<Record<string, unknown>> {
  let arr = hooks[eventName] as Array<Record<string, unknown>> | undefined;
  if (!Array.isArray(arr)) {
    arr = [];
    hooks[eventName] = arr;
  }
  let bashEntry = arr.find((entry) => entry.matcher === 'Bash');
  if (!bashEntry) {
    bashEntry = { matcher: 'Bash', hooks: [] };
    arr.push(bashEntry);
  }
  if (!Array.isArray(bashEntry.hooks)) bashEntry.hooks = [];
  return bashEntry.hooks as Array<Record<string, unknown>>;
}

// Register the tok PreToolUse hook command, collapsing any prior tok entries
// (the command form or the legacy tok-rewrite.sh script) into a single current one.
function upsertHookCommand(arr: Array<Record<string, unknown>>, hookCommand: string): InstallResult['status'] {
  const isOurs = (cmd: string) =>
    /tok-rewrite\.sh/.test(cmd) || /\bhook\s+claude\b/.test(cmd) || /\btok\b[^\n]*\brewrite\b/.test(cmd);
  const ours = arr.filter((e) => isOurs(String(e.command || '')));
  if (ours.length > 0) {
    const alreadyCurrent = ours.length === 1 && String(ours[0].command || '') === hookCommand;
    ours[0].type = 'command';
    ours[0].command = hookCommand;
    // Remove any duplicate tok entries left over from earlier installs.
    for (let i = arr.length - 1; i >= 0; i--) {
      if (arr[i] !== ours[0] && isOurs(String(arr[i].command || ''))) arr.splice(i, 1);
    }
    return alreadyCurrent ? 'skipped' : 'updated';
  }
  arr.push({ type: 'command', command: hookCommand });
  return 'installed';
}

function installCursor(): InstallResult {
  const cursorDir = path.join(os.homedir(), '.cursor');
  if (!fileExistsSync(cursorDir)) {
    return { tool: 'Cursor', status: 'not-detected' };
  }
  const hooksDir = path.join(cursorDir, 'hooks');
  ensureDir(hooksDir);
  const scriptPath = path.join(hooksDir, 'tok-rewrite.sh');
  const scriptStatus = writeHookIfChanged(scriptPath, generateCursorHook(TOK_VERSION, resolveTokInvocation()));

  // Wipe the previous fake config that we used to write at ~/.cursor/hooks.json
  // and replace with a real registration referencing the script we just wrote.
  const hooksPath = path.join(cursorDir, 'hooks.json');
  let cfg: Record<string, unknown> = {};
  const existing = readFileIfExists(hooksPath);
  if (existing) {
    const parsed = safeJsonParse<Record<string, unknown>>(existing);
    if (parsed) cfg = parsed;
  }
  // Drop the old shape (preToolUse: [{id, version, command:"tok proxy"}]) — it never worked.
  const oldPre = cfg.preToolUse as Array<Record<string, unknown>> | undefined;
  if (Array.isArray(oldPre)) {
    cfg.preToolUse = oldPre.filter((p) => p.id !== 'tok-rewrite' && !String(p.command || '').includes('tok proxy'));
    if ((cfg.preToolUse as unknown[]).length === 0) delete cfg.preToolUse;
  }
  // Real registration: reference the script file. Cursor invokes it for each tool call.
  const home = os.homedir();
  const tilde = scriptPath.startsWith(home) ? scriptPath.replace(home, '~') : scriptPath;
  const portable = tilde.replace(/\\/g, '/');
  const hooksObj = (cfg.hooks as Record<string, unknown>) || {};
  hooksObj.preToolUse = [{ id: 'tok-rewrite', version: TOK_VERSION, command: portable }];
  cfg.hooks = hooksObj;
  writeFileSafe(hooksPath, JSON.stringify(cfg, null, 2));

  return { tool: 'Cursor', status: scriptStatus, detail: `v${TOK_VERSION}`, mode: 'transparent' };
}

function installCopilot(): InstallResult {
  // Copilot has no public command-rewrite hook protocol; fall back to instruction mode
  // by writing a tok-awareness.md alongside the user's VS Code config dir if found.
  const linuxDir = path.join(os.homedir(), '.config', 'Code');
  const macDir = path.join(os.homedir(), 'Library', 'Application Support', 'Code');
  const winDir = path.join(process.env.APPDATA || '', 'Code');
  const target = [linuxDir, macDir, winDir].find((d) => fileExistsSync(d));
  if (!target) {
    return { tool: 'Copilot (VS Code)', status: 'not-detected' };
  }
  const userDir = path.join(target, 'User');
  ensureDir(userDir);
  const mdPath = path.join(userDir, TOK_AWARENESS_FILENAME);
  const ok = writeFileSafe(mdPath, generateAwarenessMd());
  return {
    tool: 'Copilot (VS Code)',
    status: ok ? 'installed' : 'failed',
    detail: `v${TOK_VERSION}`,
    mode: 'instruction',
  };
}

function installGemini(): InstallResult {
  const geminiDir = path.join(os.homedir(), '.gemini');
  const detected = fileExistsSync(geminiDir) || which('gemini');
  if (!detected) {
    return { tool: 'Gemini CLI', status: 'not-detected' };
  }
  ensureDir(geminiDir);

  // Wipe the legacy fake config we used to write (hooks.BeforeTool with command "tok proxy").
  const settingsPath = path.join(geminiDir, 'settings.json');
  const existing = readFileIfExists(settingsPath);
  if (existing) {
    const parsed = safeJsonParse<Record<string, unknown>>(existing);
    if (parsed) {
      const hooks = parsed.hooks as Record<string, unknown> | undefined;
      if (hooks && Array.isArray(hooks.BeforeTool)) {
        const arr = hooks.BeforeTool as Array<Record<string, unknown>>;
        const filtered = arr.filter((h) => h.id !== 'tok-rewrite');
        if (filtered.length === 0) delete hooks.BeforeTool;
        else hooks.BeforeTool = filtered;
        if (Object.keys(hooks).length === 0) delete parsed.hooks;
        writeFileSafe(settingsPath, JSON.stringify(parsed, null, 2));
      }
    }
  }

  // Drop instruction file in the gemini dir.
  const mdPath = path.join(geminiDir, TOK_AWARENESS_FILENAME);
  const ok = writeFileSafe(mdPath, generateAwarenessMd());
  return { tool: 'Gemini CLI', status: ok ? 'installed' : 'failed', detail: `v${TOK_VERSION}`, mode: 'instruction' };
}

function installWindsurf(): InstallResult {
  const windsurfDir = path.join(os.homedir(), '.codeium', 'windsurf');
  if (!fileExistsSync(windsurfDir)) {
    return { tool: 'Windsurf', status: 'not-detected' };
  }
  const mdPath = path.join(windsurfDir, TOK_AWARENESS_FILENAME);
  const ok = writeFileSafe(mdPath, generateAwarenessMd());
  return { tool: 'Windsurf', status: ok ? 'installed' : 'failed', detail: `v${TOK_VERSION}`, mode: 'instruction' };
}

function installCline(): InstallResult {
  const clineDir = path.join(os.homedir(), '.cline');
  if (!fileExistsSync(clineDir)) {
    return { tool: 'Cline / Roo Code', status: 'not-detected' };
  }
  const mdPath = path.join(clineDir, TOK_AWARENESS_FILENAME);
  const ok = writeFileSafe(mdPath, generateAwarenessMd());
  return { tool: 'Cline / Roo Code', status: ok ? 'installed' : 'failed', detail: `v${TOK_VERSION}`, mode: 'instruction' };
}

function uninstallAll(): string {
  const removed: string[] = [];

  const claudePre = path.join(os.homedir(), '.claude', 'hooks', 'tok-rewrite.sh');
  const claudePost = path.join(os.homedir(), '.claude', 'hooks', 'tok-usage.sh');
  if (tryUnlink(claudePre)) removed.push(claudePre);
  if (tryUnlink(claudePost)) removed.push(claudePost);

  const claudeSettings = path.join(os.homedir(), '.claude', 'settings.json');
  if (removeFromClaudeSettings(claudeSettings)) removed.push(`${claudeSettings} (tok entries)`);

  const cursorScript = path.join(os.homedir(), '.cursor', 'hooks', 'tok-rewrite.sh');
  if (tryUnlink(cursorScript)) removed.push(cursorScript);
  const cursorHookCfg = path.join(os.homedir(), '.cursor', 'hooks.json');
  if (removeFromCursor(cursorHookCfg)) removed.push(`${cursorHookCfg} (tok entries)`);

  const tools: Array<[string, string]> = [
    ['VS Code (Linux)', path.join(os.homedir(), '.config', 'Code', 'User', TOK_AWARENESS_FILENAME)],
    ['VS Code (mac)', path.join(os.homedir(), 'Library', 'Application Support', 'Code', 'User', TOK_AWARENESS_FILENAME)],
    ['VS Code (win)', path.join(process.env.APPDATA || '', 'Code', 'User', TOK_AWARENESS_FILENAME)],
    ['Gemini', path.join(os.homedir(), '.gemini', TOK_AWARENESS_FILENAME)],
    ['Windsurf', path.join(os.homedir(), '.codeium', 'windsurf', TOK_AWARENESS_FILENAME)],
    ['Cline', path.join(os.homedir(), '.cline', TOK_AWARENESS_FILENAME)],
  ];
  for (const [name, p] of tools) {
    if (tryUnlink(p)) removed.push(`${name}: ${p}`);
  }

  if (removed.length === 0) return 'Nothing to uninstall.';
  return ['Removed:', ...removed.map((r) => `  - ${r}`)].join('\n');
}

function tryUnlink(p: string): boolean {
  try {
    if (!fileExistsSync(p)) return false;
    fs.unlinkSync(p);
    return true;
  } catch (err) {
    appendErrorLog('uninstall.unlink', err);
    return false;
  }
}

function removeFromClaudeSettings(p: string): boolean {
  const existing = readFileIfExists(p);
  if (!existing) return false;
  const parsed = safeJsonParse<Record<string, unknown>>(existing);
  if (!parsed) return false;
  const hooks = parsed.hooks as Record<string, unknown> | undefined;
  if (!hooks) return false;
  let changed = false;
  for (const event of ['PreToolUse', 'PostToolUse']) {
    const arr = hooks[event] as Array<Record<string, unknown>> | undefined;
    if (!Array.isArray(arr)) continue;
    for (const entry of arr) {
      const inner = entry.hooks as Array<Record<string, unknown>> | undefined;
      if (!Array.isArray(inner)) continue;
      const before = inner.length;
      entry.hooks = inner.filter((h) => {
        const cmd = String(h.command || '');
        // Remove both the legacy script hooks and the `tok hook claude` command.
        return !(/tok-(rewrite|usage)\.sh/.test(cmd) || /\bhook\s+claude\b/.test(cmd) || /\btok\b[^\n]*\brewrite\b/.test(cmd));
      });
      if ((entry.hooks as unknown[]).length !== before) changed = true;
    }
  }
  if (changed) writeFileSafe(p, JSON.stringify(parsed, null, 2));
  return changed;
}

function removeFromCursor(p: string): boolean {
  const existing = readFileIfExists(p);
  if (!existing) return false;
  const parsed = safeJsonParse<Record<string, unknown>>(existing);
  if (!parsed) return false;
  let changed = false;
  for (const key of ['preToolUse', 'hooks']) {
    if (key === 'preToolUse') {
      const pre = parsed.preToolUse as Array<Record<string, unknown>> | undefined;
      if (Array.isArray(pre)) {
        const before = pre.length;
        parsed.preToolUse = pre.filter((h) => h.id !== 'tok-rewrite');
        if ((parsed.preToolUse as unknown[]).length !== before) changed = true;
        if ((parsed.preToolUse as unknown[]).length === 0) delete parsed.preToolUse;
      }
    } else {
      const obj = parsed.hooks as Record<string, unknown> | undefined;
      if (obj && Array.isArray(obj.preToolUse)) {
        const arr = obj.preToolUse as Array<Record<string, unknown>>;
        const before = arr.length;
        obj.preToolUse = arr.filter((h) => h.id !== 'tok-rewrite');
        if ((obj.preToolUse as unknown[]).length !== before) changed = true;
        if ((obj.preToolUse as unknown[]).length === 0) delete obj.preToolUse;
        if (Object.keys(obj).length === 0) delete parsed.hooks;
      }
    }
  }
  if (changed) writeFileSafe(p, JSON.stringify(parsed, null, 2));
  return changed;
}

function showHookStatus(): string {
  const lines: string[] = ['Hook installation status:'];

  // Claude Code is now a settings.json command entry, not a script file.
  const claudeCmd = readRegisteredClaudeCommand();
  if (claudeCmd) {
    lines.push(`  OK  ${'Claude Code (hook)'.padEnd(22)} ${claudeCmd}`);
  } else {
    lines.push(`  -   ${'Claude Code (hook)'.padEnd(22)} not installed`);
  }

  const checks: Array<[string, string]> = [
    ['Cursor (script)', path.join(os.homedir(), '.cursor', 'hooks', 'tok-rewrite.sh')],
    ['Cursor (config)', path.join(os.homedir(), '.cursor', 'hooks.json')],
    ['VS Code (awareness)', path.join(process.env.APPDATA || '', 'Code', 'User', TOK_AWARENESS_FILENAME)],
    ['Gemini (awareness)', path.join(os.homedir(), '.gemini', TOK_AWARENESS_FILENAME)],
    ['Windsurf (awareness)', path.join(os.homedir(), '.codeium', 'windsurf', TOK_AWARENESS_FILENAME)],
    ['Cline (awareness)', path.join(os.homedir(), '.cline', TOK_AWARENESS_FILENAME)],
  ];
  for (const [name, p] of checks) {
    if (fileExistsSync(p)) {
      const v = readHookVersionFromPath(p);
      lines.push(`  OK  ${name.padEnd(22)} ${p}${v ? ` (v${v})` : ''}`);
    } else {
      lines.push(`  -   ${name.padEnd(22)} not installed`);
    }
  }
  return lines.join('\n');
}

function readHookVersionFromPath(p: string): string | undefined {
  const c = readFileIfExists(p);
  if (!c) return undefined;
  const m = HOOK_VERSION_REGEX.exec(c);
  return m?.[1];
}

function which(cmd: string): boolean {
  const isWin = process.platform === 'win32';
  const result = spawnSync(isWin ? 'where' : 'which', [cmd], {
    encoding: 'utf8',
    shell: isWin,
  });
  return result.status === 0;
}

