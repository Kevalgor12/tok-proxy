package handlers

import (
	"strings"
	"testing"
)

func TestSummarizeDiagnostics(t *testing.T) {
	rust := "error[E0308]: mismatched types\nerror[E0425]: cannot find value x\nwarning: unused variable\n"
	got := summarizeDiagnostics(rust, "cargo build", false, 1)
	for _, want := range []string{"2 errors, 1 warning (cargo build):", "[E0308]", "[E0425]"} {
		if !strings.Contains(got, want) {
			t.Errorf("rust diag %q missing %q", got, want)
		}
	}
	if u := summarizeDiagnostics(rust, "cargo build", true, 1); u != "2E/1W" {
		t.Errorf("rust ultra = %q, want 2E/1W", u)
	}

	ruff := "app/main.py:10:5: F401 'os' imported but unused\napp/main.py:12:1: E302 expected 2 blank lines\n"
	got = summarizeDiagnostics(ruff, "ruff", false, 1)
	for _, want := range []string{"2 errors (ruff):", "F401", "app/main.py"} {
		if !strings.Contains(got, want) {
			t.Errorf("ruff diag %q missing %q", got, want)
		}
	}

	if s := summarizeDiagnostics("compiled successfully\n", "cargo build", false, 0); s != "✓ cargo build: clean" {
		t.Errorf("clean = %q", s)
	}
}

func TestSummarizeInfra(t *testing.T) {
	pulumi := "Resources: + 3 to create, ~ 1 to update, - 2 to delete\n"
	if s := summarizePulumi(pulumi, false, 0); s != "Resources: +3 create, ~1 update, -2 delete" {
		t.Errorf("pulumi = %q", s)
	}
	if s := summarizePulumi(pulumi, true, 0); s != "+3~1-2" {
		t.Errorf("pulumi ultra = %q", s)
	}
	if s := summarizePulumi("error: boom\n", false, 1); !strings.Contains(s, "✗ pulumi failed") {
		t.Errorf("pulumi err = %q", s)
	}

	tf := "Plan: 3 to add, 1 to change, 2 to destroy\n"
	if s := summarizeTerraform(tf, false, 0); s != "Plan: +3 add, ~1 change, -2 destroy" {
		t.Errorf("terraform = %q", s)
	}
	if s := summarizeTerraform("No changes. Your infrastructure matches.\n", false, 0); s != "✓ no changes" {
		t.Errorf("terraform no-change = %q", s)
	}
}

func TestFormatPs(t *testing.T) {
	raw := "CONTAINER ID   IMAGE   STATUS\nabc123def456   nginx   Up 2 hours\nxyz789   redis   Exited (0)\n"
	if s := formatPs(raw, false); !strings.Contains(s, "2 containers: 1 running, 1 stopped") {
		t.Errorf("ps = %q", s)
	}
	if s := formatPs(raw, true); s != "1↑/1↓" {
		t.Errorf("ps ultra = %q", s)
	}
	if s := formatPs("HEADER\n", false); s != "no containers" {
		t.Errorf("ps empty = %q", s)
	}
}

func TestSummarizeBody(t *testing.T) {
	if s := summarizeBody("", false); s != "(empty response)" {
		t.Errorf("empty = %q", s)
	}
	if s := summarizeBody(`{"a":1}`, false); s != `{"a":1}` {
		t.Errorf("small json = %q", s)
	}
	big := `{"data":"` + strings.Repeat("x", 900) + `"}`
	got := summarizeBody(big, false)
	if !strings.Contains(got, "JSON response") || !strings.Contains(got, `"data": "string"`) {
		t.Errorf("big json = %q", got)
	}
}

func TestGroupViolations(t *testing.T) {
	raw := "/path/to/file.js\n  1:5  error  Unexpected console statement  no-console\n  2:1  warning  Missing semicolon  semi\n"
	got := groupViolations(raw, false)
	for _, want := range []string{"2 violations in 1 file", "no-console: 1 (50.0%)", "/path/to/file.js: 2 violations"} {
		if !strings.Contains(got, want) {
			t.Errorf("lint %q missing %q", got, want)
		}
	}
	if s := groupViolations("all good\n", false); s != "✓ no issues" {
		t.Errorf("clean lint = %q", s)
	}
}

func TestSummarizePytest(t *testing.T) {
	if s := summarizePytest("===== 5 passed in 0.3s =====\n", false, 300); !strings.Contains(s, "✓ All 5 tests passed") {
		t.Errorf("pass = %q", s)
	}
	fail := "2 failed, 3 passed\nFAILED tests/test_x.py::test_a\nFAILED tests/test_x.py::test_b\n"
	got := summarizePytest(fail, false, 300)
	if !strings.Contains(got, "2 failed tests:") || !strings.Contains(got, "test_a") || !strings.Contains(got, "Summary: 3 passed, 2 failed (5 total)") {
		t.Errorf("fail = %q", got)
	}
}

func TestSummarizeGoTest(t *testing.T) {
	pass := "ok  github.com/foo/bar 0.5s\nok  github.com/foo/baz 0.2s\n"
	if s := summarizeGoTest(pass, false, 700); !strings.Contains(s, "✓ tests passed (2 packages") {
		t.Errorf("pass = %q", s)
	}
	fail := "--- FAIL: TestA (0.00s)\nFAIL github.com/foo/bar 0.1s\n"
	got := summarizeGoTest(fail, false, 100)
	if !strings.Contains(got, "1 failed test in 1 package") || !strings.Contains(got, "✗ TestA") {
		t.Errorf("fail = %q", got)
	}
}
