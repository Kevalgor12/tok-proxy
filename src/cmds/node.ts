import { run } from '../core/runner';
import { stripAnsi, truncate } from '../core/utils';
import { HandlerResult } from './git';

type PM = 'npm' | 'pnpm' | 'yarn';

export function handleNode(pm: PM, args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const result = run(pm, args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  let cmdType = `${pm} ${sub || 'cmd'}`;

  try {
    if (sub === 'install' || sub === 'i' || sub === 'add' || (pm === 'yarn' && sub === '')) {
      filtered = filterInstallOutput(raw, pm, ultra);
    } else if (sub === 'list' || sub === 'ls') {
      filtered = filterList(raw, ultra);
    } else if (sub === 'outdated') {
      filtered = filterOutdated(raw, ultra);
    } else if (sub === 'run' || sub === 'run-script' || (pm === 'yarn' && sub !== 'install')) {
      filtered = filterRun(raw, result.exitCode, ultra);
    } else {
      filtered = truncate(stripAnsi(raw).trim(), 30);
    }
  } catch {
    filtered = raw;
  }

  return {
    filteredOutput: filtered || (result.exitCode === 0 ? 'ok' : (result.stderr || raw).trim()),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType,
    execMs: result.execMs,
  };
}

function filterInstallOutput(raw: string, pm: PM, ultra = false): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n');

  let total = 0;
  // npm/pnpm patterns
  const m1 = /added\s+(\d+)\s+package/.exec(clean);
  const m2 = /(\d+)\s+packages? installed/.exec(clean);
  const m3 = /Done in [\d.]+s/.exec(clean);
  const m4 = /Progress:.*?([\d]+)\/([\d]+)/.exec(clean);
  const m5 = /\+\s+(\S+)@(\S+)/g;
  if (m1) total = parseInt(m1[1], 10);
  else if (m2) total = parseInt(m2[1], 10);

  const direct: string[] = [];
  let dm: RegExpExecArray | null;
  while ((dm = m5.exec(clean)) !== null) {
    direct.push(`${dm[1]}@${dm[2]}`);
  }
  for (const line of lines) {
    const a = /^added\s+(\S+)@(\S+)/.exec(line.trim());
    if (a) direct.push(`${a[1]}@${a[2]}`);
  }

  const errors: string[] = [];
  const warnings: string[] = [];
  for (const line of lines) {
    if (/^npm ERR!|^pnpm ERR|^error /i.test(line)) errors.push(line.trim());
    if (/^npm warn|^pnpm WARN|^warning /i.test(line)) warnings.push(line.trim());
  }
  const uniqueWarnings = Array.from(new Set(warnings)).slice(0, 5);
  const uniqueDirect = Array.from(new Set(direct)).slice(0, 10);

  if (errors.length > 0) {
    if (ultra) return `✗${errors.length}err`;
    return `✗ Install failed\n${errors.slice(0, 5).join('\n')}`;
  }

  if (ultra) {
    const tag = total > 0 ? `✓${total}` : '✓ok';
    if (uniqueDirect.length > 0) {
      const compact = uniqueDirect
        .map((d) => d.replace(/@(\d+)\.\d+\.\d+/, '@$1'))
        .slice(0, 3)
        .join(' ');
      return `${tag} ${compact}`;
    }
    return tag;
  }

  const out: string[] = [];
  if (total > 0) out.push(`✓ Installed ${total} packages`);
  else if (m3) out.push('✓ Done');
  else out.push('✓ ok');
  if (uniqueDirect.length > 0) {
    out.push('');
    out.push(`New: ${uniqueDirect.join(', ')}`);
  }
  if (uniqueWarnings.length > 0) {
    out.push('');
    out.push(`Warnings (${uniqueWarnings.length}):`);
    for (const w of uniqueWarnings) out.push(`  ${w}`);
  }
  return out.join('\n');
}

function filterList(raw: string, ultra: boolean): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n').filter((l) => /\S/.test(l));
  const top: string[] = [];
  for (const line of lines) {
    if (/^[├└]/.test(line) || /^\S+@/.test(line)) {
      if (!/[│ ]{2}/.test(line)) top.push(line.trim());
    }
  }
  if (ultra) return `${top.length} pkgs`;
  if (top.length === 0) return truncate(clean, 30);
  return `Top-level dependencies (${top.length}):\n${top.slice(0, 30).join('\n')}`;
}

function filterOutdated(raw: string, ultra: boolean): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n').filter((l) => /\S/.test(l));
  const dataLines = lines.slice(1).filter((l) => !/^Package/i.test(l));
  if (ultra) return `${dataLines.length} outdated`;
  if (dataLines.length === 0) return '✓ All up-to-date';
  return `${dataLines.length} outdated packages\n${dataLines.slice(0, 20).join('\n')}`;
}

function filterRun(raw: string, exitCode: number, ultra: boolean): string {
  const clean = stripAnsi(raw);
  if (exitCode === 0) {
    if (ultra) return '✓';
    const lastLines = clean.split('\n').filter((l) => l.trim()).slice(-5).join('\n');
    return lastLines || '✓ ok';
  }
  if (ultra) return '✗';
  return truncate(clean, 30);
}
