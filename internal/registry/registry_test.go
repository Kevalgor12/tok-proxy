package registry

import "testing"

func TestRewriteCommand(t *testing.T) {
	cases := []struct{ in, kind, rewritten string }{
		// Single commands - unchanged behavior.
		{"git status", "allow", "tok git status"},
		{"git", "allow", "tok git"},
		{"npm install --save react", "allow", "tok npm install --save react"},
		{"npx tsc --noEmit", "allow", "tok tsc --noEmit"},
		{"pulumi preview --diff", "allow", "tok pulumi preview --diff"},
		{"pulumi stack", "none", ""}, // only preview/up/destroy are rewritten
		{"go test ./...", "allow", "tok go test ./..."},
		{"tok git status", "none", ""}, // already a tok command
		{"whoami", "none", ""},         // no rule
		{"", "none", ""},

		// Pipes / redirects / subshells stay untouched (the risky forms).
		{"git log | head", "none", ""},
		{"git status | grep modified", "none", ""},
		{"npm run build 2>&1", "none", ""},
		{"npm run build > out.log", "none", ""},
		{"git checkout $(git rev-parse HEAD)", "none", ""},

		// Compound commands - the recognized, safe segments get rewritten.
		{"cd /tmp && ls", "allow", "cd /tmp && tok ls"},
		{"cd api && npm ci && npm run build", "allow", "cd api && tok npm ci && tok npm run build"},
		{"git add -A && git commit -m \"msg\"", "allow", "tok git add -A && tok git commit -m \"msg\""},
		{"mkdir foo; cd foo; npm init -y", "allow", "mkdir foo; cd foo; tok npm init -y"},
		{"npm ci || echo failed", "allow", "tok npm ci || echo failed"},
		{"git status && echo done", "allow", "tok git status && echo done"},

		// A separator inside quotes must not be treated as a split point.
		{"echo \"a && b\" && git status", "allow", "echo \"a && b\" && tok git status"},

		// Nothing recognized in a compound - leave it entirely alone.
		{"cd foo && mkdir bar", "none", ""},

		// A compound where one segment pipes: rewrite the safe one, keep the piped one raw.
		{"npm ci && git log | grep fix", "allow", "tok npm ci && git log | grep fix"},

		// Unbalanced quotes - refuse to touch it.
		{"git status \"", "none", ""},
	}
	for _, c := range cases {
		got := RewriteCommand(c.in)
		if got.Kind != c.kind || got.Rewritten != c.rewritten {
			t.Errorf("RewriteCommand(%q) = {%q, %q}, want {%q, %q}", c.in, got.Kind, got.Rewritten, c.kind, c.rewritten)
		}
	}
}
