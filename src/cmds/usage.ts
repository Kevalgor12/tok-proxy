import { spawnSync } from 'child_process';
import * as crypto from 'crypto';
import * as fs from 'fs';
import * as path from 'path';

import { TokConfig } from '../core/config';
import { DB, recordAIUsage, readAIUsage, AIUsageRecord } from '../core/local-db';
import { formatNumber, dollar, appendErrorLog, safeJsonParse } from '../core/utils';

interface IngestArgs {
  source: 'claude-code' | 'ccusage';
  since?: string;
}

export function runUsageIngest(db: DB, config: TokConfig, args: IngestArgs): string {
  if (args.source === 'claude-code') return ingestClaudeCode(db, config, args.since);
  if (args.source === 'ccusage') return ingestCcusage(db, args.since);
  return 'unknown source';
}

function ingestClaudeCode(db: DB, config: TokConfig, since?: string): string {
  const root = config.claudeCodeDataDir;
  if (!fs.existsSync(root)) {
    return `Claude Code data directory not found: ${root}`;
  }

  const sinceMs = since ? new Date(since).getTime() : 0;
  let inserted = 0;
  let skipped = 0;
  let fileCount = 0;

  const files = collectJsonl(root);
  for (const file of files) {
    fileCount++;
    let content: string;
    try {
      content = fs.readFileSync(file, 'utf8');
    } catch (err) {
      appendErrorLog('ingestClaudeCode.read', err);
      continue;
    }
    const sessionId = path.basename(file, '.jsonl');
    for (const line of content.split('\n')) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      const obj = safeJsonParse<{
        type?: string;
        timestamp?: string;
        message?: {
          model?: string;
          usage?: {
            input_tokens?: number;
            output_tokens?: number;
            cache_creation_input_tokens?: number;
            cache_read_input_tokens?: number;
          };
        };
      }>(trimmed);
      if (!obj || obj.type !== 'assistant') continue;
      const usage = obj.message?.usage;
      const model = obj.message?.model;
      if (!usage || !model || !obj.timestamp) continue;
      if (sinceMs && new Date(obj.timestamp).getTime() < sinceMs) continue;

      const record: AIUsageRecord = {
        timestamp: obj.timestamp,
        session_id: sessionId,
        model,
        source: 'claude-code',
        input_tokens: usage.input_tokens || 0,
        output_tokens: usage.output_tokens || 0,
        cache_write_tokens: usage.cache_creation_input_tokens || 0,
        cache_read_tokens: usage.cache_read_input_tokens || 0,
        cost_usd: 0,
      };
      const ok = recordAIUsage(db, record);
      if (ok) inserted++;
      else skipped++;
    }
  }
  return `Ingested ${formatNumber(inserted)} new entries from ${fileCount} files. Skipped ${formatNumber(skipped)} already imported.`;
}

function collectJsonl(root: string): string[] {
  const out: string[] = [];
  const stack = [root];
  while (stack.length) {
    const dir = stack.pop()!;
    let entries: fs.Dirent[];
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const e of entries) {
      const full = path.join(dir, e.name);
      if (e.isDirectory()) stack.push(full);
      else if (e.isFile() && e.name.endsWith('.jsonl')) out.push(full);
    }
  }
  return out;
}

function ingestCcusage(db: DB, since?: string): string {
  const sinceArg = since || isoDaysAgo(90);
  const isWin = process.platform === 'win32';

  let result = spawnSync('ccusage', ['--json', '--since', sinceArg], {
    encoding: 'utf8',
    shell: isWin,
    maxBuffer: 50 * 1024 * 1024,
  });
  if (result.error || result.status !== 0) {
    result = spawnSync('npx', ['--yes', 'ccusage', '--json', '--since', sinceArg], {
      encoding: 'utf8',
      shell: isWin,
      maxBuffer: 50 * 1024 * 1024,
    });
  }
  if (result.error || result.status !== 0) {
    return `ccusage failed. Install with: npm i -g ccusage  (error: ${result.stderr || result.error?.message || 'unknown'})`;
  }

  const parsed = safeJsonParse<{
    daily?: Array<{
      date: string;
      models: Record<string, {
        input_tokens?: number;
        output_tokens?: number;
        cache_creation_input_tokens?: number;
        cache_read_input_tokens?: number;
        cost?: number;
      }>;
    }>;
  }>(result.stdout);
  if (!parsed || !Array.isArray(parsed.daily)) {
    return 'ccusage returned no daily data';
  }

  let inserted = 0;
  let skipped = 0;
  for (const day of parsed.daily) {
    const ts = `${day.date}T12:00:00.000Z`;
    for (const [model, data] of Object.entries(day.models || {})) {
      const record: AIUsageRecord = {
        timestamp: ts,
        session_id: `ccusage-${day.date}`,
        model,
        source: 'ccusage',
        input_tokens: data.input_tokens || 0,
        output_tokens: data.output_tokens || 0,
        cache_write_tokens: data.cache_creation_input_tokens || 0,
        cache_read_tokens: data.cache_read_input_tokens || 0,
        cost_usd: data.cost || 0,
      };
      const ok = recordAIUsage(db, record);
      if (ok) inserted++;
      else skipped++;
    }
  }
  return `Ingested ${formatNumber(inserted)} new entries from ccusage. Skipped ${formatNumber(skipped)} already imported.`;
}

interface ManualLogArgs {
  model: string;
  input: number;
  output: number;
  cacheWrite?: number;
  cacheRead?: number;
  cost?: number;
}

export function runUsageLog(db: DB, args: ManualLogArgs): string {
  const record: AIUsageRecord = {
    timestamp: new Date().toISOString(),
    session_id: crypto.randomUUID(),
    model: args.model,
    source: 'manual',
    input_tokens: args.input,
    output_tokens: args.output,
    cache_write_tokens: args.cacheWrite || 0,
    cache_read_tokens: args.cacheRead || 0,
    cost_usd: args.cost || 0,
  };
  recordAIUsage(db, record);
  return `Logged: ${args.model} | ${formatNumber(args.input)} in + ${formatNumber(args.output)} out | ${dollar(args.cost || 0)}`;
}

export function runUsageModels(db: DB): string {
  const byModel = new Map<string, { model: string; n: number; tokens: number; cost: number }>();
  for (const r of readAIUsage(db)) {
    const g = byModel.get(r.model) || { model: r.model, n: 0, tokens: 0, cost: 0 };
    g.n += 1;
    g.tokens += r.input_tokens + r.output_tokens;
    g.cost += r.cost_usd;
    byModel.set(r.model, g);
  }
  const rows = Array.from(byModel.values()).sort((a, b) => b.tokens - a.tokens);
  if (rows.length === 0) return 'No models seen yet.';
  const lines: string[] = ['Models seen:', ''];
  for (const r of rows) {
    lines.push(`  ${r.model.padEnd(28)} ${r.n.toString().padStart(6)} entries  ${formatNumber(r.tokens).padStart(11)} tokens  ${dollar(r.cost)}`);
  }
  return lines.join('\n');
}

function isoDaysAgo(days: number): string {
  const d = new Date(Date.now() - days * 24 * 3600 * 1000);
  return d.toISOString().slice(0, 10);
}
