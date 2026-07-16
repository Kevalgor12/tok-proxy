import * as fs from 'fs';
import * as path from 'path';
import { run } from '../core/runner';
import { stripAnsi, truncate, readFileIfExists, safeJsonParse } from '../core/utils';
import { TokConfig } from '../core/config';
import { FilterLevel, filterCode, langFromPath, smartSummary, extractStructure } from '../core/filter';
import { HandlerResult } from './git';

export function handleLs(args: string[], ultra: boolean, config: TokConfig): HandlerResult {
  const target = args.find((a) => !a.startsWith('-')) || '.';
  const start = Date.now();
  let filtered = '';
  let raw = '';
  let exitCode = 0;
  try {
    raw = readDirRaw(target);
    filtered = formatTree(target, ultra, config);
  } catch (err) {
    raw = String(err);
    filtered = raw;
    exitCode = 1;
  }
  return {
    filteredOutput: filtered,
    exitCode,
    rawOutput: raw,
    cmdType: 'ls',
    execMs: Date.now() - start,
  };
}

function readDirRaw(target: string): string {
  try {
    const entries = fs.readdirSync(target);
    return entries.join('\n');
  } catch (err) {
    return String(err);
  }
}

export function formatTree(dirPath: string, ultra: boolean, config: TokConfig): string {
  const noise = new Set(config.noiseDirectories);
  const maxDepth = config.filters.ls.maxDepth;

  let totalFiles = 0;
  let totalDirs = 0;

  type Node = { name: string; isDir: boolean; children: Node[]; fileCount: number };
  function walk(p: string, depth: number): Node {
    const node: Node = {
      name: path.basename(p) || p,
      isDir: true,
      children: [],
      fileCount: 0,
    };
    if (depth > maxDepth) return node;
    let entries: fs.Dirent[];
    try {
      entries = fs.readdirSync(p, { withFileTypes: true });
    } catch {
      return node;
    }
    const files = entries.filter((e) => e.isFile());
    const dirs = entries.filter((e) => e.isDirectory() && !noise.has(e.name));
    node.fileCount = files.length;

    for (const f of files) totalFiles++;
    for (const d of dirs) {
      totalDirs++;
      node.children.push(walk(path.join(p, d.name), depth + 1));
    }
    if (files.length <= 5) {
      for (const f of files) {
        node.children.push({ name: f.name, isDir: false, children: [], fileCount: 0 });
      }
    }
    return node;
  }

  const root = walk(dirPath, 0);

  if (ultra) {
    const items: string[] = [];
    let extra = 0;
    for (const c of root.children) {
      if (c.isDir) {
        if (items.length < 6) items.push(`${c.name}/${c.fileCount}`);
        else extra++;
      } else if (items.length < 6) {
        items.push(c.name);
      }
    }
    if (extra > 0) items.push(`[+${extra} dirs]`);
    return items.join(' ');
  }

  const lines: string[] = [];
  lines.push(`${root.name === '.' ? path.resolve(dirPath) : root.name}/`);
  renderTree(root, '', lines);
  lines.push('');
  lines.push(`Total: ${totalFiles} files in ${totalDirs} directories`);
  return lines.join('\n');
}

function renderTree(node: { name: string; isDir: boolean; children: { name: string; isDir: boolean; children: any[]; fileCount: number }[]; fileCount: number }, prefix: string, lines: string[]): void {
  for (let i = 0; i < node.children.length; i++) {
    const child = node.children[i];
    const last = i === node.children.length - 1;
    const branch = last ? '└─' : '├─';
    const nextPrefix = prefix + (last ? '   ' : '│  ');
    if (child.isDir) {
      if (child.fileCount > 5) {
        lines.push(`${prefix}${branch} ${child.name}/ (${child.fileCount} files)`);
      } else {
        lines.push(`${prefix}${branch} ${child.name}/`);
        renderTree(child as any, nextPrefix, lines);
      }
    } else {
      lines.push(`${prefix}${branch} ${child.name}`);
    }
  }
}

export function handleCat(args: string[], ultra: boolean, config: TokConfig): HandlerResult {
  const file = args.find((a) => !a.startsWith('-'));
  const start = Date.now();
  if (!file) {
    return {
      filteredOutput: 'usage: tok cat <file>',
      exitCode: 2,
      rawOutput: '',
      cmdType: 'cat',
      execMs: Date.now() - start,
    };
  }
  const raw = readFileIfExists(file) || '';
  if (!raw) {
    return {
      filteredOutput: `cannot read: ${file}`,
      exitCode: 1,
      rawOutput: '',
      cmdType: 'cat',
      execMs: Date.now() - start,
    };
  }
  const lang = langFromPath(file);
  const level = pickLevel(args, config.filters.cat.defaultLevel, ultra);
  let filtered = filterCode(raw, lang, level);
  filtered = truncate(filtered, config.filters.cat.maxLines);
  return {
    filteredOutput: filtered,
    exitCode: 0,
    rawOutput: raw,
    cmdType: 'cat',
    execMs: Date.now() - start,
  };
}

function pickLevel(args: string[], def: FilterLevel, ultra: boolean): FilterLevel {
  if (ultra) return FilterLevel.Aggressive;
  if (args.includes('--aggressive')) return FilterLevel.Aggressive;
  if (args.includes('--minimal')) return FilterLevel.Minimal;
  if (args.includes('--none')) return FilterLevel.None;
  return def;
}

export function handleSmart(args: string[], _ultra: boolean): HandlerResult {
  const file = args.find((a) => !a.startsWith('-'));
  const start = Date.now();
  if (!file) {
    return {
      filteredOutput: 'usage: tok smart <file>',
      exitCode: 2,
      rawOutput: '',
      cmdType: 'smart',
      execMs: Date.now() - start,
    };
  }
  const raw = readFileIfExists(file) || '';
  if (!raw) {
    return {
      filteredOutput: `cannot read: ${file}`,
      exitCode: 1,
      rawOutput: '',
      cmdType: 'smart',
      execMs: Date.now() - start,
    };
  }
  const lang = langFromPath(file);
  return {
    filteredOutput: smartSummary(raw, lang),
    exitCode: 0,
    rawOutput: raw,
    cmdType: 'smart',
    execMs: Date.now() - start,
  };
}

export function handleGrep(args: string[], ultra: boolean, config: TokConfig): HandlerResult {
  const cmd = which('rg') ? 'rg' : 'grep';
  let useArgs = args.slice();

  // Detect single-file targets; force -H so grep prefixes the filename even with one file,
  // otherwise the grouping regex (which expects file:line:content) sees only line:content
  // and reports "0 matches".
  const positional = useArgs.filter((a) => !a.startsWith('-'));
  const looksLikeSingleFile = positional.length >= 2 && positional.slice(1).filter((p) => existsSyncSafe(p)).length === 1;

  if (cmd === 'grep') {
    if (!useArgs.includes('-rn') && !useArgs.includes('-n')) useArgs = ['-rn', ...useArgs];
    if (looksLikeSingleFile && !useArgs.includes('-H')) useArgs = ['-H', ...useArgs];
  } else if (cmd === 'rg') {
    // ripgrep already prefixes filenames per match by default — no change needed.
  }

  const result = run(cmd, useArgs);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  let filtered = '';
  try {
    filtered = groupByFile(raw, ultra, config.filters.grep.maxMatches);
  } catch {
    filtered = raw;
  }
  return {
    filteredOutput: filtered || (result.exitCode === 0 ? '0 matches' : raw),
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: 'grep',
    execMs: result.execMs,
  };
}

function existsSyncSafe(p: string): boolean {
  try { return fs.existsSync(p); } catch { return false; }
}

export function groupByFile(raw: string, ultra: boolean, maxMatches: number): string {
  const clean = stripAnsi(raw);
  const lines = clean.split('\n').filter((l) => l.trim());
  const byFile = new Map<string, number[]>();
  let total = 0;
  for (const line of lines) {
    const m = /^(.+?):(\d+):/.exec(line);
    if (!m) continue;
    total++;
    const arr = byFile.get(m[1]) || [];
    arr.push(parseInt(m[2], 10));
    byFile.set(m[1], arr);
  }
  if (total === 0) return ultra ? '0' : '0 matches';

  if (ultra) {
    const parts: string[] = [];
    for (const [file, ls] of byFile) {
      const short = file.split(/[\/\\]/).pop() || file;
      parts.push(`${short}:${ls.length}`);
    }
    return parts.slice(0, 8).join(' ');
  }

  const out: string[] = [];
  let shown = 0;
  for (const [file, ls] of byFile) {
    if (shown + ls.length > maxMatches) {
      out.push(`[+${total - shown} more matches]`);
      break;
    }
    const lines = ls.slice(0, 10).map((n) => `L${n}`).join(', ');
    const more = ls.length > 10 ? `, +${ls.length - 10} more` : '';
    out.push(`${file}: ${ls.length} match${ls.length === 1 ? '' : 'es'} (${lines}${more})`);
    shown += ls.length;
  }
  return out.join('\n');
}

function which(cmd: string): boolean {
  try {
    const result = run(process.platform === 'win32' ? 'where' : 'which', [cmd]);
    return result.exitCode === 0;
  } catch {
    return false;
  }
}

export function handleFind(args: string[], ultra: boolean): HandlerResult {
  const start = Date.now();

  // On Windows the system `find` is a substring search tool with completely different
  // arguments from unix find, and `where` only finds executables on PATH. Implement
  // our own portable walker so `tok find <dir> -name "*.ts"` works the same everywhere.
  let lines: string[];
  let exitCode = 0;
  let raw = '';
  if (process.platform === 'win32' || hasUnsupportedFindArg(args)) {
    const { dir, pattern } = parseFindArgs(args);
    try {
      lines = walkFiles(dir, pattern);
      raw = lines.join('\n');
    } catch (err) {
      lines = [];
      raw = String(err);
      exitCode = 1;
    }
  } else {
    const result = run('find', args);
    raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
    exitCode = result.exitCode;
    lines = stripAnsi(raw).split('\n').filter((l) => l.trim());
  }

  let filtered: string;
  if (ultra) {
    filtered = `${lines.length} files`;
  } else if (lines.length > 50) {
    filtered = `${lines.length} files found\n${lines.slice(0, 50).join('\n')}\n[+${lines.length - 50} more]`;
  } else {
    filtered = lines.join('\n') || '0 files';
  }
  return {
    filteredOutput: filtered,
    exitCode,
    rawOutput: raw,
    cmdType: 'find',
    execMs: Date.now() - start,
  };
}

function hasUnsupportedFindArg(args: string[]): boolean {
  // Args we know our portable walker doesn't implement; fall back to system find on POSIX.
  const supported = new Set(['-name', '-iname']);
  return args.some((a) => a.startsWith('-') && !supported.has(a));
}

function parseFindArgs(args: string[]): { dir: string; pattern: RegExp | null } {
  let dir = '.';
  let pattern: RegExp | null = null;
  let caseInsensitive = false;
  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (a === '-name' || a === '-iname') {
      caseInsensitive = a === '-iname';
      const glob = args[++i] || '';
      pattern = globToRegex(glob, caseInsensitive);
    } else if (!a.startsWith('-') && i === 0) {
      dir = a;
    } else if (!a.startsWith('-')) {
      // Additional positional — usually a path filter; ignore for portability.
    }
  }
  return { dir, pattern };
}

function globToRegex(glob: string, ci: boolean): RegExp {
  let re = '';
  for (const ch of glob) {
    if (ch === '*') re += '.*';
    else if (ch === '?') re += '.';
    else if ('.+^${}()|[]\\'.includes(ch)) re += '\\' + ch;
    else re += ch;
  }
  return new RegExp('^' + re + '$', ci ? 'i' : '');
}

function walkFiles(dir: string, pattern: RegExp | null): string[] {
  const results: string[] = [];
  const queue: string[] = [dir];
  while (queue.length) {
    const cur = queue.shift()!;
    let entries: fs.Dirent[];
    try { entries = fs.readdirSync(cur, { withFileTypes: true }); } catch { continue; }
    for (const e of entries) {
      const full = path.join(cur, e.name);
      if (e.isDirectory()) {
        if (e.name === 'node_modules' || e.name === '.git') continue;
        queue.push(full);
      } else if (e.isFile()) {
        if (!pattern || pattern.test(e.name)) results.push(full);
      }
    }
  }
  return results;
}

export function handleDiff(args: string[], ultra: boolean): HandlerResult {
  const result = run('diff', args);
  const raw = result.stdout + (result.stderr ? `\n${result.stderr}` : '');
  const clean = stripAnsi(raw);
  const lines = clean.split('\n');
  let added = 0;
  let removed = 0;
  for (const line of lines) {
    if (line.startsWith('>') || (line.startsWith('+') && !line.startsWith('+++'))) added++;
    else if (line.startsWith('<') || (line.startsWith('-') && !line.startsWith('---'))) removed++;
  }
  const filtered = ultra ? `+${added}-${removed}` : `+${added} -${removed} (${lines.length} diff lines)`;
  return {
    filteredOutput: filtered,
    exitCode: result.exitCode,
    rawOutput: raw,
    cmdType: 'diff',
    execMs: result.execMs,
  };
}

export function handleJson(args: string[], _ultra: boolean): HandlerResult {
  const file = args.find((a) => !a.startsWith('-'));
  const start = Date.now();
  if (!file) {
    return {
      filteredOutput: 'usage: tok json <file>',
      exitCode: 2,
      rawOutput: '',
      cmdType: 'json',
      execMs: Date.now() - start,
    };
  }
  const raw = readFileIfExists(file) || '';
  if (!raw) {
    return {
      filteredOutput: `cannot read: ${file}`,
      exitCode: 1,
      rawOutput: '',
      cmdType: 'json',
      execMs: Date.now() - start,
    };
  }
  const parsed = safeJsonParse(raw);
  if (parsed === null) {
    return {
      filteredOutput: `invalid JSON: ${file}`,
      exitCode: 1,
      rawOutput: raw,
      cmdType: 'json',
      execMs: Date.now() - start,
    };
  }
  const structure = extractStructure(parsed);
  return {
    filteredOutput: JSON.stringify(structure, null, 2),
    exitCode: 0,
    rawOutput: raw,
    cmdType: 'json',
    execMs: Date.now() - start,
  };
}
