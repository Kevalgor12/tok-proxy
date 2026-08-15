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

// A command with any shell-composition token isn't safe to rewrite at the front, because
// the matched word may be inside a subshell or after a logical operator. Pass it through.
var complexShell = regexp.MustCompile("[|&;`$()<>]|" + `\|\||&&|\\` + "\n")

func RewriteCommand(input string) Outcome {
	cmd := strings.TrimSpace(input)
	if cmd == "" || complexShell.MatchString(cmd) {
		return Outcome{Kind: "none"}
	}
	for _, r := range rules {
		if !r.re.MatchString(cmd) {
			continue
		}
		if r.replace == "__noop__" {
			return Outcome{Kind: "none"}
		}
		rewritten := r.re.ReplaceAllString(cmd, r.replace)
		switch r.action {
		case "deny":
			return Outcome{Kind: "deny"}
		case "ask":
			return Outcome{Kind: "ask", Rewritten: rewritten}
		default:
			return Outcome{Kind: "allow", Rewritten: rewritten}
		}
	}
	return Outcome{Kind: "none"}
}
