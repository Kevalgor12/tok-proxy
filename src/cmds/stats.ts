import { DB, readAIUsage } from '../core/local-db';
import { TokConfig } from '../core/config';
import { dollar, formatNumber, isoDay, isoMonth, isoWeek, percent, relativeTime, withinDays } from '../core/utils';

interface StatsArgs {
  model?: string;
  daily?: boolean;
  weekly?: boolean;
  monthly?: boolean;
  graph?: boolean;
  export?: 'json' | 'csv';
}

interface UsageRow {
  timestamp: string;
  model: string;
  source: string;
  input_tokens: number;
  output_tokens: number;
  cache_write_tokens: number;
  cache_read_tokens: number;
  cost_usd: number;
}

export function runStats(db: DB, config: TokConfig, args: StatsArgs): string {
  if (args.export === 'json') return exportJson(db, args);
  if (args.export === 'csv') return exportCsv(db, args);
  if (args.graph) return graphView(db, args);
  if (args.daily) return periodView(db, args, 'day');
  if (args.weekly) return periodView(db, args, 'week');
  if (args.monthly) return periodView(db, args, 'month');
  return summaryView(db, config, args);
}

function selectRows(db: DB, args: StatsArgs, sinceDays?: number): UsageRow[] {
  let rows = readAIUsage(db) as unknown as UsageRow[];
  if (args.model) {
    const needle = args.model.toLowerCase();
    rows = rows.filter((r) => r.model.toLowerCase().includes(needle));
  }
  if (sinceDays !== undefined) {
    rows = rows.filter((r) => withinDays(r.timestamp, sinceDays));
  }
  return rows.slice().sort((a, b) => (a.timestamp < b.timestamp ? 1 : a.timestamp > b.timestamp ? -1 : 0));
}

function aggregate(rows: UsageRow[]) {
  let inT = 0, outT = 0, cwT = 0, crT = 0, cost = 0;
  for (const r of rows) {
    inT += r.input_tokens;
    outT += r.output_tokens;
    cwT += r.cache_write_tokens;
    crT += r.cache_read_tokens;
    cost += r.cost_usd;
  }
  return { input: inT, output: outT, cacheWrite: cwT, cacheRead: crT, cost };
}

function summaryView(db: DB, config: TokConfig, args: StatsArgs): string {
  const total = readAIUsage(db).length;
  if (total === 0) {
    return [
      'No AI usage data yet.',
      '',
      'Get started by ingesting your existing usage:',
      '  tok usage ingest --claude-code     (parse local Claude Code logs)',
      '  tok usage ingest --ccusage         (use ccusage CLI)',
      '  tok usage log --model <m> --input N --output N   (manual entry)',
    ].join('\n');
  }

  const today = aggregate(selectRows(db, args, 1));
  const week = aggregate(selectRows(db, args, 7));
  const month = aggregate(selectRows(db, args, 30));

  const lines: string[] = [];
  lines.push('AI token consumption');
  lines.push('═'.repeat(63));
  lines.push('');
  lines.push('Period          Input       Output    Cache↓    Cache↑    Cost');
  lines.push('─'.repeat(63));
  lines.push(`Today        ${formatNumber(today.input).padStart(9)} ${formatNumber(today.output).padStart(10)} ${formatNumber(today.cacheRead).padStart(9)} ${formatNumber(today.cacheWrite).padStart(9)} ${dollar(today.cost).padStart(8)}`);
  lines.push(`Last 7 days  ${formatNumber(week.input).padStart(9)} ${formatNumber(week.output).padStart(10)} ${formatNumber(week.cacheRead).padStart(9)} ${formatNumber(week.cacheWrite).padStart(9)} ${dollar(week.cost).padStart(8)}`);
  lines.push(`Last 30 days ${formatNumber(month.input).padStart(9)} ${formatNumber(month.output).padStart(10)} ${formatNumber(month.cacheRead).padStart(9)} ${formatNumber(month.cacheWrite).padStart(9)} ${dollar(month.cost).padStart(8)}`);

  // Models used (last 30 days)
  const byModel = new Map<string, { model: string; tokens: number; cost: number }>();
  for (const r of readAIUsage(db)) {
    if (!withinDays(r.timestamp, 30)) continue;
    const g = byModel.get(r.model) || { model: r.model, tokens: 0, cost: 0 };
    g.tokens += r.input_tokens + r.output_tokens;
    g.cost += r.cost_usd;
    byModel.set(r.model, g);
  }
  const modelRows = Array.from(byModel.values()).sort((a, b) => b.tokens - a.tokens);
  const totalTokens = modelRows.reduce((s, r) => s + r.tokens, 0);
  if (modelRows.length > 0) {
    lines.push('');
    lines.push('Models used (last 30 days):');
    for (const r of modelRows.slice(0, 10)) {
      const pct = totalTokens > 0 ? (r.tokens / totalTokens) * 100 : 0;
      lines.push(`  ${r.model.padEnd(24)} ${formatNumber(r.tokens).padStart(11)} tokens  ${pct.toFixed(0).padStart(3)}%  ${dollar(r.cost)}`);
    }
  }

  // Cache efficiency
  const allInput = month.input + month.cacheRead;
  const cacheReadPct = allInput > 0 ? (month.cacheRead / allInput) * 100 : 0;
  const hitRate = (month.cacheRead + month.cacheWrite) > 0
    ? (month.cacheRead / (month.cacheRead + month.cacheWrite)) * 100 : 0;
  const cacheSavingsUsd = (month.cacheRead / 1000) * (config.tokenPricePer1k * 0.9);
  lines.push('');
  lines.push('Cache efficiency (last 30 days):');
  lines.push(`  Cache reads:    ${formatNumber(month.cacheRead).padStart(11)} tokens (${cacheReadPct.toFixed(0)}% of all input)`);
  lines.push(`  Cache writes:   ${formatNumber(month.cacheWrite).padStart(11)} tokens`);
  lines.push(`  Cache hit rate: ${hitRate.toFixed(1)}%`);
  lines.push(`  Est. cache savings: ${dollar(cacheSavingsUsd)}`);

  // Source attribution — most recently ingested source
  const lastBySource = new Map<string, string>();
  for (const r of readAIUsage(db)) {
    const cur = lastBySource.get(r.source);
    if (!cur || r.timestamp > cur) lastBySource.set(r.source, r.timestamp);
  }
  const sourceRow = Array.from(lastBySource.entries())
    .map(([source, last]) => ({ source, last }))
    .sort((a, b) => (a.last < b.last ? 1 : -1))[0];
  if (sourceRow) {
    lines.push('');
    lines.push(`Data source: ${sourceRow.source} (last ingested: ${relativeTime(sourceRow.last)})`);
  }
  lines.push('No data yet? Run: tok usage ingest --ccusage');
  return lines.join('\n');
}

function periodView(db: DB, args: StatsArgs, period: 'day' | 'week' | 'month'): string {
  const rows = selectRows(db, args);
  if (rows.length === 0) return 'No AI usage data yet.';
  const buckets = new Map<string, { in: number; out: number; cw: number; cr: number; cost: number }>();
  for (const r of rows) {
    let key = '';
    if (period === 'day') key = isoDay(r.timestamp);
    else if (period === 'week') key = isoWeek(r.timestamp);
    else key = isoMonth(r.timestamp);
    const cur = buckets.get(key) || { in: 0, out: 0, cw: 0, cr: 0, cost: 0 };
    cur.in += r.input_tokens;
    cur.out += r.output_tokens;
    cur.cw += r.cache_write_tokens;
    cur.cr += r.cache_read_tokens;
    cur.cost += r.cost_usd;
    buckets.set(key, cur);
  }
  const sorted = Array.from(buckets.entries()).sort((a, b) => (a[0] < b[0] ? 1 : -1));
  const lines: string[] = [];
  lines.push(`AI usage by ${period}`);
  lines.push('─'.repeat(70));
  lines.push(`${period.padEnd(10)}    Input        Output       Cache↓        Cache↑       Cost`);
  lines.push('─'.repeat(70));
  for (const [k, v] of sorted.slice(0, 30)) {
    lines.push(
      `${k.padEnd(10)}  ${formatNumber(v.in).padStart(9)} ${formatNumber(v.out).padStart(11)} ${formatNumber(v.cr).padStart(11)} ${formatNumber(v.cw).padStart(11)} ${dollar(v.cost).padStart(9)}`,
    );
  }
  return lines.join('\n');
}

function graphView(db: DB, args: StatsArgs): string {
  const byDay = new Map<string, number>();
  for (const r of readAIUsage(db)) {
    if (!withinDays(r.timestamp, 30)) continue;
    const day = r.timestamp.slice(0, 10);
    byDay.set(day, (byDay.get(day) || 0) + r.input_tokens + r.output_tokens);
  }
  const rows = Array.from(byDay.entries())
    .map(([day, tokens]) => ({ day, tokens }))
    .sort((a, b) => (a.day < b.day ? -1 : 1));
  if (rows.length === 0) return 'No data yet.';
  const max = Math.max(...rows.map((r) => r.tokens));
  const lines: string[] = ['Daily AI token consumption (last 30 days):', ''];
  for (const r of rows) {
    const len = max > 0 ? Math.round((r.tokens / max) * 40) : 0;
    lines.push(`  ${r.day} ${'█'.repeat(len)} ${formatNumber(r.tokens)}`);
  }
  return lines.join('\n');
}

function exportJson(db: DB, args: StatsArgs): string {
  const rows = selectRows(db, args);
  return JSON.stringify(rows, null, 2);
}

function exportCsv(db: DB, args: StatsArgs): string {
  const rows = selectRows(db, args);
  const header = [
    'timestamp', 'session_id', 'model', 'source',
    'input_tokens', 'output_tokens', 'cache_write_tokens', 'cache_read_tokens',
    'cost_usd', 'day', 'week', 'month', 'total_tokens',
  ];
  const lines: string[] = [header.join(',')];
  for (const r of rows as Array<UsageRow & { session_id: string }>) {
    const day = isoDay(r.timestamp);
    const week = isoWeek(r.timestamp);
    const month = isoMonth(r.timestamp);
    const totalTokens =
      r.input_tokens + r.output_tokens + r.cache_write_tokens + r.cache_read_tokens;
    lines.push([
      r.timestamp,
      esc(r.session_id),
      esc(r.model),
      esc(r.source),
      r.input_tokens,
      r.output_tokens,
      r.cache_write_tokens,
      r.cache_read_tokens,
      r.cost_usd,
      day,
      week,
      month,
      totalTokens,
    ].join(','));
  }
  return lines.join('\n');
}

function esc(s: string): string {
  if (/[",\n]/.test(s)) return `"${s.replace(/"/g, '""')}"`;
  return s;
}
