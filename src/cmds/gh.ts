import { run } from '../core/runner';
import { stripAnsi, truncate } from '../core/utils';
import { HandlerResult } from './git';

// GitHub CLI. `gh` output is dense human tables; we collapse them to counts +
// the leading identifier for each row so the model can still act on them.
export function handleGh(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || '';
  const verb = args[1] || '';
  const result = run('gh', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  const cmdType = `gh ${sub}${verb ? ` ${verb}` : ''}`.trim();

  try {
    if (sub === 'pr' && verb === 'list') filtered = formatPrList(result.stdout, ultra);
    else if (sub === 'pr' && verb === 'view') filtered = formatPrView(result.stdout, ultra);
    else if (sub === 'pr' && verb === 'checks') filtered = formatChecks(result.stdout, ultra);
    else if (sub === 'issue' && verb === 'list') filtered = formatIssueList(result.stdout, ultra);
    else if (sub === 'run' && verb === 'list') filtered = formatRunList(result.stdout, ultra);
    else filtered = stripAnsi(raw).trim(); // unknown gh subcommand: pass through in full
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

function tableRows(raw: string): string[] {
  return stripAnsi(raw)
    .split('\n')
    .map((l) => l.replace(/\r$/, ''))
    .filter((l) => l.trim() !== '' && !/^Showing\b/i.test(l));
}

function formatPrList(raw: string, ultra: boolean): string {
  const rows = tableRows(raw);
  if (rows.length === 0) return ultra ? '0 PRs' : 'no open pull requests';
  if (ultra) return `${rows.length} PRs`;
  const out = [`${rows.length} open PR${rows.length === 1 ? '' : 's'}:`];
  for (const r of rows.slice(0, 20)) {
    const cols = r.split('\t');
    const num = cols[0]?.trim() || '';
    const title = (cols[1] || '').trim().slice(0, 60);
    const branch = (cols[2] || '').trim();
    out.push(`  #${num.replace(/^#/, '')} ${title}${branch ? ` [${branch}]` : ''}`);
  }
  return out.join('\n');
}

function formatIssueList(raw: string, ultra: boolean): string {
  const rows = tableRows(raw);
  if (rows.length === 0) return ultra ? '0 issues' : 'no open issues';
  if (ultra) return `${rows.length} issues`;
  const out = [`${rows.length} open issue${rows.length === 1 ? '' : 's'}:`];
  for (const r of rows.slice(0, 20)) {
    const cols = r.split('\t');
    const num = (cols[0] || '').trim().replace(/^#/, '');
    const title = (cols[2] || cols[1] || '').trim().slice(0, 60);
    out.push(`  #${num} ${title}`);
  }
  return out.join('\n');
}

function formatRunList(raw: string, ultra: boolean): string {
  const rows = tableRows(raw);
  if (rows.length === 0) return ultra ? '0 runs' : 'no workflow runs';
  let ok = 0;
  let fail = 0;
  let other = 0;
  for (const r of rows) {
    if (/\b(completed\s+success|✓|success)\b/i.test(r)) ok++;
    else if (/\b(failure|✗|X|cancelled|timed_out)\b/i.test(r)) fail++;
    else other++;
  }
  if (ultra) return `${ok}✓/${fail}✗${other ? `/${other}?` : ''}`;
  const out = [`${rows.length} run${rows.length === 1 ? '' : 's'}: ${ok} passed, ${fail} failed${other ? `, ${other} in-progress` : ''}`];
  for (const r of rows.slice(0, 10)) {
    const cols = r.split('\t').map((c) => c.trim());
    const status = cols.find((c) => /success|failure|cancelled|in_progress|queued/i.test(c)) || cols[0] || '';
    const name = cols.find((c) => c && c !== status && !/^\d+$/.test(c)) || '';
    out.push(`  ${status} ${name}`.slice(0, 70));
  }
  return out.join('\n');
}

function formatPrView(raw: string, ultra: boolean): string {
  const clean = stripAnsi(raw);
  const title = /^title:\s*(.+)$/im.exec(clean)?.[1]?.trim() || '';
  const state = /^state:\s*(.+)$/im.exec(clean)?.[1]?.trim() || '';
  const reviewers = /^reviewers:\s*(.+)$/im.exec(clean)?.[1]?.trim() || '';
  if (ultra) return `${state || '?'}: ${title}`.slice(0, 60);
  const out = [];
  if (title) out.push(`${title}`);
  if (state) out.push(`state: ${state}`);
  if (reviewers) out.push(`reviewers: ${reviewers}`);
  // Keep the body but trimmed.
  const bodyIdx = clean.indexOf('--\n');
  if (bodyIdx >= 0) {
    const body = clean.slice(bodyIdx + 3).trim();
    if (body) out.push('', truncate(body, 15));
  }
  return out.length ? out.join('\n') : truncate(clean, 20);
}

function formatChecks(raw: string, ultra: boolean): string {
  const rows = tableRows(raw);
  let pass = 0;
  let fail = 0;
  let pending = 0;
  for (const r of rows) {
    if (/\bpass\b|✓/i.test(r)) pass++;
    else if (/\bfail\b|✗|X/i.test(r)) fail++;
    else if (/\bpending\b|in_progress/i.test(r)) pending++;
  }
  if (ultra) return `${pass}✓/${fail}✗${pending ? `/${pending}⋯` : ''}`;
  const out = [`checks: ${pass} passing, ${fail} failing${pending ? `, ${pending} pending` : ''}`];
  if (fail > 0) {
    for (const r of rows.filter((x) => /\bfail\b|✗/i.test(x)).slice(0, 10)) {
      out.push(`  ✗ ${r.split('\t')[0].trim()}`);
    }
  }
  return out.join('\n');
}
