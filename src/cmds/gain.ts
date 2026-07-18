import { TokConfig } from '../core/config';
import { DB, readCommands, CommandRow } from '../core/local-db';
import { dollar, estimateTokens, percent, formatNumber, withinDays } from '../core/utils';

interface GainArgs {
  graph?: boolean;
  history?: boolean;
  daily?: boolean;
  format?: string;
}

interface DayBucket {
  day: string;
  inBytes: number;
  outBytes: number;
  saved: number;
  count: number;
}

export function runGain(db: DB, config: TokConfig, args: GainArgs): string {
  if (args.format === 'json') return jsonExport(db);
  if (args.graph) return graphView(db);
  if (args.history) return historyView(db);
  if (args.daily) return dailyView(db);
  return summaryView(db, config);
}

function summaryView(db: DB, config: TokConfig): string {
  const cmds = readCommands(db);
  const today = aggregate(cmds, 1);
  const week = aggregate(cmds, 7);
  const allTime = aggregate(cmds, 36500);

  const lines: string[] = [];
  lines.push('tok savings - filter compression');
  lines.push('═'.repeat(58));
  lines.push(`Today:        ${tokRow(today, config)}`);
  lines.push(`Last 7 days:  ${tokRow(week, config)}`);
  lines.push(`All time:     ${tokRow(allTime, config)}`);

  lines.push('');
  lines.push('Top commands today:');
  const todayGroups = new Map<string, { runs: number; sumPct: number }>();
  for (const r of cmds) {
    if (!withinDays(r.timestamp, 1)) continue;
    const g = todayGroups.get(r.cmd_type) || { runs: 0, sumPct: 0 };
    g.runs += 1;
    g.sumPct += r.savings_pct;
    todayGroups.set(r.cmd_type, g);
  }
  const topCmds = Array.from(todayGroups.entries())
    .map(([cmd_type, g]) => ({ cmd_type, runs: g.runs, avgPct: g.runs ? g.sumPct / g.runs : 0 }))
    .sort((a, b) => b.runs - a.runs)
    .slice(0, 8);
  if (topCmds.length === 0) {
    lines.push('  (no data yet)');
  } else {
    for (const row of topCmds) {
      lines.push(`  ${row.cmd_type.padEnd(14)} ${percent(row.avgPct, 0).padEnd(5)} ${row.runs} runs`);
    }
  }

  const unoptCounts = new Map<string, number>();
  for (const r of cmds) {
    if (r.savings_pct < 1 && withinDays(r.timestamp, 7)) {
      unoptCounts.set(r.cmd_type, (unoptCounts.get(r.cmd_type) || 0) + 1);
    }
  }
  const unopt = Array.from(unoptCounts.entries())
    .map(([cmd_type, runs]) => ({ cmd_type, runs }))
    .sort((a, b) => b.runs - a.runs)
    .slice(0, 5);
  if (unopt.length > 0) {
    lines.push('');
    lines.push('Not yet optimized (0% savings):');
    const compact = unopt.map((r) => `${r.cmd_type} (×${r.runs})`).join('  ');
    lines.push(`  ${compact}`);
    lines.push('  → Run: tok discover for optimization suggestions');
  }
  return lines.join('\n');
}

function tokRow(b: DayBucket, config: TokConfig): string {
  const inT = estimateTokens(' '.repeat(b.inBytes));
  const outT = estimateTokens(' '.repeat(b.outBytes));
  const savedT = estimateTokens(' '.repeat(b.saved));
  const pct = b.inBytes > 0 ? (b.saved / b.inBytes) * 100 : 0;
  const cost = (savedT / 1000) * config.tokenPricePer1k;
  return `${formatNumber(inT)} → ${formatNumber(outT)} tokens   saved ${pct.toFixed(0)}%  (~${dollar(cost)})`;
}

function aggregate(cmds: CommandRow[], days: number): DayBucket {
  let inB = 0;
  let outB = 0;
  let sB = 0;
  let n = 0;
  for (const r of cmds) {
    if (!withinDays(r.timestamp, days)) continue;
    inB += r.input_bytes;
    outB += r.out_bytes;
    sB += r.saved_bytes;
    n += 1;
  }
  return { day: '', inBytes: inB, outBytes: outB, saved: sB, count: n };
}

function graphView(db: DB): string {
  const byDay = new Map<string, number>();
  for (const r of readCommands(db)) {
    if (!withinDays(r.timestamp, 30)) continue;
    const day = r.timestamp.slice(0, 10);
    byDay.set(day, (byDay.get(day) || 0) + r.saved_bytes);
  }
  const rows = Array.from(byDay.entries())
    .map(([day, sB]) => ({ day, sB }))
    .sort((a, b) => (a.day < b.day ? -1 : 1));
  if (rows.length === 0) return 'No data yet.';
  const max = Math.max(...rows.map((r) => r.sB));
  const lines: string[] = ['Daily savings (last 30 days, bytes):', ''];
  for (const r of rows) {
    const len = max > 0 ? Math.round((r.sB / max) * 40) : 0;
    const bar = '█'.repeat(len);
    lines.push(`  ${r.day} ${bar} ${formatNumber(r.sB)}`);
  }
  return lines.join('\n');
}

function historyView(db: DB): string {
  const rows = readCommands(db).slice(-20).reverse();
  if (rows.length === 0) return 'No history yet.';
  const lines: string[] = ['Last 20 commands:', ''];
  for (const r of rows) {
    lines.push(
      `  ${r.timestamp.replace('T', ' ').slice(0, 19)} ${r.cmd_type.padEnd(14)} ${formatNumber(r.input_bytes).padStart(8)} → ${formatNumber(r.out_bytes).padStart(8)}  ${percent(r.savings_pct, 0).padStart(5)}`,
    );
  }
  return lines.join('\n');
}

function dailyView(db: DB): string {
  const byDay = new Map<string, { runs: number; inB: number; outB: number; sB: number }>();
  for (const r of readCommands(db)) {
    if (!withinDays(r.timestamp, 30)) continue;
    const day = r.timestamp.slice(0, 10);
    const g = byDay.get(day) || { runs: 0, inB: 0, outB: 0, sB: 0 };
    g.runs += 1;
    g.inB += r.input_bytes;
    g.outB += r.out_bytes;
    g.sB += r.saved_bytes;
    byDay.set(day, g);
  }
  const rows = Array.from(byDay.entries())
    .map(([day, g]) => ({ day, ...g }))
    .sort((a, b) => (a.day < b.day ? 1 : -1));
  if (rows.length === 0) return 'No data yet.';
  const lines: string[] = [
    'Daily savings (last 30 days)',
    '─'.repeat(70),
    'Day         Runs      Input         Output          Saved      %',
    '─'.repeat(70),
  ];
  for (const r of rows) {
    const pct = r.inB > 0 ? (r.sB / r.inB) * 100 : 0;
    lines.push(
      `${r.day}  ${String(r.runs).padStart(5)} ${formatNumber(r.inB).padStart(11)} ${formatNumber(r.outB).padStart(13)} ${formatNumber(r.sB).padStart(13)} ${percent(pct, 0).padStart(5)}`,
    );
  }
  return lines.join('\n');
}

function jsonExport(db: DB): string {
  const rows = readCommands(db).slice().reverse();
  return JSON.stringify({ rows }, null, 2);
}
