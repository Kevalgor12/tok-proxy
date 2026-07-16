import { run } from '../core/runner';
import { stripAnsi, truncate } from '../core/utils';
import { HandlerResult } from './git';

// Infrastructure-as-code: pulumi + terraform. Their plans are enormous resource
// dumps; the actionable part is the change summary (+create / ~update / -delete)
// plus any errors.

export function handlePulumi(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const result = run('pulumi', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    filtered = summarizePulumi(raw, ultra, result.exitCode);
  } catch {
    filtered = raw;
  }
  return {
    filteredOutput: filtered || (result.exitCode === 0 ? 'ok' : raw),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: `pulumi ${sub || 'cmd'}`,
    execMs: result.execMs,
  };
}

export function handleTerraform(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const result = run('terraform', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    filtered = summarizeTerraform(raw, ultra, result.exitCode);
  } catch {
    filtered = raw;
  }
  return {
    filteredOutput: filtered || (result.exitCode === 0 ? 'ok' : raw),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: `terraform ${sub || 'cmd'}`,
    execMs: result.execMs,
  };
}

function summarizePulumi(raw: string, ultra: boolean, exitCode: number): string {
  const clean = stripAnsi(raw);
  // "Resources: + 3 to create, ~ 1 to update, - 2 to delete" (preview)
  // "Resources: + 3 created, ~ 1 updated" (up) / "- 5 deleted" (destroy)
  const create = num(/([+])\s*(\d+)\s*(?:to create|created)/.exec(clean));
  const update = num(/([~])\s*(\d+)\s*(?:to update|updated|changed)/.exec(clean));
  const del = num(/([-])\s*(\d+)\s*(?:to delete|deleted)/.exec(clean));
  const errors = clean.split('\n').filter((l) => /^\s*error:/i.test(l));

  if (errors.length > 0) {
    if (ultra) return `✗${errors.length}err`;
    return `✗ pulumi failed:\n${errors.slice(0, 6).join('\n')}`;
  }
  if (create + update + del === 0) {
    if (exitCode !== 0) return truncate(clean, ultra ? 3 : 12);
    return ultra ? '✓ no changes' : '✓ no changes';
  }
  if (ultra) return `+${create}~${update}-${del}`;
  return `Resources: +${create} create, ~${update} update, -${del} delete`;
}

function summarizeTerraform(raw: string, ultra: boolean, exitCode: number): string {
  const clean = stripAnsi(raw);
  let m = /Plan:\s+(\d+)\s+to add,\s+(\d+)\s+to change,\s+(\d+)\s+to destroy/.exec(clean);
  if (!m) m = /Apply complete!\s+Resources:\s+(\d+)\s+added,\s+(\d+)\s+changed,\s+(\d+)\s+destroyed/.exec(clean);
  const errors = clean.split('\n').filter((l) => /^(Error:|╷|│ Error)/.test(l));
  if (errors.length > 0) {
    if (ultra) return `✗${errors.length}err`;
    return `✗ terraform error:\n${errors.slice(0, 6).join('\n')}`;
  }
  if (!m) {
    if (/No changes/i.test(clean)) return ultra ? '✓ no changes' : '✓ no changes';
    if (exitCode === 0) return ultra ? '✓' : '✓ ok';
    return truncate(clean, ultra ? 3 : 12);
  }
  if (ultra) return `+${m[1]}~${m[2]}-${m[3]}`;
  return `Plan: +${m[1]} add, ~${m[2]} change, -${m[3]} destroy`;
}

function num(m: RegExpExecArray | null): number {
  return m ? parseInt(m[2], 10) : 0;
}
