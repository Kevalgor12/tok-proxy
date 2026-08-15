package registry

import "testing"

func TestRewriteCommand(t *testing.T) {
	cases := []struct{ in, kind, rewritten string }{
		{"git status", "allow", "tok git status"},
		{"git", "allow", "tok git"},
		{"npm install --save react", "allow", "tok npm install --save react"},
		{"npx tsc --noEmit", "allow", "tok tsc --noEmit"},
		{"pulumi preview --diff", "allow", "tok pulumi preview --diff"},
		{"pulumi stack", "none", ""}, // only preview/up/destroy are rewritten
		{"go test ./...", "allow", "tok go test ./..."},
		{"tok git status", "none", ""}, // already a tok command
		{"git log | head", "none", ""}, // pipeline
		{"cd /tmp && ls", "none", ""},  // chain
		{"whoami", "none", ""},         // no rule
		{"", "none", ""},
	}
	for _, c := range cases {
		got := RewriteCommand(c.in)
		if got.Kind != c.kind || got.Rewritten != c.rewritten {
			t.Errorf("RewriteCommand(%q) = {%q, %q}, want {%q, %q}", c.in, got.Kind, got.Rewritten, c.kind, c.rewritten)
		}
	}
}
