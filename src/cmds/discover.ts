import { DB, readCommands } from '../core/local-db';
import { TokConfig } from '../core/config';
import { dollar, estimateTokens, formatNumber, withinDays } from '../core/utils';

const KNOWN_FILTERED = new Set([
  'git status', 'git st', 'git diff', 'git log', 'git push', 'git pull', 'git add',
  'git commit', 'git branch', 'git fetch',
  'npm install', 'npm i', 'npm add', 'npm list', 'npm ls', 'npm outdated', 'npm run',
  'pnpm install', 'pnpm add', 'pnpm list', 'pnpm outdated',
  'yarn', 'yarn add', 'yarn list', 'yarn outdated',
  'tsc',
  'jest', 'vitest', 'mocha',
  'eslint', 'biome', 'prettier',
  'ls', 'cat', 'grep', 'find', 'diff', 'json', 'smart',
  'docker ps', 'docker images', 'docker logs', 'docker compose',
  'kubectl get', 'kubectl logs',
]);

const POTENTIAL_REDUCTIONS: Record<string, number> = {
  'docker logs': 0.85,
  'docker compose': 0.7,
  'kubectl logs': 0.85,
  'npm run dev': 0.7,
  'npm run start': 0.7,
  'tail': 0.85,
  'find': 0.6,
  'curl': 0.5,
};

export function runDiscover(db: DB, config: TokConfig): string {
  const recent = readCommands(db).filter(
    (r) => withinDays(r.timestamp, 7) && (r.savings_pct === 0 || r.savings_pct == null),
  );
  const grouped = new Map<string, { runs: number; totalIn: number }>();
  for (const r of recent) {
    const g = grouped.get(r.cmd_type) || { runs: 0, totalIn: 0 };
    g.runs += 1;
    g.totalIn += r.input_bytes;
    grouped.set(r.cmd_type, g);
  }
  const rows = Array.from(grouped.entries())
    .map(([cmd_type, g]) => ({ cmd_type, runs: g.runs, avgIn: g.runs ? g.totalIn / g.runs : 0 }))
    .sort((a, b) => b.runs - a.runs);

  const candidates = rows.filter((r) => !KNOWN_FILTERED.has(r.cmd_type));
  if (candidates.length === 0) {
    return 'No missed optimizations detected this week.';
  }

  const lines: string[] = ['Missed optimization opportunities (last 7 days):'];
  let totalPotential = 0;
  for (const c of candidates.slice(0, 10)) {
    const pct = guessReduction(c.cmd_type);
    const potentialBytes = c.runs * c.avgIn * pct;
    totalPotential += potentialBytes;
    const tokens = estimateTokens(' '.repeat(Math.floor(potentialBytes)));
    lines.push(
      `  ${c.cmd_type.padEnd(18)} ${String(c.runs).padStart(3)} runs × ~${(pct * 100).toFixed(0)}% savings = ~${formatNumber(tokens)} tokens/week potential`,
    );
  }
  const totalTokens = estimateTokens(' '.repeat(Math.floor(totalPotential)));
  const totalUsd = (totalTokens / 1000) * config.tokenPricePer1k;
  lines.push('');
  lines.push(`Total potential:  ~${formatNumber(totalTokens)} tokens/week (~${dollar(totalUsd)}/week at current pricing)`);
  lines.push('Fix with: tok init (auto-rewrites these via hooks)');
  lines.push('Or run manually: tok <cmd> <args>');
  return lines.join('\n');
}

function guessReduction(cmdType: string): number {
  for (const [key, value] of Object.entries(POTENTIAL_REDUCTIONS)) {
    if (cmdType.includes(key)) return value;
  }
  return 0.6;
}
