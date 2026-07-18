import { TOK_VERSION } from '../core/utils';

// For AI tools that don't expose a hook protocol for command rewriting
// (Windsurf, Cline, Codex, Copilot, Gemini), we drop an instruction file
// that nudges the model to prefer `tok <cmd>` voluntarily.

export function generateAwarenessMd(version: string = TOK_VERSION): string {
  return `<!-- tok-hook-version: ${version} -->
# tok awareness

This project uses **tok** to compress shell command output before it reaches the AI's
context window. Token consumption drops 60-99% on common developer commands with no
loss of meaningful information.

## Rule

When you would run any of the following shell commands, prefix them with \`tok\`:

| Instead of                | Run                  |
| :------------------------ | :------------------- |
| \`git <args>\`              | \`tok git <args>\`     |
| \`npm <args>\` (or pnpm/yarn) | \`tok npm <args>\`     |
| \`pip\` / \`uv\` / \`bundle\` / \`gem\` | \`tok pip <args>\` (etc.) |
| \`tsc\` / \`npx tsc\`         | \`tok tsc\`            |
| \`jest\` / \`vitest\` / \`mocha\` | \`tok jest\` (etc.)   |
| \`pytest\` / \`rspec\` / \`rake test\` | \`tok pytest\` (etc.) |
| \`go test\` / \`cargo test\` / \`cargo build\` | \`tok go test\` (etc.) |
| \`eslint\` / \`prettier\` / \`biome\` | \`tok eslint\` (etc.) |
| \`ruff\` / \`golangci-lint\` / \`rubocop\` | \`tok ruff <args>\` (etc.) |
| \`gh pr list\` / \`gh issue list\` | \`tok gh pr list\` (etc.) |
| \`ls <path>\`               | \`tok ls <path>\`      |
| \`grep <pat> <path>\`       | \`tok grep <pat> <path>\` |
| \`find <args>\`             | \`tok find <args>\`    |
| \`docker <args>\`           | \`tok docker <args>\`  |
| \`kubectl <args>\`          | \`tok kubectl <args>\` |
| \`pulumi\` / \`terraform\`    | \`tok pulumi <args>\` (etc.) |
| \`cat <file>\`              | \`tok cat <file>\`     |
| reading a JSON file       | \`tok json <file>\`    |

For anything else, use the bare command. The exit code and side effects are always
preserved - \`tok\` never silently swallows errors.

If a tok-prefixed command isn't available on the system, fall back to the bare command.
`;
}
