import { DB, readAIUsage, readCommands } from '../core/local-db';
import { TokConfig, ModelPricing } from '../core/config';
import { dollar, estimateTokens, formatNumber, isoDay, isoMonth, isoWeek, percent, withinDays } from '../core/utils';

interface EconArgs {
  daily?: boolean;
  weekly?: boolean;
  monthly?: boolean;
  export?: 'json' | 'csv';
}

interface PeriodStats {
  in: number;
  out: number;
  cw: number;
  cr: number;
  cost: number;
  saved: number;
}

export function runEcon(db: DB, config: TokConfig, args: EconArgs): string {
  if (args.export === 'json') return exportJson(db, config, args);
  if (args.export === 'csv') return exportCsv(db, config, args);
  if (args.daily) return periodView(db, config, 'day');
  if (args.weekly) return periodView(db, config, 'week');
  if (args.monthly) return periodView(db, config, 'month');
  return summaryView(db, config);
}

function getStats(db: DB, sinceDays?: number): PeriodStats {
  const inWindow = (ts: string) => sinceDays === undefined || withinDays(ts, sinceDays);
  let i = 0, o = 0, cw = 0, cr = 0, cost = 0, saved = 0;
  for (const r of readAIUsage(db)) {
    if (!inWindow(r.timestamp)) continue;
    i += r.input_tokens;
    o += r.output_tokens;
    cw += r.cache_write_tokens;
    cr += r.cache_read_tokens;
    cost += r.cost_usd;
  }
  for (const r of readCommands(db)) {
    if (!inWindow(r.timestamp)) continue;
    saved += r.saved_bytes;
  }
  return { in: i, out: o, cw, cr, cost, saved };
}

function calcWeightedCpt(s: PeriodStats, fallbackPer1k: number): { inputCpt: number; estimated: boolean } {
  const weightedUnits = s.in + 5.0 * s.out + 1.25 * s.cw + 0.1 * s.cr;
  if (s.cost > 0 && weightedUnits > 0) {
    return { inputCpt: s.cost / weightedUnits, estimated: false };
  }
  return { inputCpt: fallbackPer1k / 1000, estimated: true };
}

function summaryView(db: DB, config: TokConfig): string {
  const month = getStats(db, 30);
  const savedTokens = estimateTokens(' '.repeat(month.saved));

  const { inputCpt, estimated } = calcWeightedCpt(month, config.tokenPricePer1k);
  const savedValueUsd = savedTokens * inputCpt;
  const withoutTok = month.cost + savedValueUsd;
  const savedPctOfBill = withoutTok > 0 ? (savedValueUsd / withoutTok) * 100 : 0;

  const lines: string[] = [];
  lines.push('tok economics dashboard');
  lines.push('═'.repeat(63));
  lines.push('');
  lines.push('LAST 30 DAYS');
  lines.push('─'.repeat(63));
  lines.push(`AI tokens consumed    ${formatNumber(month.in).padStart(11)} input  +  ${formatNumber(month.out)} output`);
  lines.push(`Cache tokens          ${formatNumber(month.cr).padStart(11)} reads  +  ${formatNumber(month.cw)} writes`);
  const costNote = estimated ? '(estimated — run tok usage ingest --ccusage for actuals)' : '(verified)';
  lines.push(`Actual cost ${costNote.padStart(36)}  ${dollar(month.cost)}`);
  lines.push(`Effective input CPT                 ${dollar(inputCpt).padStart(7)} / token`);
  lines.push('');
  lines.push(`tok filter savings    ${formatNumber(savedTokens).padStart(11)} tokens prevented`);
  lines.push(`Cost avoided (weighted)             ${dollar(savedValueUsd).padStart(7)}`);
  lines.push('─'.repeat(63));
  lines.push(`Net cost WITH tok                   ${dollar(month.cost).padStart(7)}`);
  lines.push(`Estimated cost WITHOUT tok          ${dollar(withoutTok).padStart(7)}`);
  lines.push(`tok saved you                       ${percent(savedPctOfBill, 1).padStart(7)} of your AI bill`);

  // Context window health
  lines.push('');
  lines.push('CONTEXT WINDOW HEALTH');
  lines.push('─'.repeat(63));
  const sessionInfo = computeSessionStats(db);
  lines.push(`Avg tokens per session       ${formatNumber(sessionInfo.avgTokens).padStart(11)}`);
  if (sessionInfo.largest) {
    lines.push(`Largest session              ${formatNumber(sessionInfo.largest.tokens).padStart(11)} tokens   (${sessionInfo.largest.day})`);
  }
  const heavy = sessionInfo.over100k;
  const warn = heavy > 0 ? '⚠ approaching limit' : '✓ comfortable';
  lines.push(`Sessions > 100K tokens       ${String(heavy).padStart(11)}          ${warn}`);
  const cacheHit = (month.cr + month.cw) > 0 ? (month.cr / (month.cr + month.cw)) * 100 : 0;
  const hitTag = cacheHit >= 50 ? '✓ good caching' : '⚠ low cache reuse';
  lines.push(`Cache hit rate               ${percent(cacheHit, 1).padStart(11)}          ${hitTag}`);

  // Model comparison
  lines.push('');
  lines.push('MODEL COST COMPARISON (this session)');
  lines.push('─'.repeat(63));
  const compareModels = ['claude-opus-4-5', 'claude-sonnet-4-5', 'claude-haiku-4-5'];
  const session = getStats(db, 1);
  const sessionTokens = session.in + session.out;
  if (sessionTokens === 0) {
    lines.push('  (no AI usage today to compare)');
  } else {
    const opusCost = priceModel(session, config.modelPricing['claude-opus-4-5']);
    for (const m of compareModels) {
      const p = config.modelPricing[m];
      if (!p) continue;
      const c = priceModel(session, p);
      const tag = m === 'claude-opus-4-5' ? '(actual)' : `(estimated at ${m.split('-')[1]} pricing)`;
      lines.push(`${m.padEnd(20)} ${dollar(c).padStart(7)}   ${tag}`);
    }
    const sonnet = priceModel(session, config.modelPricing['claude-sonnet-4-5']);
    if (opusCost > 0) {
      const reduction = ((opusCost - sonnet) / opusCost) * 100;
      lines.push(`→ Switch to Sonnet → save ~${reduction.toFixed(0)}% on cost with comparable quality`);
    }
  }

  return lines.join('\n');
}

function priceModel(s: PeriodStats, p: ModelPricing | undefined): number {
  if (!p) return 0;
  return (
    (s.in / 1000) * p.inputPer1k +
    (s.out / 1000) * p.outputPer1k +
    (s.cw / 1000) * p.cacheWritePer1k +
    (s.cr / 1000) * p.cacheReadPer1k
  );
}

interface SessionInfo {
  avgTokens: number;
  largest: { day: string; tokens: number } | null;
  over100k: number;
}

function computeSessionStats(db: DB): SessionInfo {
  const bySession = new Map<string, { tokens: number; day: string }>();
  for (const r of readAIUsage(db)) {
    const day = r.timestamp.slice(0, 10);
    const g = bySession.get(r.session_id) || { tokens: 0, day };
    g.tokens += r.input_tokens + r.output_tokens;
    if (day < g.day) g.day = day; // MIN(day)
    bySession.set(r.session_id, g);
  }
  const rows = Array.from(bySession.entries()).map(([session_id, g]) => ({
    session_id,
    tokens: g.tokens,
    day: g.day,
  }));
  if (rows.length === 0) return { avgTokens: 0, largest: null, over100k: 0 };
  const total = rows.reduce((s, r) => s + r.tokens, 0);
  const avg = Math.floor(total / rows.length);
  const largest = rows.reduce((acc, r) => (r.tokens > acc.tokens ? r : acc), rows[0]);
  const over100k = rows.filter((r) => r.tokens > 100000).length;
  return { avgTokens: avg, largest: { day: largest.day, tokens: largest.tokens }, over100k };
}

function periodView(db: DB, config: TokConfig, period: 'day' | 'week' | 'month'): string {
  const usage = readAIUsage(db);
  const cmds = readCommands(db);

  const buckets = new Map<string, PeriodStats>();
  const keyFor = (ts: string) =>
    period === 'day' ? isoDay(ts) : period === 'week' ? isoWeek(ts) : isoMonth(ts);

  for (const r of usage) {
    const k = keyFor(r.timestamp);
    const cur = buckets.get(k) || { in: 0, out: 0, cw: 0, cr: 0, cost: 0, saved: 0 };
    cur.in += r.input_tokens;
    cur.out += r.output_tokens;
    cur.cw += r.cache_write_tokens;
    cur.cr += r.cache_read_tokens;
    cur.cost += r.cost_usd;
    buckets.set(k, cur);
  }
  for (const r of cmds) {
    const k = keyFor(r.timestamp);
    const cur = buckets.get(k) || { in: 0, out: 0, cw: 0, cr: 0, cost: 0, saved: 0 };
    cur.saved += r.saved_bytes;
    buckets.set(k, cur);
  }

  const sorted = Array.from(buckets.entries()).sort((a, b) => (a[0] < b[0] ? 1 : -1));
  const lines: string[] = [];
  lines.push(`Economics by ${period}`);
  lines.push('─'.repeat(75));
  lines.push(`${period.padEnd(10)}    Cost      Saved$    ROI%   Tokens(in+out)   Saved tokens`);
  lines.push('─'.repeat(75));
  for (const [k, v] of sorted.slice(0, 30)) {
    const { inputCpt } = calcWeightedCpt(v, config.tokenPricePer1k);
    const savedTokens = estimateTokens(' '.repeat(v.saved));
    const savedUsd = savedTokens * inputCpt;
    const roi = v.cost > 0 ? (savedUsd / v.cost) * 100 : 0;
    lines.push(
      `${k.padEnd(10)}  ${dollar(v.cost).padStart(8)} ${dollar(savedUsd).padStart(9)} ${percent(roi, 0).padStart(6)}  ${formatNumber(v.in + v.out).padStart(13)} ${formatNumber(savedTokens).padStart(13)}`,
    );
  }
  return lines.join('\n');
}

function exportJson(db: DB, config: TokConfig, args: EconArgs): string {
  const month = getStats(db, 30);
  const savedTokens = estimateTokens(' '.repeat(month.saved));
  const { inputCpt, estimated } = calcWeightedCpt(month, config.tokenPricePer1k);
  return JSON.stringify({
    period: 'last_30_days',
    input_tokens: month.in,
    output_tokens: month.out,
    cache_write_tokens: month.cw,
    cache_read_tokens: month.cr,
    cost_usd: month.cost,
    saved_bytes: month.saved,
    saved_tokens: savedTokens,
    weighted_input_cpt: inputCpt,
    saved_usd_estimated: savedTokens * inputCpt,
    cost_estimated: estimated,
  }, null, 2);
}

function exportCsv(db: DB, config: TokConfig, args: EconArgs): string {
  const rows = readAIUsage(db);
  const cmdsByDay = new Map<string, number>();
  const cmds = readCommands(db);
  for (const c of cmds) {
    const d = isoDay(c.timestamp);
    cmdsByDay.set(d, (cmdsByDay.get(d) || 0) + c.saved_bytes);
  }

  const header = [
    'timestamp', 'day', 'week', 'month', 'model', 'source',
    'input_tokens', 'output_tokens', 'cache_write_tokens', 'cache_read_tokens',
    'cost_usd', 'saved_bytes_day', 'saved_tokens_day',
  ];
  const lines: string[] = [header.join(',')];
  for (const r of rows) {
    const day = isoDay(r.timestamp);
    const savedB = cmdsByDay.get(day) || 0;
    const savedT = estimateTokens(' '.repeat(savedB));
    lines.push([
      r.timestamp,
      day,
      isoWeek(r.timestamp),
      isoMonth(r.timestamp),
      esc(r.model),
      esc(r.source),
      r.input_tokens,
      r.output_tokens,
      r.cache_write_tokens,
      r.cache_read_tokens,
      r.cost_usd,
      savedB,
      savedT,
    ].join(','));
  }
  return lines.join('\n');
}

function esc(s: string): string {
  if (/[",\n]/.test(s)) return `"${s.replace(/"/g, '""')}"`;
  return s;
}
