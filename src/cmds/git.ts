import { run } from '../core/runner';
import { stripAnsi, truncate } from '../core/utils';

export interface HandlerResult {
  filteredOutput: string;
  exitCode: number;
  rawOutput: string;
  cmdType: string;
  execMs: number;
}

export function handleGit(args: string[], ultra: boolean): HandlerResult {
  const sub = args[0] || 'status';
  const result = run('git', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  let cmdType = `git ${sub}`;

  try {
    switch (sub) {
      case 'status':
      case 'st':
        filtered = formatStatusOutput(result.stdout, ultra);
        break;
      case 'diff':
        filtered = compactDiff(result.stdout, ultra);
        break;
      case 'log':
        filtered = formatLog(result.stdout, ultra);
        break;
      case 'push':
        filtered = compactPush(raw);
        break;
      case 'pull':
        filtered = compactPull(raw);
        break;
      case 'add':
        filtered = ultra ? '✓' : 'ok';
        if (result.exitCode !== 0) filtered = (result.stderr || raw).trim() || filtered;
        break;
      case 'commit':
        filtered = compactCommit(raw, ultra);
        break;
      case 'branch':
        filtered = compactBranch(result.stdout, ultra);
        break;
      case 'fetch':
        filtered = ultra ? '✓ fetch' : 'ok: fetched';
        break;
      default:
        filtered = truncate(stripAnsi(raw).trim(), 50);
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

function formatStatusOutput(raw: string, ultra = false): string {
  const clean = stripAnsi(raw);
  if (/working tree clean/i.test(clean) || clean.trim() === '') {
    return ultra ? 'clean' : 'nothing to commit, working tree clean';
  }

  const counts: Record<string, number> = {
    modified: 0,
    'new file': 0,
    deleted: 0,
    renamed: 0,
    untracked: 0,
  };

  for (const line of clean.split('\n')) {
    const m = /^\s*(modified|new file|deleted|renamed):\s+(.+)/.exec(line);
    if (m) {
      counts[m[1]]++;
      continue;
    }
    if (/^\s+([^\s].*)$/.test(line) && /Untracked files/i.test(clean)) {
      // skip headers
    }
    if (/^\?\?\s+/.test(line)) {
      counts.untracked++;
    }
  }

  // Also parse porcelain-like lines (M, A, D, R, ??)
  const porcelain = clean.split('\n').filter((l) => /^[ MADRCU?][ MADRCU?]\s/.test(l));
  if (porcelain.length > 0 && counts.modified + counts['new file'] + counts.deleted + counts.untracked === 0) {
    for (const line of porcelain) {
      const code = line.slice(0, 2);
      if (code === '??') counts.untracked++;
      else if (/[M]/.test(code)) counts.modified++;
      else if (/[A]/.test(code)) counts['new file']++;
      else if (/[D]/.test(code)) counts.deleted++;
      else if (/[R]/.test(code)) counts.renamed++;
    }
  }

  // Untracked from "Untracked files:" section
  const untrackedSection = /Untracked files:[\s\S]*?(?=\n\n|\nChanges|$)/.exec(clean);
  if (untrackedSection) {
    const lines = untrackedSection[0]
      .split('\n')
      .filter((l) => /^\t/.test(l) || /^\s{2,}\S/.test(l))
      .filter((l) => !/\(use /.test(l));
    counts.untracked = Math.max(counts.untracked, lines.length);
  }

  if (ultra) {
    const parts: string[] = [];
    if (counts.modified) parts.push(`${counts.modified}M`);
    if (counts['new file']) parts.push(`${counts['new file']}N`);
    if (counts.deleted) parts.push(`${counts.deleted}D`);
    if (counts.renamed) parts.push(`${counts.renamed}R`);
    if (counts.untracked) parts.push(`${counts.untracked}U`);
    return parts.length ? parts.join(' ') : 'clean';
  }

  const parts: string[] = [];
  if (counts.modified) parts.push(`${counts.modified} modified`);
  if (counts['new file']) parts.push(`${counts['new file']} new file`);
  if (counts.deleted) parts.push(`${counts.deleted} deleted`);
  if (counts.renamed) parts.push(`${counts.renamed} renamed`);
  if (counts.untracked) parts.push(`${counts.untracked} untracked`);
  return parts.length ? parts.join(', ') : 'nothing to commit, working tree clean';
}

function compactDiff(raw: string, ultra = false): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n');
  let files = 0;
  let added = 0;
  let removed = 0;
  for (const line of lines) {
    if (line.startsWith('+++ ')) files++;
    else if (line.startsWith('+') && !line.startsWith('+++')) added++;
    else if (line.startsWith('-') && !line.startsWith('---')) removed++;
  }
  if (files === 0 && added === 0 && removed === 0) {
    return ultra ? '0' : 'no changes';
  }
  if (ultra) return `+${added}-${removed}/${files}f`;
  return `${files} file${files === 1 ? '' : 's'}: +${added}/-${removed}`;
}

function formatLog(raw: string, ultra = false): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n');
  const out: string[] = [];
  let current: { hash?: string; subject?: string; date?: string } = {};

  for (const line of lines) {
    const m = /^commit\s+([0-9a-f]+)/.exec(line);
    if (m) {
      if (current.hash) out.push(formatLogEntry(current, ultra));
      current = { hash: m[1].slice(0, 7) };
      continue;
    }
    const dm = /^Date:\s+(.+)/.exec(line);
    if (dm) {
      current.date = dm[1].trim();
      continue;
    }
    if (current.hash && !current.subject) {
      const subject = line.trim();
      if (subject && !/^Author:|^Date:|^Merge:/.test(line)) {
        current.subject = subject;
      }
    }
  }
  if (current.hash) out.push(formatLogEntry(current, ultra));

  if (out.length === 0) {
    // Maybe oneline format
    for (const line of lines) {
      const m = /^([0-9a-f]{7,40})\s+(.+)/.exec(line.trim());
      if (m) {
        out.push(`${m[1].slice(0, 7)} ${m[2]}`);
      }
    }
  }
  return out.join('\n');
}

function formatLogEntry(
  e: { hash?: string; subject?: string; date?: string },
  ultra: boolean,
): string {
  const subject = e.subject || '';
  if (ultra) return `${e.hash} ${subject}`;
  const rel = e.date ? ` (${shortRelative(e.date)})` : '';
  return `${e.hash} ${subject}${rel}`;
}

function shortRelative(iso: string): string {
  const t = new Date(iso).getTime();
  if (isNaN(t)) return iso;
  const ms = Date.now() - t;
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const d = Math.floor(hr / 24);
  if (d < 30) return `${d}d ago`;
  const mo = Math.floor(d / 30);
  if (mo < 12) return `${mo}mo ago`;
  return `${Math.floor(mo / 12)}y ago`;
}

function compactPush(raw: string): string {
  const clean = stripAnsi(raw);
  const m = /\b([\w./-]+)\s*->\s*([\w./-]+)/.exec(clean);
  if (m) return `ok ${m[1]}→${m[2]}`;
  if (/up-to-date|Everything up-to-date/i.test(clean)) return 'ok: up-to-date';
  return 'ok';
}

function compactPull(raw: string): string {
  const clean = stripAnsi(raw);
  if (/Already up.to.date/i.test(clean)) return 'ok: up-to-date';
  const stat = /(\d+)\s+files?\s+changed.*?(\d+)\s+insertions?.*?(\d+)\s+deletions?/.exec(clean);
  const commits = /Fast-forward|Merging|(\d+)\s+commit/.exec(clean);
  const cm = /(\d+)\s+commit/.exec(clean);
  if (stat) {
    const n = cm ? cm[1] : '?';
    return `ok: ${n} commits +${stat[2]}-${stat[3]}`;
  }
  if (commits) return 'ok: pulled';
  return 'ok';
}

function compactCommit(raw: string, ultra: boolean): string {
  const clean = stripAnsi(raw);
  const m = /\[\S+\s+([0-9a-f]+)\]\s*(.*)/.exec(clean);
  if (m) {
    if (ultra) return `✓ ${m[1].slice(0, 7)}`;
    return `ok ${m[1].slice(0, 7)}: ${m[2]}`;
  }
  if (/nothing to commit/i.test(clean)) return ultra ? 'clean' : 'nothing to commit';
  return ultra ? '✓' : 'ok';
}

function compactBranch(raw: string, ultra: boolean): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n').filter((l) => l.trim() !== '');
  let current = '';
  const others: string[] = [];
  for (const line of lines) {
    const m = /^\*\s+(.+)/.exec(line);
    if (m) {
      current = m[1].trim();
      continue;
    }
    const t = line.trim();
    if (t) others.push(t);
  }
  if (ultra) return `*${current} (${lines.length}b)`;
  return `current: ${current} | ${lines.length} branches total`;
}
