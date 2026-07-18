import { TokConfig } from '../core/config';
import { DB, cacheStats, topCacheEntries, clearCache } from '../core/local-db';
import { formatBytes, formatNumber, estimateTokens, relativeTime, pad } from '../core/utils';

interface CacheOpts {
  clear?: boolean;
  list?: boolean;
}

// `tok cache` - inspect or clear the output cache. The cache is what powers
// unchanged-detection: repeated idempotent reads return a marker instead of the
// full payload.
export function runCache(db: DB, config: TokConfig, opts: CacheOpts): string {
  if (opts.clear) {
    const n = clearCache(db);
    return `Cleared ${formatNumber(n)} cache entr${n === 1 ? 'y' : 'ies'}.`;
  }

  const stats = cacheStats(db);
  const savedTokens = estimateTokens(' '.repeat(stats.savedBytes));
  const lines: string[] = [];
  lines.push('tok cache - unchanged-output detection');
  lines.push('══════════════════════════════════════════════════════════');
  lines.push(`Status:        ${config.cache.enabled ? 'enabled' : 'disabled'}`);
  lines.push(`Entries:       ${formatNumber(stats.entries)} / ${formatNumber(config.cache.maxEntries)} max`);
  lines.push(`Cache hits:    ${formatNumber(stats.hits)}  (repeats served as a marker)`);
  lines.push(`Tokens saved:  ~${formatNumber(savedTokens)}  (${formatBytes(stats.savedBytes)})`);

  if (opts.list || stats.hits > 0) {
    const top = topCacheEntries(db, 15).filter((e) => e.hit_count > 0);
    if (top.length > 0) {
      lines.push('');
      lines.push('Most-reused commands:');
      lines.push(`  ${pad('hits', 6)} ${pad('bytes', 9)} command (last seen)`);
      for (const e of top) {
        lines.push(
          `  ${pad(String(e.hit_count), 6)} ${pad(formatBytes(e.filtered_bytes), 9)} ${e.cmd_type} (${relativeTime(e.last_seen)})`,
        );
      }
    }
  }

  if (stats.entries === 0) {
    lines.push('');
    lines.push('No cached commands yet. The cache fills as idempotent reads');
    lines.push('(git status, ls, cat, grep, …) are run more than once.');
  }
  lines.push('');
  lines.push('Commands: tok cache --list | tok cache --clear');
  return lines.join('\n');
}
