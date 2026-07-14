import { stripAnsi } from './utils';

export enum FilterLevel {
  None = 'none',
  Minimal = 'minimal',
  Aggressive = 'aggressive',
}

const LANG_BY_EXT: Record<string, string> = {
  ts: 'ts', tsx: 'tsx',
  js: 'js', jsx: 'jsx', mjs: 'js', cjs: 'js',
  py: 'py',
  go: 'go',
  rs: 'rs',
  java: 'java',
  cs: 'cs',
  cpp: 'cpp', cc: 'cpp', cxx: 'cpp', hpp: 'cpp', hxx: 'cpp',
  c: 'c', h: 'c',
  rb: 'rb',
};

export function langFromPath(p: string): string {
  const ext = p.split('.').pop()?.toLowerCase() || '';
  return LANG_BY_EXT[ext] || '';
}

function stripBlockComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, '');
}

function stripLineComments(src: string, prefix: string): string {
  return src
    .split('\n')
    .map((line) => {
      const idx = findCommentIdx(line, prefix);
      return idx >= 0 ? line.slice(0, idx).replace(/\s+$/, '') : line;
    })
    .join('\n');
}

function findCommentIdx(line: string, prefix: string): number {
  let inSingle = false;
  let inDouble = false;
  let inBacktick = false;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    const prev = i > 0 ? line[i - 1] : '';
    if (c === "'" && prev !== '\\' && !inDouble && !inBacktick) inSingle = !inSingle;
    else if (c === '"' && prev !== '\\' && !inSingle && !inBacktick) inDouble = !inDouble;
    else if (c === '`' && prev !== '\\' && !inSingle && !inDouble) inBacktick = !inBacktick;
    else if (!inSingle && !inDouble && !inBacktick) {
      if (line.startsWith(prefix, i)) return i;
    }
  }
  return -1;
}

function collapseBlanks(src: string): string {
  return src.replace(/\n{3,}/g, '\n\n');
}

const C_LIKE = new Set(['ts', 'tsx', 'js', 'jsx', 'go', 'rs', 'java', 'cs', 'cpp', 'c']);
const HASH_LANG = new Set(['py', 'rb']);

export function filterCode(source: string, lang: string, level: FilterLevel): string {
  if (level === FilterLevel.None) return source;

  let out = source;
  if (C_LIKE.has(lang)) {
    out = stripBlockComments(out);
    out = stripLineComments(out, '//');
  } else if (HASH_LANG.has(lang)) {
    out = stripLineComments(out, '#');
  } else {
    out = stripBlockComments(out);
    out = stripLineComments(out, '//');
    out = stripLineComments(out, '#');
  }

  out = collapseBlanks(out);

  if (level === FilterLevel.Minimal) return out;

  return aggressiveFilter(out, lang);
}

function aggressiveFilter(src: string, lang: string): string {
  if (C_LIKE.has(lang)) return aggressiveCLike(src);
  if (lang === 'py') return aggressivePython(src);
  if (lang === 'rb') return aggressiveRuby(src);
  return src;
}

function aggressiveCLike(src: string): string {
  const out: string[] = [];
  const lines = src.split('\n');
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const trimmed = line.trim();
    const keepers = /^(import\b|export\b|from\b|const\b|let\b|var\b|type\b|interface\b|enum\b|namespace\b|declare\b|use\b|using\b|package\b)/;
    const sigStart = /^(export\s+)?(default\s+)?(async\s+)?(function|class|interface|type|enum)\b/;
    const methodSig = /^[a-zA-Z_$][\w$]*\s*\([^)]*\)\s*(:\s*[^{]+)?\s*\{?\s*$/;
    const arrowFn = /=>\s*\{?\s*$/;

    if (sigStart.test(trimmed) && trimmed.includes('{')) {
      const end = findMatchingBrace(lines, i, line.indexOf('{'));
      const sig = line.replace(/\{[^]*$/, '{ ... }');
      out.push(sig);
      i = end + 1;
      continue;
    }

    if (sigStart.test(trimmed) && !trimmed.includes('{')) {
      out.push(line);
      i++;
      while (i < lines.length && !lines[i].includes('{') && !lines[i].endsWith(';')) {
        out.push(lines[i]);
        i++;
      }
      if (i < lines.length && lines[i].includes('{')) {
        const end = findMatchingBrace(lines, i, lines[i].indexOf('{'));
        out.push(lines[i].replace(/\{[^]*$/, '{ ... }'));
        i = end + 1;
        continue;
      }
      continue;
    }

    if (methodSig.test(trimmed) && trimmed.includes('{')) {
      const end = findMatchingBrace(lines, i, line.indexOf('{'));
      out.push(line.replace(/\{[^]*$/, '{ ... }'));
      i = end + 1;
      continue;
    }

    if (arrowFn.test(trimmed) && trimmed.includes('{')) {
      const end = findMatchingBrace(lines, i, line.lastIndexOf('{'));
      out.push(line.replace(/\{[^]*$/, '{ ... }'));
      i = end + 1;
      continue;
    }

    if (keepers.test(trimmed)) {
      out.push(line);
      i++;
      continue;
    }

    if (trimmed === '' || /^[}\])]+;?\s*$/.test(trimmed)) {
      out.push(line);
      i++;
      continue;
    }

    i++;
  }
  return collapseBlanks(out.join('\n'));
}

function findMatchingBrace(lines: string[], startLine: number, startCol: number): number {
  let depth = 0;
  let started = false;
  for (let li = startLine; li < lines.length; li++) {
    const line = lines[li];
    const from = li === startLine ? startCol : 0;
    for (let ci = from; ci < line.length; ci++) {
      const c = line[ci];
      if (c === '{') {
        depth++;
        started = true;
      } else if (c === '}') {
        depth--;
        if (started && depth === 0) return li;
      }
    }
  }
  return lines.length - 1;
}

function aggressivePython(src: string): string {
  const lines = src.split('\n');
  const out: string[] = [];
  const sig = /^(\s*)(async\s+)?(def|class)\s+/;
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const m = sig.exec(line);
    if (m) {
      const baseIndent = m[1].length;
      out.push(`${line.replace(/:\s*$/, ':')} ...`);
      i++;
      while (i < lines.length) {
        const next = lines[i];
        const trimmed = next.trim();
        if (trimmed === '') { i++; continue; }
        const indent = next.search(/\S/);
        if (indent <= baseIndent) break;
        i++;
      }
      continue;
    }
    if (/^(import |from )/.test(line) || /^[A-Z_]+\s*=/.test(line.trim())) {
      out.push(line);
    } else if (line.trim() === '') {
      out.push(line);
    }
    i++;
  }
  return collapseBlanks(out.join('\n'));
}

function aggressiveRuby(src: string): string {
  const lines = src.split('\n');
  const out: string[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const trimmed = line.trim();
    if (/^(def |class |module )/.test(trimmed)) {
      out.push(line);
      i++;
      while (i < lines.length && !/^\s*end\b/.test(lines[i])) {
        i++;
      }
      if (i < lines.length) {
        out.push(lines[i]);
        i++;
      }
      continue;
    }
    if (/^(require|include|extend|attr_)/.test(trimmed) || trimmed === '') {
      out.push(line);
    }
    i++;
  }
  return collapseBlanks(out.join('\n'));
}

export function smartSummary(source: string, lang: string): string {
  const clean = stripAnsi(source);
  const lines = clean.split('\n');
  const lineCount = lines.length;

  if (lang === 'tsx' || lang === 'jsx') {
    const props = countMatches(clean, /interface\s+\w*Props|type\s+\w*Props\s*=|\bprops\.\w+/g);
    const hooks = countMatches(clean, /\buse[A-Z]\w+\s*\(/g);
    const isComponent = /export\s+(default\s+)?(function|const)\s+[A-Z]/.test(clean) ||
      /\breturn\s*\(\s*</.test(clean);
    const renders = isComponent ? 'a component' : 'JSX content';
    return `React component: ${props} props, ${hooks} hooks, renders ${renders}\n${lineCount} lines total`;
  }

  if (lang === 'ts' || lang === 'js') {
    const classes = countMatches(clean, /\bclass\s+\w+/g);
    const interfaces = countMatches(clean, /\binterface\s+\w+/g);
    const exports = countMatches(clean, /\bexport\s+(default\s+|const\s+|function\s+|class\s+|interface\s+|type\s+|enum\s+|let\s+|var\s+)/g);
    const functions = countMatches(clean, /\bfunction\s+\w+|\bconst\s+\w+\s*=\s*(async\s+)?\(/g);
    const asyncFns = countMatches(clean, /\basync\s+(function|\()/g);
    const imports = countMatches(clean, /^import\s+/gm);
    if (classes > 0) {
      return `TypeScript class: ${classes} classes, ${interfaces} interfaces, ${imports} imports\n${functions} functions (${asyncFns} async), ${lineCount} lines`;
    }
    return `Node module: ${exports} exports, ${asyncFns} async functions, ${imports} imports\n${functions} functions, ${lineCount} lines`;
  }

  if (lang === 'py') {
    const classes = countMatches(clean, /^\s*class\s+\w+/gm);
    const defs = countMatches(clean, /^\s*(async\s+)?def\s+\w+/gm);
    const imports = countMatches(clean, /^(import\s+|from\s+\w)/gm);
    return `Python module: ${classes} classes, ${defs} functions, ${imports} imports\n${lineCount} lines total`;
  }

  if (lang === 'go') {
    const funcs = countMatches(clean, /^func\s+/gm);
    const types = countMatches(clean, /^type\s+\w+/gm);
    const imports = countMatches(clean, /^import\s+/gm);
    return `Go file: ${funcs} functions, ${types} types, ${imports} import blocks\n${lineCount} lines total`;
  }

  return `${lang || 'text'} file: ${lineCount} lines, ${clean.length} bytes`;
}

function countMatches(s: string, re: RegExp): number {
  const m = s.match(re);
  return m ? m.length : 0;
}

export function deduplicateLines(raw: string): string {
  const lines = stripAnsi(raw).split('\n');
  const order: string[] = [];
  const counts = new Map<string, { display: string; count: number }>();

  for (const line of lines) {
    const normalized = line
      .replace(/\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?/g, '<TS>')
      .replace(/\b[0-9a-f]{8,}\b/gi, '<ID>')
      .trim();
    if (normalized === '') continue;
    const existing = counts.get(normalized);
    if (existing) {
      existing.count++;
    } else {
      counts.set(normalized, { display: line, count: 1 });
      order.push(normalized);
    }
  }

  const sorted = order
    .map((k) => counts.get(k)!)
    .sort((a, b) => b.count - a.count);

  return sorted
    .map((entry) => (entry.count >= 2 ? `${entry.display} (×${entry.count})` : entry.display))
    .join('\n');
}

export function extractStructure(value: unknown, depth = 0): unknown {
  if (depth > 6) return '<...>';
  if (value === null) return 'null';
  if (Array.isArray(value)) {
    if (value.length === 0) return [];
    return [extractStructure(value[0], depth + 1)];
  }
  const t = typeof value;
  if (t === 'object') {
    const obj: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      obj[k] = extractStructure(v, depth + 1);
    }
    return obj;
  }
  return t;
}
