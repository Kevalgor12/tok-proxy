import { DB, readCommands } from '../core/local-db';
import { formatNumber, percent, withinDays } from '../core/utils';

interface Row {
  timestamp: string;
  cmd_type: string;
  saved_bytes: number;
  savings_pct: number;
}

export function runSession(db: DB): string {
  const rows = readCommands(db)
    .slice()
    .sort((a, b) => (a.timestamp < b.timestamp ? -1 : a.timestamp > b.timestamp ? 1 : 0)) as Row[];

  if (rows.length === 0) return 'No commands logged yet.';

  const sessions: Array<{ start: string; end: string; rows: Row[] }> = [];
  let current: Row[] = [];
  let lastTs = 0;
  for (const r of rows) {
    const ts = new Date(r.timestamp).getTime();
    if (current.length === 0) {
      current.push(r);
    } else if (ts - lastTs > 30 * 60 * 1000) {
      sessions.push({
        start: current[0].timestamp,
        end: current[current.length - 1].timestamp,
        rows: current,
      });
      current = [r];
    } else {
      current.push(r);
    }
    lastTs = ts;
  }
  if (current.length > 0) {
    sessions.push({
      start: current[0].timestamp,
      end: current[current.length - 1].timestamp,
      rows: current,
    });
  }

  const lines: string[] = ['Recent sessions:'];
  for (const s of sessions.slice(-10).reverse()) {
    const total = s.rows.length;
    const proxied = s.rows.filter((r) => r.savings_pct > 0).length;
    const saved = s.rows.reduce((acc, r) => acc + r.saved_bytes, 0);
    const startStr = formatRange(s.start, s.end);
    const pct = total > 0 ? (proxied / total) * 100 : 0;
    lines.push(
      `  ${startStr}   ${percent(pct, 0).padStart(4)} proxied (${proxied}/${total} commands)   saved ${formatNumber(saved)} bytes`,
    );
  }

  // Unproxied commands this week
  const unproxCounts = new Map<string, number>();
  for (const r of rows) {
    if (r.savings_pct === 0 && withinDays(r.timestamp, 7)) {
      unproxCounts.set(r.cmd_type, (unproxCounts.get(r.cmd_type) || 0) + 1);
    }
  }
  const unprox = Array.from(unproxCounts.entries())
    .map(([cmd_type, runs]) => ({ cmd_type, runs }))
    .sort((a, b) => b.runs - a.runs)
    .slice(0, 6);
  if (unprox.length > 0) {
    lines.push('');
    lines.push('Unproxied commands this week:');
    lines.push('  ' + unprox.map((u) => `${u.cmd_type} (×${u.runs})`).join('   '));
    lines.push('  → Run: tok discover for suggestions');
  }
  return lines.join('\n');
}

function formatRange(start: string, end: string): string {
  const s = new Date(start);
  const e = new Date(end);
  const month = s.toLocaleString('en-US', { month: 'short', day: 'numeric' });
  const sH = String(s.getHours()).padStart(2, '0') + ':' + String(s.getMinutes()).padStart(2, '0');
  const eH = String(e.getHours()).padStart(2, '0') + ':' + String(e.getMinutes()).padStart(2, '0');
  return `${month} ${sH}-${eH}`;
}
