#!/usr/bin/env node
'use strict';

// Builds standalone tok binaries for every platform into build/, using @yao-pkg/pkg.
// Run `npm run build:binaries`. Each binary bundles the Node runtime, so end users
// need nothing installed — download one file and run it (like a Go/Rust binary).
//
// pkg cross-compiles all targets from any host (it fetches a prebuilt Node base per
// target the first time). Upload the resulting files to a GitHub Release; the
// installers (scripts/install.sh / install.ps1) download the right one by name.

const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const NODE = 'node22';
const TARGETS = [
  { target: `${NODE}-win-x64`, out: 'tok-windows-x64.exe' },
  { target: `${NODE}-linux-x64`, out: 'tok-linux-x64' },
  { target: `${NODE}-linux-arm64`, out: 'tok-linux-arm64' },
  { target: `${NODE}-macos-x64`, out: 'tok-macos-x64' },
  { target: `${NODE}-macos-arm64`, out: 'tok-macos-arm64' },
];

const entry = path.join(ROOT, 'dist', 'main.js');
if (!fs.existsSync(entry)) {
  console.error('dist/main.js not found — run `npm run build` first.');
  process.exit(1);
}
fs.mkdirSync(path.join(ROOT, 'build'), { recursive: true });

// Allow building a subset: `node scripts/build-binaries.js win linux`
const filter = process.argv.slice(2);
const selected = filter.length
  ? TARGETS.filter((t) => filter.some((f) => t.target.includes(f) || t.out.includes(f)))
  : TARGETS;

const built = [];
const failed = [];
for (const { target, out } of selected) {
  const outPath = path.join('build', out);
  console.log(`\n▶ building ${out}  (${target})`);
  try {
    // No --compress: pkg's GZip step only runs for the host platform and would block
    // cross-compiling the other targets from one machine.
    execSync(`npx pkg dist/main.js --targets ${target} --output ${outPath}`, {
      cwd: ROOT,
      stdio: 'inherit',
    });
    built.push(out);
  } catch (err) {
    // arm64 targets can't be cross-built from an x64 host (pkg runs an arm step) — that's
    // expected on most dev machines. CI builds them on native arm runners.
    failed.push(out);
    console.error(`✗ skipped ${out}: ${err.message.split('\n')[0]}`);
  }
}

console.log(`\nBuilt ${built.length}/${selected.length}: ${built.join(', ') || '(none)'}`);
if (failed.length) {
  console.log(`Skipped (build on a native host / CI): ${failed.join(', ')}`);
}
console.log('Binaries are in build/. Attach them to a GitHub Release.');
