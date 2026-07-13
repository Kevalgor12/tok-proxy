import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { configDir, ensureDir, readFileIfExists, safeJsonParse, TOK_VERSION } from './utils';
import { FilterLevel } from './filter';

export interface ModelPricing {
  inputPer1k: number;
  outputPer1k: number;
  cacheWritePer1k: number;
  cacheReadPer1k: number;
}

export interface TokConfig {
  version: string;
  tokenPricePer1k: number;

  tee: {
    enabled: boolean;
    mode: 'failures' | 'always' | 'never';
  };

  filters: {
    maxOutputLines: number;
    ultraCompact: boolean;
    git: { diffMaxLines: number };
    cat: { maxLines: number; defaultLevel: FilterLevel };
    grep: { maxMatches: number };
    ls: { maxDepth: number };
  };

  cache: {
    enabled: boolean;
    maxEntries: number;
    maxOutputBytes: number;
    commands: string[];
  };

  excludeCommands: string[];
  noiseDirectories: string[];
  claudeCodeDataDir: string;

  modelPricing: { [modelName: string]: ModelPricing };
}

export const DEFAULTS: TokConfig = {
  version: TOK_VERSION,
  tokenPricePer1k: 0.015,
  tee: { enabled: true, mode: 'failures' },
  filters: {
    maxOutputLines: 150,
    ultraCompact: false,
    git: { diffMaxLines: 100 },
    cat: { maxLines: 200, defaultLevel: FilterLevel.Minimal },
    grep: { maxMatches: 100 },
    ls: { maxDepth: 4 },
  },
  cache: {
    enabled: true,
    maxEntries: 5000,
    maxOutputBytes: 65536,
    // Only idempotent, read-only command types are cached. Mutating commands
    // (commit, push, install, build, test) are never served from cache.
    commands: [
      'git status', 'git diff', 'git log', 'git branch',
      'ls', 'cat', 'grep', 'find', 'json', 'smart', 'diff',
      'docker ps', 'docker images', 'kubectl get',
      'gh pr list', 'gh issue list', 'gh run list',
      'pip list', 'npm list', 'pnpm list', 'yarn list',
      'env',
    ],
  },
  excludeCommands: ['ssh', 'vim', 'nano', 'less', 'psql', 'mysql'],
  noiseDirectories: [
    'node_modules', '.git', 'dist', 'build', '.next', 'target',
    '__pycache__', '.cache', 'coverage', '.turbo', 'vendor',
    '.svn', '.hg', 'out', 'tmp', '.tmp',
  ],
  claudeCodeDataDir: path.join(os.homedir(), '.claude', 'projects'),
  modelPricing: {
    'claude-opus-4-5': {
      inputPer1k: 0.015, outputPer1k: 0.075,
      cacheWritePer1k: 0.01875, cacheReadPer1k: 0.0015,
    },
    'claude-sonnet-4-5': {
      inputPer1k: 0.003, outputPer1k: 0.015,
      cacheWritePer1k: 0.00375, cacheReadPer1k: 0.0003,
    },
    'claude-haiku-4-5': {
      inputPer1k: 0.00025, outputPer1k: 0.00125,
      cacheWritePer1k: 0.0003, cacheReadPer1k: 0.00003,
    },
  },
};

export function configPath(): string {
  return path.join(configDir(), 'config.json');
}

function deepMerge<T>(base: T, override: Partial<T>): T {
  if (override === null || override === undefined) return base;
  if (typeof base !== 'object' || base === null) return override as T;
  if (typeof override !== 'object') return override as T;
  if (Array.isArray(base)) return (override as unknown as T) ?? base;

  const out: Record<string, unknown> = { ...(base as Record<string, unknown>) };
  for (const [k, v] of Object.entries(override as Record<string, unknown>)) {
    if (v === undefined) continue;
    const existing = (out as Record<string, unknown>)[k];
    if (
      existing !== null && existing !== undefined &&
      typeof existing === 'object' && !Array.isArray(existing) &&
      typeof v === 'object' && !Array.isArray(v) && v !== null
    ) {
      out[k] = deepMerge(existing, v as Record<string, unknown>);
    } else {
      out[k] = v;
    }
  }
  return out as T;
}

export function loadConfig(): TokConfig {
  let cfg: TokConfig = JSON.parse(JSON.stringify(DEFAULTS));

  try {
    const p = configPath();
    const raw = readFileIfExists(p);
    if (raw === null) {
      writeDefaultConfig();
    } else {
      const parsed = safeJsonParse<Partial<TokConfig>>(raw);
      if (parsed) {
        cfg = deepMerge(cfg, parsed);
      }
    }
  } catch {
    // never throw — fall through to defaults
  }

  if (process.env.TOK_PRICE) {
    const v = parseFloat(process.env.TOK_PRICE);
    if (!isNaN(v)) cfg.tokenPricePer1k = v;
  }
  if (process.env.TOK_ULTRA_COMPACT === '1') {
    cfg.filters.ultraCompact = true;
  }

  cfg.version = TOK_VERSION;
  return cfg;
}

function writeDefaultConfig(): void {
  try {
    ensureDir(configDir());
    fs.writeFileSync(configPath(), JSON.stringify(DEFAULTS, null, 2));
  } catch {
    // ignore — config is purely optional
  }
}

export function shouldSkipTracking(): boolean {
  return process.env.TOK_NO_TRACK === '1';
}

export function shouldSkipCache(): boolean {
  return process.env.TOK_NO_CACHE === '1';
}
