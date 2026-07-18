import * as crypto from 'crypto';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

export const TOK_VERSION = '0.3.0';

// Short, stable content hash used by the output cache to detect unchanged results.
export function shortHash(input: string): string {
  return crypto.createHash('sha1').update(input).digest('hex').slice(0, 16);
}

export function stripAnsi(raw: string): string {
  return raw
    .replace(/\x1b\[[0-9;]*[mGKHF]/g, '')
    .replace(/\r[^\n]*/g, '')
    .replace(/[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]/g, '');
}

export function truncate(text: string, maxLines: number): string {
  const lines = text.split('\n');
  if (lines.length <= maxLines) return text;
  const kept = lines.slice(0, maxLines).join('\n');
  return `${kept}\n[+${lines.length - maxLines} more lines]`;
}

export function estimateTokens(text: string): number {
  return Math.floor(text.length / 4);
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

export function formatNumber(n: number): string {
  return n.toLocaleString('en-US');
}

// Quote a CSV field only when it holds a comma, quote, or newline.
export function escapeCsv(s: string): string {
  if (/[",\n]/.test(s)) return `"${s.replace(/"/g, '""')}"`;
  return s;
}

// Single tok home directory under the user profile (~/.tok), shared by data + config.
//
// On Windows this deliberately avoids %LOCALAPPDATA% / %APPDATA%. MSIX/Store-packaged
// apps - Claude Desktop is one - run sandboxed, and Windows transparently redirects
// those two folders into the package's private ...\Packages\<app>\LocalCache\. The hook
// (spawned by Claude, sandboxed) would then write savings to that private folder while
// `tok gain` in your normal terminal reads the real one - so your terminal shows 0 even
// though tok is saving. os.homedir() (USERPROFILE) is NOT redirected, so ~/.tok is the
// same directory in both contexts. Override with TOK_HOME if you need a custom location.
export function tokHome(): string {
  return process.env.TOK_HOME || path.join(os.homedir(), '.tok');
}

export function dataDir(): string {
  return tokHome();
}

export function configDir(): string {
  return tokHome();
}

export function ensureDir(dirPath: string): void {
  try {
    fs.mkdirSync(dirPath, { recursive: true });
  } catch {
    // ignore
  }
}

export function appendErrorLog(scope: string, err: Error | unknown): void {
  try {
    ensureDir(dataDir());
    const logPath = path.join(dataDir(), 'errors.log');
    const ts = new Date().toISOString();
    const message = err instanceof Error ? `${err.message}\n${err.stack || ''}` : String(err);
    fs.appendFileSync(logPath, `[${ts}] [${scope}] ${message}\n`, { flag: 'a' });
  } catch {
    // never crash on log failure
  }
}

export function nowIso(): string {
  return new Date().toISOString();
}

export function relativeTime(iso: string): string {
  const past = new Date(iso).getTime();
  if (isNaN(past)) return iso;
  const diffMs = Date.now() - past;
  const sec = Math.floor(diffMs / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.floor(hr / 24);
  if (day < 30) return `${day}d ago`;
  const mo = Math.floor(day / 30);
  if (mo < 12) return `${mo}mo ago`;
  return `${Math.floor(mo / 12)}y ago`;
}

export function pad(s: string, width: number, alignRight = false): string {
  if (s.length >= width) return s;
  const spaces = ' '.repeat(width - s.length);
  return alignRight ? spaces + s : s + spaces;
}

export function fileExistsSync(p: string): boolean {
  try {
    fs.accessSync(p);
    return true;
  } catch {
    return false;
  }
}

export function safeJsonParse<T = unknown>(s: string): T | null {
  try {
    return JSON.parse(s) as T;
  } catch {
    return null;
  }
}

export function readFileIfExists(p: string): string | null {
  try {
    return fs.readFileSync(p, 'utf8');
  } catch {
    return null;
  }
}

export function writeFileSafe(p: string, content: string): boolean {
  try {
    ensureDir(path.dirname(p));
    fs.writeFileSync(p, content);
    return true;
  } catch (err) {
    appendErrorLog('writeFile', err);
    return false;
  }
}

export function chmodIfPosix(p: string, mode: number): void {
  if (process.platform === 'win32') return;
  try {
    fs.chmodSync(p, mode);
  } catch {
    // ignore
  }
}

export function unique<T>(arr: T[]): T[] {
  return Array.from(new Set(arr));
}

// True when the ISO timestamp falls within the last `days` - a rolling 24h×days
// window (not calendar days), used by the analytics for "today / 7d / 30d".
export function withinDays(ts: string, days: number): boolean {
  const t = new Date(ts).getTime();
  if (isNaN(t)) return false;
  return t >= Date.now() - days * 24 * 3600 * 1000;
}

// Bytes → estimated tokens (~4 bytes/token), without allocating a filler string.
export function bytesToTokens(bytes: number): number {
  return Math.floor(Math.max(0, bytes) / 4);
}

export function isoDay(d: Date | string): string {
  const date = typeof d === 'string' ? new Date(d) : d;
  return date.toISOString().slice(0, 10);
}

export function isoWeek(d: Date | string): string {
  const date = typeof d === 'string' ? new Date(d) : d;
  const target = new Date(date.valueOf());
  const dayNum = (target.getUTCDay() + 6) % 7;
  target.setUTCDate(target.getUTCDate() - dayNum + 3);
  const firstThursday = target.valueOf();
  target.setUTCMonth(0, 1);
  if (target.getUTCDay() !== 4) {
    target.setUTCMonth(0, 1 + ((4 - target.getUTCDay()) + 7) % 7);
  }
  const week = 1 + Math.ceil((firstThursday - target.valueOf()) / (7 * 24 * 3600 * 1000));
  const year = new Date(date.valueOf()).getUTCFullYear();
  return `${year}-W${String(week).padStart(2, '0')}`;
}

export function isoMonth(d: Date | string): string {
  const date = typeof d === 'string' ? new Date(d) : d;
  return date.toISOString().slice(0, 7);
}

export function dollar(n: number): string {
  if (n === 0) return '$0.00';
  if (Math.abs(n) < 0.01) return `$${n.toFixed(4)}`;
  return `$${n.toFixed(2)}`;
}

export function percent(n: number, digits = 1): string {
  return `${n.toFixed(digits)}%`;
}
