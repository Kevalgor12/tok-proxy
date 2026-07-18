#!/usr/bin/env node
'use strict';

// Runs automatically after `npm install`. Two jobs:
//   1. Build dist/ if it isn't there yet (so a fresh `git clone` + `npm install` just works).
//   2. Install the AI-tool hooks via `tok init` so token-saving starts immediately.
//
// It is deliberately best-effort: it never fails the install, and it no-ops cleanly
// in CI or when opted out with TOK_SKIP_SETUP=1. Re-run manually with `npm run setup`.

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

const ROOT = path.resolve(__dirname, '..');
const MAIN_JS = path.join(ROOT, 'dist', 'main.js');
const FORCE = process.argv.includes('--force');

function log(msg) {
  process.stdout.write(`tok: ${msg}\n`);
}

function skip(reason) {
  log(`setup skipped (${reason}). Run \`npm run setup\` when ready.`);
  process.exit(0);
}

try {
  if (!FORCE && (process.env.TOK_SKIP_SETUP === '1' || process.env.CI)) {
    skip(process.env.CI ? 'CI detected' : 'TOK_SKIP_SETUP=1');
  }

  // 1. Build if the compiled entrypoint is missing.
  if (!fs.existsSync(MAIN_JS)) {
    const tscEntry = path.join(ROOT, 'node_modules', 'typescript', 'bin', 'tsc');
    if (!fs.existsSync(tscEntry)) {
      skip('TypeScript not installed — run `npm install` (with dev deps) then `npm run build`');
    }
    log('building…');
    const build = spawnSync(process.execPath, [tscEntry, '--project', 'tsconfig.json'], {
      cwd: ROOT,
      stdio: 'inherit',
    });
    if (build.status !== 0 || !fs.existsSync(MAIN_JS)) {
      skip('build failed — run `npm run build` and check the output');
    }
  }

  // 2. Detect installed AI tools and wire up their hooks.
  log('installing hooks…');
  const init = spawnSync(process.execPath, [MAIN_JS, 'init'], { cwd: ROOT, stdio: 'inherit' });
  if (init.status !== 0) {
    skip('`tok init` did not complete — run `npm run setup` to retry');
  }

  log('ready. Restart your AI tool, then run `node dist/main.js doctor` to verify.');
} catch (err) {
  skip((err && err.message) || 'unexpected error');
}
