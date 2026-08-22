// Package registry is the single source of truth for command rewrite rules: which shell
// commands get proxied through tok, and how their leading token is rewritten. The hook
// and `tok rewrite` both decide here.
package registry

import (
	"regexp"
	"strings"
)

// Outcome.Kind is one of: "allow" (rewrite + auto-approve), "ask" (rewrite, keep prompt),
// "deny" (hand off to the tool's native deny), or "none" (pass through unchanged).
type Outcome struct {
	Kind      string
	Rewritten string
}

type rule struct {
	re      *regexp.Regexp
	replace string
	action  string // "", "allow", "ask", "deny"
}

// Order matters: more specific rules (npx tsc, pulumi preview) come before generic ones.
var rules = []rule{
	// Bypass - already a tok command.
	{regexp.MustCompile(`^tok(\s|$)`), "__noop__", ""},

	// npx invocations of supported tools.
	{regexp.MustCompile(`^npx\s+tsc(\s|$)`), "tok tsc$1", ""},
	{regexp.MustCompile(`^npx\s+jest(\s|$)`), "tok jest$1", ""},
	{regexp.MustCompile(`^npx\s+vitest(\s|$)`), "tok vitest$1", ""},
	{regexp.MustCompile(`^npx\s+mocha(\s|$)`), "tok mocha$1", ""},
	{regexp.MustCompile(`^npx\s+eslint(\s|$)`), "tok eslint$1", ""},
	{regexp.MustCompile(`^npx\s+prettier(\s|$)`), "tok prettier$1", ""},
	{regexp.MustCompile(`^npx\s+biome(\s|$)`), "tok biome$1", ""},
	{regexp.MustCompile(`^npx\s+prisma(\s|$)`), "tok prisma$1", ""},
	{regexp.MustCompile(`^(?:npx\s+)?playwright\s+test(\s|$)`), "tok playwright test$1", ""},
	{regexp.MustCompile(`^(?:npx\s+)?next\s+build(\s|$)`), "tok next build$1", ""},
	{regexp.MustCompile(`^(?:npx\s+)?next\s+lint(\s|$)`), "tok next lint$1", ""},

	// Targeted subcommands only - don't mis-summarize unrelated tasks like `pulumi stack`.
	{regexp.MustCompile(`^rake\s+test(\s|$)`), "tok rake test$1", ""},
	{regexp.MustCompile(`^pulumi\s+(preview|up|destroy)(\s|$)`), "tok pulumi $1$2", ""},
	{regexp.MustCompile(`^terraform\s+(plan|apply)(\s|$)`), "tok terraform $1$2", ""},

	// Direct invocations.
	{regexp.MustCompile(`^git(\s|$)`), "tok git$1", ""},
	{regexp.MustCompile(`^npm(\s|$)`), "tok npm$1", ""},
	{regexp.MustCompile(`^pnpm(\s|$)`), "tok pnpm$1", ""},
	{regexp.MustCompile(`^yarn(\s|$)`), "tok yarn$1", ""},
	{regexp.MustCompile(`^tsc(\s|$)`), "tok tsc$1", ""},
	{regexp.MustCompile(`^jest(\s|$)`), "tok jest$1", ""},
	{regexp.MustCompile(`^vitest(\s|$)`), "tok vitest$1", ""},
	{regexp.MustCompile(`^mocha(\s|$)`), "tok mocha$1", ""},
	{regexp.MustCompile(`^eslint(\s|$)`), "tok eslint$1", ""},
	{regexp.MustCompile(`^prettier(\s|$)`), "tok prettier$1", ""},
	{regexp.MustCompile(`^biome(\s|$)`), "tok biome$1", ""},
	{regexp.MustCompile(`^docker(\s|$)`), "tok docker$1", ""},
	{regexp.MustCompile(`^kubectl(\s|$)`), "tok kubectl$1", ""},
	{regexp.MustCompile(`^ls(\s|$)`), "tok ls$1", ""},
	{regexp.MustCompile(`^grep(\s|$)`), "tok grep$1", ""},
	{regexp.MustCompile(`^rg(\s|$)`), "tok grep$1", ""},
	{regexp.MustCompile(`^find(\s|$)`), "tok find$1", ""},

	// GitHub CLI.
	{regexp.MustCompile(`^gh(\s|$)`), "tok gh$1", ""},

	// Non-JS test runners.
	{regexp.MustCompile(`^pytest(\s|$)`), "tok pytest$1", ""},
	{regexp.MustCompile(`^rspec(\s|$)`), "tok rspec$1", ""},

	// Go / Rust toolchains - the handler dispatches per subcommand.
	{regexp.MustCompile(`^go(\s|$)`), "tok go$1", ""},
	{regexp.MustCompile(`^cargo(\s|$)`), "tok cargo$1", ""},

	// Linters / compilers for other ecosystems.
	{regexp.MustCompile(`^ruff(\s|$)`), "tok ruff$1", ""},
	{regexp.MustCompile(`^golangci-lint(\s|$)`), "tok golangci-lint$1", ""},
	{regexp.MustCompile(`^rubocop(\s|$)`), "tok rubocop$1", ""},

	// Package managers / codegen.
	{regexp.MustCompile(`^pip(\s|$)`), "tok pip$1", ""},
	{regexp.MustCompile(`^uv(\s|$)`), "tok uv$1", ""},
	{regexp.MustCompile(`^bundle(\s|$)`), "tok bundle$1", ""},
	{regexp.MustCompile(`^prisma(\s|$)`), "tok prisma$1", ""},
	{regexp.MustCompile(`^gem(\s|$)`), "tok gem$1", ""},

	// HTTP fetchers.
	{regexp.MustCompile(`^curl(\s|$)`), "tok curl$1", ""},
	{regexp.MustCompile(`^wget(\s|$)`), "tok wget$1", ""},
}

// segUnsafe flags the shell tokens that make a single segment unsafe to rewrite at the
// front - a pipe, redirect, subshell, command substitution, background, or newline. The word
// tok would rewrite might feed a pipe or land inside a subshell, so those segments pass
// through untouched. (Sequential operators && || ; are handled by splitting, not here.)
var segUnsafe = regexp.MustCompile("[|&$()<>\n\x60]")

// RewriteCommand decides how a shell command should be proxied through tok. Real agent
// commands are usually compound ("cd api && npm ci && npm run build"), so the command is
// split on the top-level sequential operators (&& || ;) and each recognized, side-effect-free
// segment is rewritten independently; segments with pipes/redirects/subshells, or commands
// tok doesn't know, are left exactly as they were. tok preserves every command's exit code,
// so && / || / ; chaining behaves identically after the rewrite.
func RewriteCommand(input string) Outcome {
	cmd := strings.TrimSpace(input)
	if cmd == "" {
		return Outcome{Kind: "none"}
	}
	segs, seps, ok := splitOperators(cmd)
	if !ok {
		return Outcome{Kind: "none"} // unbalanced quotes - don't risk a bad rewrite
	}

	anyRewritten, anyAsk := false, false
	for i, seg := range segs {
		rewritten, kind := rewriteSegment(seg)
		switch kind {
		case "deny":
			return Outcome{Kind: "deny"}
		case "ask":
			segs[i], anyRewritten, anyAsk = rewritten, true, true
		case "allow":
			segs[i], anyRewritten = rewritten, true
		}
	}
	if !anyRewritten {
		return Outcome{Kind: "none"}
	}

	var b strings.Builder
	for i, seg := range segs {
		b.WriteString(seg)
		if i < len(seps) {
			b.WriteString(seps[i])
		}
	}
	kind := "allow"
	if anyAsk {
		kind = "ask"
	}
	return Outcome{Kind: kind, Rewritten: b.String()}
}

// rewriteSegment rewrites one segment's leading command if it is recognized and safe,
// preserving the segment's surrounding whitespace. Returns the (possibly unchanged) segment
// and the decision: "allow" / "ask" / "deny" when a rule matched, "none" otherwise.
func rewriteSegment(seg string) (string, string) {
	trimmed := strings.TrimSpace(seg)
	if trimmed == "" || segUnsafe.MatchString(trimmed) {
		return seg, "none"
	}
	for _, r := range rules {
		if !r.re.MatchString(trimmed) {
			continue
		}
		if r.replace == "__noop__" {
			return seg, "none" // already a tok command
		}
		rewritten := r.re.ReplaceAllString(trimmed, r.replace)
		lead := seg[:len(seg)-len(strings.TrimLeft(seg, " \t"))]
		trail := seg[len(strings.TrimRight(seg, " \t")):]
		switch r.action {
		case "deny":
			return seg, "deny"
		case "ask":
			return lead + rewritten + trail, "ask"
		default:
			return lead + rewritten + trail, "allow"
		}
	}
	return seg, "none"
}

// splitOperators splits cmd on the top-level sequential operators && || ; while respecting
// single/double quotes and backslash escapes, so a separator inside a quoted argument is not
// treated as a split point. It returns the segments, the separators between them
// (len(seps) == len(segs)-1), and ok=false if the quoting is unbalanced.
func splitOperators(cmd string) (segs []string, seps []string, ok bool) {
	var buf strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(cmd); {
		c := cmd[i]
		switch {
		case c == '\\' && i+1 < len(cmd):
			buf.WriteByte(c)
			buf.WriteByte(cmd[i+1])
			i += 2
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			buf.WriteByte(c)
			i++
		case c == '"' && !inSingle:
			inDouble = !inDouble
			buf.WriteByte(c)
			i++
		case !inSingle && !inDouble && c == '&' && i+1 < len(cmd) && cmd[i+1] == '&':
			segs, seps = append(segs, buf.String()), append(seps, "&&")
			buf.Reset()
			i += 2
		case !inSingle && !inDouble && c == '|' && i+1 < len(cmd) && cmd[i+1] == '|':
			segs, seps = append(segs, buf.String()), append(seps, "||")
			buf.Reset()
			i += 2
		case !inSingle && !inDouble && c == ';':
			segs, seps = append(segs, buf.String()), append(seps, ";")
			buf.Reset()
			i++
		default:
			buf.WriteByte(c)
			i++
		}
	}
	if inSingle || inDouble {
		return nil, nil, false
	}
	return append(segs, buf.String()), seps, true
}
