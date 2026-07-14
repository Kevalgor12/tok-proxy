import * as fs from 'fs';
import * as path from 'path';
import { dataDir, ensureDir, nowIso, TOK_VERSION, appendErrorLog } from './utils';

// Zero-dependency local store. No native modules, no build step — just files under
// the OS data dir, so `npm install` can never fail on a missing compiler:
//   commands.ndjson   append-only log of filtered-command savings (one JSON per line)
//   ai_usage.ndjson   append-only log of ingested AI token usage
//   meta.json         small key/value bag (versions, timestamps)
//   cache.json        output-cache index (unchanged-detection metadata, no payloads)
//
// Event logs are append-only (fast hot path); analytics read them back and aggregate
// in JS. meta/cache are tiny and rewritten atomically (temp file + rename).

export interface CommandRow {
  timestamp: string;
  cmd_type: string;
  input_bytes: number;
  out_bytes: number;
  saved_bytes: number;
  savings_pct: number;
  exec_ms: number;
}

export interface AIUsageRecord {
  timestamp: string;
  session_id: string;
  model: string;
  source: string;
  input_tokens: number;
  output_tokens: number;
  cache_write_tokens: number;
  cache_read_tokens: number;
  cost_usd: number;
}

export interface CacheRow {
  cache_key: string;
  cmd_type: string;
  output_hash: string;
  filtered_bytes: number;
  hit_count: number;
  first_seen: string;
  last_seen: string;
}

class TokStore {
  readonly dir: string;
  private readonly commandsFile: string;
  private readonly aiUsageFile: string;
  private readonly metaFile: string;
  private readonly cacheFile: string;

  private meta: Record<string, string> | null = null;
  private cache: Record<string, CacheRow> | null = null;
  private aiKeys: Set<string> | null = null;

  constructor() {
    this.dir = dataDir();
    ensureDir(this.dir);
    this.commandsFile = path.join(this.dir, 'commands.ndjson');
    this.aiUsageFile = path.join(this.dir, 'ai_usage.ndjson');
    this.metaFile = path.join(this.dir, 'meta.json');
    this.cacheFile = path.join(this.dir, 'cache.json');
  }

  // ---- NDJSON event logs -------------------------------------------------

  private readNdjson<T>(file: string): T[] {
    let raw: string;
    try {
      raw = fs.readFileSync(file, 'utf8');
    } catch {
      return []; // missing file = empty log
    }
    const out: T[] = [];
    for (const line of raw.split('\n')) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        out.push(JSON.parse(trimmed) as T);
      } catch {
        // skip a torn/partial line rather than throwing
      }
    }
    return out;
  }

  private appendNdjson(file: string, row: unknown): void {
    try {
      fs.appendFileSync(file, JSON.stringify(row) + '\n');
    } catch (err) {
      appendErrorLog('store.append', err);
    }
  }

  readCommands(): CommandRow[] {
    return this.readNdjson<CommandRow>(this.commandsFile);
  }

  readAIUsage(): AIUsageRecord[] {
    return this.readNdjson<AIUsageRecord>(this.aiUsageFile);
  }

  appendCommand(row: CommandRow): void {
    this.appendNdjson(this.commandsFile, row);
  }

  // Dedup on (timestamp, source, model) so repeated ingests don't double-count.
  // Returns true when the row was newly written.
  appendAIUsage(row: AIUsageRecord): boolean {
    try {
      if (this.aiKeys === null) {
        this.aiKeys = new Set(this.readAIUsage().map(aiKey));
      }
      const key = aiKey(row);
      if (this.aiKeys.has(key)) return false;
      this.aiKeys.add(key);
      this.appendNdjson(this.aiUsageFile, row);
      return true;
    } catch (err) {
      appendErrorLog('store.appendAIUsage', err);
      return false;
    }
  }

  private countLines(file: string): number {
    try {
      const raw = fs.readFileSync(file, 'utf8');
      let n = 0;
      for (const line of raw.split('\n')) if (line.trim()) n++;
      return n;
    } catch {
      return 0;
    }
  }

  rowCounts(): { commands: number; aiUsage: number } {
    return { commands: this.countLines(this.commandsFile), aiUsage: this.countLines(this.aiUsageFile) };
  }

  // ---- meta (tiny key/value) --------------------------------------------

  private loadMeta(): Record<string, string> {
    if (this.meta === null) this.meta = readJson<Record<string, string>>(this.metaFile) || {};
    return this.meta;
  }

  getMeta(key: string): string | undefined {
    return this.loadMeta()[key];
  }

  setMeta(key: string, value: string): void {
    const m = this.loadMeta();
    m[key] = value;
    writeJsonAtomic(this.metaFile, m);
  }

  // ---- output cache index -----------------------------------------------

  private loadCache(): Record<string, CacheRow> {
    if (this.cache === null) this.cache = readJson<Record<string, CacheRow>>(this.cacheFile) || {};
    return this.cache;
  }

  private saveCache(): void {
    if (this.cache) writeJsonAtomic(this.cacheFile, this.cache);
  }

  getCacheEntry(key: string): CacheRow | undefined {
    return this.loadCache()[key];
  }

  upsertCacheEntry(row: CacheRow): void {
    this.loadCache()[row.cache_key] = row;
    this.saveCache();
  }

  bumpCacheHit(key: string, lastSeen: string): void {
    const c = this.loadCache();
    const e = c[key];
    if (!e) return;
    e.hit_count += 1;
    e.last_seen = lastSeen;
    this.saveCache();
  }

  cacheStats(): { entries: number; hits: number; savedBytes: number } {
    const c = this.loadCache();
    let entries = 0;
    let hits = 0;
    let savedBytes = 0;
    for (const e of Object.values(c)) {
      entries++;
      hits += e.hit_count;
      savedBytes += e.hit_count * e.filtered_bytes;
    }
    return { entries, hits, savedBytes };
  }

  topCacheEntries(limit: number): CacheRow[] {
    return Object.values(this.loadCache())
      .sort((a, b) => b.hit_count - a.hit_count || (a.last_seen < b.last_seen ? 1 : -1))
      .slice(0, limit);
  }

  clearCache(): number {
    const c = this.loadCache();
    const n = Object.keys(c).length;
    this.cache = {};
    this.saveCache();
    return n;
  }

  // Bound the cache: drop least-recently-seen entries past maxEntries.
  pruneCache(maxEntries: number): void {
    const c = this.loadCache();
    const keys = Object.keys(c);
    if (keys.length <= maxEntries) return;
    const byOldest = keys.sort((a, b) => (c[a].last_seen < c[b].last_seen ? -1 : 1));
    for (const k of byOldest.slice(0, keys.length - maxEntries)) delete c[k];
    this.saveCache();
  }
}

function aiKey(r: AIUsageRecord): string {
  return `${r.timestamp}|${r.source}|${r.model}`;
}

function readJson<T>(file: string): T | null {
  try {
    return JSON.parse(fs.readFileSync(file, 'utf8')) as T;
  } catch {
    return null;
  }
}

function writeJsonAtomic(file: string, obj: unknown): void {
  // Write to a per-process temp file then rename over the target. rename is atomic
  // on POSIX and replaces the destination on Windows (libuv passes REPLACE_EXISTING),
  // so a reader never sees a half-written file. The pid suffix keeps concurrent tok
  // processes from clobbering each other's temp file.
  const tmp = `${file}.${process.pid}.tmp`;
  try {
    fs.writeFileSync(tmp, JSON.stringify(obj));
    fs.renameSync(tmp, file);
  } catch (err) {
    appendErrorLog('store.write', err);
    try { fs.unlinkSync(tmp); } catch { /* nothing to clean up */ }
  }
}

export type DB = TokStore;

let cached: TokStore | null = null;

export function openDb(): DB {
  if (cached) return cached;
  const store = new TokStore();
  store.setMeta('tok_version', TOK_VERSION);
  if (!store.getMeta('install_at')) store.setMeta('install_at', nowIso());
  cached = store;
  return store;
}

// Path to the data directory — reported by `tok doctor`.
export function dbPath(): string {
  return dataDir();
}

// ---- Thin functional wrappers (preserve existing call sites) -------------

export function getMeta(db: DB, key: string): string | undefined {
  return db.getMeta(key);
}

export function setMeta(db: DB, key: string, value: string): void {
  db.setMeta(key, value);
}

export function recordCommand(db: DB, row: CommandRow): void {
  db.appendCommand(row);
}

export function recordAIUsage(db: DB, row: AIUsageRecord): boolean {
  return db.appendAIUsage(row);
}

export function readCommands(db: DB): CommandRow[] {
  return db.readCommands();
}

export function readAIUsage(db: DB): AIUsageRecord[] {
  return db.readAIUsage();
}

export function rowCounts(db: DB): { commands: number; aiUsage: number } {
  return db.rowCounts();
}

export function getCacheEntry(db: DB, key: string): CacheRow | undefined {
  return db.getCacheEntry(key);
}

export function upsertCacheEntry(db: DB, row: CacheRow): void {
  db.upsertCacheEntry(row);
}

export function bumpCacheHit(db: DB, key: string, lastSeen: string): void {
  db.bumpCacheHit(key, lastSeen);
}

export function cacheStats(db: DB): { entries: number; hits: number; savedBytes: number } {
  return db.cacheStats();
}

export function topCacheEntries(db: DB, limit: number): CacheRow[] {
  return db.topCacheEntries(limit);
}

export function clearCache(db: DB): number {
  return db.clearCache();
}

export function pruneCache(db: DB, maxEntries: number): void {
  db.pruneCache(maxEntries);
}
