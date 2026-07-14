// Single source of truth for command rewrite + permission rules.
// The hook scripts delegate here via `tok rewrite "<cmd>"`.
//
// Exit-code protocol used by `tok rewrite`:
//   0  + stdout = rewritten cmd     → hook returns "allow" (auto-allowed)
//   1                                 → no rewrite available, pass through
//   2                                 → deny rule matched, pass through to AI tool's native deny
//   3  + stdout = rewritten cmd     → "ask" rule matched, hook returns rewrite without auto-allow

export type RewriteOutcome =
  | { kind: 'allow'; rewritten: string }
  | { kind: 'none' }
  | { kind: 'deny' }
  | { kind: 'ask'; rewritten: string };

interface Rule {
  // Match the leading token(s) of the command. Plain string = exact prefix; regex = exact match.
  match: string | RegExp;
  // What to substitute the matched leading portion with.
  replace: string;
  // Optional permission action.
  action?: 'allow' | 'ask' | 'deny';
}

// Order matters: more specific rules (npx tsc, npx jest) must come before generic ones.
export const RULES: Rule[] = [
  // Bypass — already a tok command
  { match: /^tok(\s|$)/, replace: '__noop__' },

  // Rewrite npx invocations of supported tools
  { match: /^npx\s+tsc(\s|$)/, replace: 'tok tsc$1' },
  { match: /^npx\s+jest(\s|$)/, replace: 'tok jest$1' },
  { match: /^npx\s+vitest(\s|$)/, replace: 'tok vitest$1' },
  { match: /^npx\s+mocha(\s|$)/, replace: 'tok mocha$1' },
  { match: /^npx\s+eslint(\s|$)/, replace: 'tok eslint$1' },
  { match: /^npx\s+prettier(\s|$)/, replace: 'tok prettier$1' },
  { match: /^npx\s+biome(\s|$)/, replace: 'tok biome$1' },
  { match: /^npx\s+prisma(\s|$)/, replace: 'tok prisma$1' },
  { match: /^(?:npx\s+)?playwright\s+test(\s|$)/, replace: 'tok playwright test$1' },
  { match: /^(?:npx\s+)?next\s+build(\s|$)/, replace: 'tok next build$1' },
  { match: /^(?:npx\s+)?next\s+lint(\s|$)/, replace: 'tok next lint$1' },

  // Targeted subcommands — only rewrite the sub we know how to compress, so we
  // never mis-summarize unrelated tasks (e.g. `rake db:migrate`, `pulumi stack`).
  { match: /^rake\s+test(\s|$)/, replace: 'tok rake test$1' },
  { match: /^pulumi\s+(preview|up|destroy)(\s|$)/, replace: 'tok pulumi $1$2' },
  { match: /^terraform\s+(plan|apply)(\s|$)/, replace: 'tok terraform $1$2' },

  // Direct invocations
  { match: /^git(\s|$)/, replace: 'tok git$1' },
  { match: /^npm(\s|$)/, replace: 'tok npm$1' },
  { match: /^pnpm(\s|$)/, replace: 'tok pnpm$1' },
  { match: /^yarn(\s|$)/, replace: 'tok yarn$1' },
  { match: /^tsc(\s|$)/, replace: 'tok tsc$1' },
  { match: /^jest(\s|$)/, replace: 'tok jest$1' },
  { match: /^vitest(\s|$)/, replace: 'tok vitest$1' },
  { match: /^mocha(\s|$)/, replace: 'tok mocha$1' },
  { match: /^eslint(\s|$)/, replace: 'tok eslint$1' },
  { match: /^prettier(\s|$)/, replace: 'tok prettier$1' },
  { match: /^biome(\s|$)/, replace: 'tok biome$1' },
  { match: /^docker(\s|$)/, replace: 'tok docker$1' },
  { match: /^kubectl(\s|$)/, replace: 'tok kubectl$1' },
  { match: /^ls(\s|$)/, replace: 'tok ls$1' },
  { match: /^grep(\s|$)/, replace: 'tok grep$1' },
  { match: /^rg(\s|$)/, replace: 'tok grep$1' },
  { match: /^find(\s|$)/, replace: 'tok find$1' },

  // GitHub CLI
  { match: /^gh(\s|$)/, replace: 'tok gh$1' },

  // Test runners (non-JS). Unknown subcommands fall through to full output.
  { match: /^pytest(\s|$)/, replace: 'tok pytest$1' },
  { match: /^rspec(\s|$)/, replace: 'tok rspec$1' },

  // Go / Rust toolchains — handler dispatches per sub-command; non-test/build
  // subs (go run, cargo fmt, …) pass through in full.
  { match: /^go(\s|$)/, replace: 'tok go$1' },
  { match: /^cargo(\s|$)/, replace: 'tok cargo$1' },

  // Linters / compilers for other ecosystems
  { match: /^ruff(\s|$)/, replace: 'tok ruff$1' },
  { match: /^golangci-lint(\s|$)/, replace: 'tok golangci-lint$1' },
  { match: /^rubocop(\s|$)/, replace: 'tok rubocop$1' },

  // Package managers / codegen
  { match: /^pip(\s|$)/, replace: 'tok pip$1' },
  { match: /^uv(\s|$)/, replace: 'tok uv$1' },
  { match: /^bundle(\s|$)/, replace: 'tok bundle$1' },
  { match: /^prisma(\s|$)/, replace: 'tok prisma$1' },
  { match: /^gem(\s|$)/, replace: 'tok gem$1' },

  // HTTP fetchers
  { match: /^curl(\s|$)/, replace: 'tok curl$1' },
  { match: /^wget(\s|$)/, replace: 'tok wget$1' },
];

// Commands containing shell composition tokens are not safe to rewrite at the front,
// because the matched word may be inside a subshell or after a logical operator. Pass through.
const COMPLEX_SHELL = /[|&;`$()<>]|\|\||&&|\\\n/;

export function rewriteCommand(input: string): RewriteOutcome {
  const cmd = input.trim();
  if (!cmd) return { kind: 'none' };

  // Don't try to rewrite shell pipelines, command substitutions, or chains.
  // Safe: a literal call like `git status` or `npm install --save react`.
  if (COMPLEX_SHELL.test(cmd)) return { kind: 'none' };

  for (const rule of RULES) {
    const re = typeof rule.match === 'string' ? new RegExp(`^${escapeRegex(rule.match)}`) : rule.match;
    if (!re.test(cmd)) continue;

    if (rule.replace === '__noop__') return { kind: 'none' };

    const rewritten = cmd.replace(re, rule.replace);
    if (rule.action === 'deny') return { kind: 'deny' };
    if (rule.action === 'ask') return { kind: 'ask', rewritten };
    return { kind: 'allow', rewritten };
  }

  return { kind: 'none' };
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
