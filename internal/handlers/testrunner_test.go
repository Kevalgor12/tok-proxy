package handlers

import (
	"strings"
	"testing"
)

func TestTestRunnerRegex(t *testing.T) {
	raw := "Tests: 1 failed, 2 passed, 3 total\nFAIL src/a.test.js\n"
	got := formatFromRegex(raw, false, 100)
	for _, want := range []string{"1 failed test", "FAIL src/a.test.js", "Summary: 2 passed, 1 failed (3 total)"} {
		if !strings.Contains(got, want) {
			t.Errorf("regex %q missing %q", got, want)
		}
	}
	if s := formatFromRegex("Tests: 5 passed, 5 total\n", false, 50); !strings.Contains(s, "All 5 tests passed") {
		t.Errorf("pass = %q", s)
	}
	if s := formatFromRegex("Tests: 1 failed, 2 passed, 3 total\n", true, 0); s != "✗1/3" {
		t.Errorf("ultra = %q", s)
	}
}

func TestTestRunnerJSON(t *testing.T) {
	p := tryParseJestJSON(`some log noise
{"numTotalTests":3,"numPassedTests":3,"numFailedTests":0}`)
	if p == nil {
		t.Fatal("expected JSON to parse past the log prefix")
	}
	if s := formatFromJSON(p, false, 200); !strings.Contains(s, "All 3 tests passed") {
		t.Errorf("json pass = %q", s)
	}
	fail := &jestJSON{NumTotalTests: 2, NumPassedTests: 1, NumFailedTests: 1}
	if s := formatFromJSON(fail, true, 0); s != "✗1/2" {
		t.Errorf("json ultra = %q", s)
	}
}
