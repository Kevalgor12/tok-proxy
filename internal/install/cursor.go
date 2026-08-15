package install

import "strings"

// GenerateCursorHook builds Cursor's preToolUse hook script. Cursor's protocol differs from
// Claude Code's: it wants {"permission":"allow","updated_input":{"command":"..."}} on a
// rewrite and a bare {} otherwise. The script shells out to tok for the rewrite decision.
func GenerateCursorHook(version, tokBin string) string {
	script := `#!/usr/bin/env bash
# tok-hook-version: {{VERSION}}
# Cursor preToolUse hook - rewrites shell commands to use tok for token savings.
# Output protocol: {"permission":"allow","updated_input":{"command":"..."}} on rewrite,
# {} otherwise.

INPUT=$(cat)

TOK_BIN_STRING="{{TOKBIN}}"
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
`
	return strings.NewReplacer("{{VERSION}}", version, "{{TOKBIN}}", tokBin).Replace(script)
}
