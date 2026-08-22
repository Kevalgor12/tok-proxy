package install

import (
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// Instruction-mode support for IDEs whose hooks cannot rewrite a command (Cursor, Antigravity,
// Windsurf, and the rest). tok can't transparently intercept them - only Claude Code exposes a
// hook that rewrites the command - so instead it writes a rule into the tool's real rules file
// asking the agent to prefix commands with `tok`. Best-effort: savings depend on the model
// following the rule.

const (
	ruleStart = "<!-- tok:start -->"
	ruleEnd   = "<!-- tok:end -->"
)

// tokRuleText is the shared guidance body (no markers, no frontmatter).
func tokRuleText() string {
	return strings.Join([]string{
		"## tok - compress shell output to save AI tokens",
		"",
		"Before running a shell command, prefix it with `tok` when the leading program is one of:",
		"git, npm/pnpm/yarn, pip/uv/bundle/gem/prisma, tsc, jest/vitest/mocha, pytest/rspec/rake test,",
		"go, cargo, eslint/prettier/biome, ruff/golangci-lint/rubocop, gh, docker/kubectl,",
		"pulumi/terraform, curl/wget, ls/cat/grep/find/diff. tok runs the real command and returns a",
		"compact summary (60-99% fewer tokens) with the exit code and side effects preserved.",
		"",
		"Examples: `git status` -> `tok git status`; `npm install` -> `tok npm install`; `tsc` -> `tok tsc`.",
		"In a compound command, prefix each recognized part: `cd api && npm ci` -> `cd api && tok npm ci`.",
		"Leave commands with pipes, redirects, or `$(...)` unprefixed. If `tok` isn't installed, use the bare command.",
	}, "\n")
}

// tokRuleBlock wraps the rule in markers for insertion into a shared rules file the user may
// also edit (Antigravity's AGENTS.md, Windsurf's global_rules.md).
func tokRuleBlock() string {
	return ruleStart + "\n" + tokRuleText() + "\n" + ruleEnd
}

// cursorRuleFile is a standalone Cursor rule (.mdc) that tok owns outright. The YAML
// frontmatter's alwaysApply keeps it active across every project.
func cursorRuleFile() string {
	return strings.Join([]string{
		"---",
		"description: Use tok to compress shell command output and save tokens",
		"alwaysApply: true",
		"---",
		tokRuleText(),
	}, "\n") + "\n"
}

// upsertRulesBlock inserts or refreshes tok's marker block in the rules file at path,
// creating the file if needed and leaving any surrounding user content untouched. Returns
// installed / updated / skipped / failed.
func upsertRulesBlock(path string) string {
	block := tokRuleBlock()
	existing, had := util.ReadFileIfExists(path)
	if !had {
		if !util.WriteFileSafe(path, block+"\n") {
			return "failed"
		}
		return "installed"
	}
	if start := strings.Index(existing, ruleStart); start >= 0 {
		end := strings.Index(existing, ruleEnd)
		if end < 0 || end < start {
			end = len(existing) - len(ruleEnd) // malformed: treat rest as the block
		}
		candidate := existing[:start] + block + existing[end+len(ruleEnd):]
		if candidate == existing {
			return "skipped"
		}
		if !util.WriteFileSafe(path, candidate) {
			return "failed"
		}
		return "updated"
	}
	sep := "\n\n"
	if strings.HasSuffix(existing, "\n") {
		sep = "\n"
	}
	if !util.WriteFileSafe(path, existing+sep+block+"\n") {
		return "failed"
	}
	return "updated"
}

// removeRulesBlock strips tok's marker block from the rules file at path. Returns true if it
// removed anything.
func removeRulesBlock(path string) bool {
	existing, had := util.ReadFileIfExists(path)
	if !had {
		return false
	}
	start := strings.Index(existing, ruleStart)
	end := strings.Index(existing, ruleEnd)
	if start < 0 || end < start {
		return false
	}
	cleaned := strings.TrimRight(existing[:start], "\n")
	rest := existing[end+len(ruleEnd):]
	if cleaned != "" && strings.TrimSpace(rest) != "" {
		cleaned += "\n"
	}
	cleaned += strings.TrimLeft(rest, "\n")
	util.WriteFileSafe(path, strings.TrimRight(cleaned, "\n"))
	return true
}

// hasRulesBlock reports whether tok's managed rule block is present in a shared rules file.
func hasRulesBlock(path string) bool {
	c, ok := util.ReadFileIfExists(path)
	return ok && strings.Contains(c, ruleStart)
}
