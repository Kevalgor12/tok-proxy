#!/usr/bin/env bash
# Integration test for the Claude Code rewrite hook.
# Pipes synthetic PreToolUse payloads into the generated hook and asserts the
# output JSON contains hookSpecificOutput.updatedInput.command with the expected rewrite.
#
# Usage: bash scripts/test-tok-rewrite.sh
# Run from repo root after `npm run build`.

set -u

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# Convert to a path Node can resolve on Windows — Git Bash's /c/... or /e/... are not recognized.
if command -v cygpath >/dev/null 2>&1; then
  REPO_ROOT_NATIVE=$(cygpath -m "$REPO_ROOT")
else
  REPO_ROOT_NATIVE="$REPO_ROOT"
fi
HOOK_DIR="$REPO_ROOT/.tmp"
HOOK_PATH="$HOOK_DIR/tok-rewrite.sh"
mkdir -p "$HOOK_DIR"
if command -v cygpath >/dev/null 2>&1; then
  HOOK_PATH_NATIVE=$(cygpath -m "$HOOK_PATH")
else
  HOOK_PATH_NATIVE="$HOOK_PATH"
fi

# Generate the hook script using the installed dist build.
TOK_INVOCATION="node $REPO_ROOT_NATIVE/dist/main.js"
node -e "
  const path = require('path');
  const mod = path.join('$REPO_ROOT_NATIVE', 'dist', 'hooks', 'pre-tool-use.sh.js');
  const { generatePreToolUseHook } = require(mod);
  const fs = require('fs');
  fs.writeFileSync('$HOOK_PATH_NATIVE', generatePreToolUseHook(undefined, '$TOK_INVOCATION'));
  fs.chmodSync('$HOOK_PATH_NATIVE', 0o755);
"

PASS=0
FAIL=0

check() {
  local label="$1"
  local input="$2"
  local expect="$3"   # substring expected in updatedInput.command, or "PASSTHROUGH" for empty stdout
  local actual

  actual=$(echo "$input" | bash "$HOOK_PATH")

  if [ "$expect" = "PASSTHROUGH" ]; then
    if [ -z "$actual" ]; then
      echo "  PASS  $label"
      PASS=$((PASS+1))
    else
      echo "  FAIL  $label"
      echo "        expected empty output, got: $actual"
      FAIL=$((FAIL+1))
    fi
    return
  fi

  local rewritten
  rewritten=$(node -e "try{const o=JSON.parse(process.argv[1]);process.stdout.write(o?.hookSpecificOutput?.updatedInput?.command||'')}catch{process.exit(0)}" "$actual" 2>/dev/null)

  if echo "$rewritten" | grep -q "$expect"; then
    echo "  PASS  $label"
    PASS=$((PASS+1))
  else
    echo "  FAIL  $label"
    echo "        expected updatedInput.command to contain: $expect"
    echo "        got rewritten: $rewritten"
    echo "        raw output:    $actual"
    FAIL=$((FAIL+1))
  fi
}

echo "Testing hook: $HOOK_PATH"
echo

check "git status -> tok git status" \
  '{"tool_name":"Bash","tool_input":{"command":"git status"}}' \
  "tok git status"

check "npm install -> tok npm install" \
  '{"tool_name":"Bash","tool_input":{"command":"npm install react"}}' \
  "tok npm install react"

check "npx tsc -> tok tsc" \
  '{"tool_name":"Bash","tool_input":{"command":"npx tsc --noEmit"}}' \
  "tok tsc"

check "cd /tmp passes through" \
  '{"tool_name":"Bash","tool_input":{"command":"cd /tmp"}}' \
  "PASSTHROUGH"

check "shell pipeline passes through (safety)" \
  '{"tool_name":"Bash","tool_input":{"command":"git status | head"}}' \
  "PASSTHROUGH"

check "already-tok command passes through" \
  '{"tool_name":"Bash","tool_input":{"command":"tok git status"}}' \
  "PASSTHROUGH"

check "non-Bash tool passes through" \
  '{"tool_name":"Read","tool_input":{"command":"git status"}}' \
  "PASSTHROUGH"

echo
echo "Result: $PASS passed, $FAIL failed"
exit $FAIL
