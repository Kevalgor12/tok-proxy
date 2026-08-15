package filter

import (
	"strings"
	"testing"

	"github.com/Kevalgor12/tok-proxy/internal/config"
)

func TestLangFromPath(t *testing.T) {
	cases := map[string]string{
		"main.go": "go", "app.tsx": "tsx", "x.mjs": "js", "y.PY": "py", "noext": "", "a.unknown": "",
	}
	for in, want := range cases {
		if got := LangFromPath(in); got != want {
			t.Errorf("LangFromPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFilterCodeMinimalStripsComments(t *testing.T) {
	src := "// header\nfunc f() {} // trailing\n/* block */\nx := 1\n"
	out := FilterCode(src, "go", config.FilterMinimal)
	if strings.Contains(out, "header") || strings.Contains(out, "trailing") || strings.Contains(out, "block") {
		t.Errorf("comments not stripped:\n%s", out)
	}
	if !strings.Contains(out, "x := 1") {
		t.Errorf("code dropped:\n%s", out)
	}
}

func TestFilterCodeNoneIsIdentity(t *testing.T) {
	src := "// keep me\nx := 1\n"
	if FilterCode(src, "go", config.FilterNone) != src {
		t.Error("FilterNone should return source unchanged")
	}
}

// The // inside a string literal must not be treated as a comment.
func TestFilterCodeStringAware(t *testing.T) {
	src := `url := "http://example.com" // real comment`
	out := FilterCode(src, "go", config.FilterMinimal)
	if !strings.Contains(out, `"http://example.com"`) {
		t.Errorf("string with // was corrupted:\n%s", out)
	}
	if strings.Contains(out, "real comment") {
		t.Errorf("trailing comment not stripped:\n%s", out)
	}
}

func TestDeduplicateLines(t *testing.T) {
	out := DeduplicateLines("error: boom\nerror: boom\nerror: boom\nok\n")
	if !strings.Contains(out, "error: boom (×3)") {
		t.Errorf("expected a ×3 count, got:\n%s", out)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("unique line dropped:\n%s", out)
	}
}
