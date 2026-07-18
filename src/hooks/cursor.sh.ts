import { TOK_VERSION } from '../core/utils';

// Cursor's preToolUse hook expects JSON output with `permission` and `updated_input` keys
// (different from Claude Code's `hookSpecificOutput.updatedInput`).
// On no-rewrite, Cursor requires an empty JSON object `{}` rather than non-zero exit.

export function generateCursorHook(version: string = TOK_VERSION, tokBin: string = 'tok'): string {
  return `#!/usr/bin/env bash
# tok-hook-version: ${version}
# Cursor preToolUse hook - rewrites shell commands to use tok for token savings.
# Output protocol: {"permission":"allow","updated_input":{"command":"..."}} on rewrite,
# {} otherwise.

INPUT=$(cat)

TOK_BIN_STRING="${tokBin}"
read -r TOK_FIRST _ <<<"$TOK_BIN_STRING"
if ! command -v "$TOK_FIRST" >/dev/null 2>&1; then
  echo '{}'
  exit 0
fi

CMD=$(node -e '
  let buf = "";
  process.stdin.on("data", d => buf += d);
  process.stdin.on("end", () => {
    try {
      const o = JSON.parse(buf);
      process.stdout.write(String(o?.tool_input?.command || ""));
    } catch { process.exit(0); }
  });
' <<<"$INPUT")

if [ -z "$CMD" ]; then
  echo '{}'
  exit 0
fi

REWRITTEN=$($TOK_BIN_STRING rewrite "$CMD" 2>/dev/null) || { echo '{}'; exit 0; }

if [ "$CMD" = "$REWRITTEN" ]; then
  echo '{}'
  exit 0
fi

node -e '
  const out = { permission: "allow", updated_input: { command: process.argv[1] } };
  process.stdout.write(JSON.stringify(out));
' "$REWRITTEN"
`;
}
