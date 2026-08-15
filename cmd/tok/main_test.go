package main

import (
	"strings"
	"testing"
)

func TestParseGlobalFlags(t *testing.T) {
	f, rest := parseGlobalFlags([]string{"-u", "git", "status", "--no-cache"})
	if !f.ultra || !f.noCache {
		t.Errorf("flags = %+v", f)
	}
	if len(rest) != 2 || rest[0] != "git" || rest[1] != "status" {
		t.Errorf("rest = %v", rest)
	}
	if f2, _ := parseGlobalFlags([]string{"-vv"}); f2.verbose != 2 {
		t.Errorf("verbose = %d", f2.verbose)
	}
}

func TestKwargHelpers(t *testing.T) {
	args := []string{"--model", "opus", "--export=csv", "stats"}
	if v, ok := getKwarg(args, "model"); !ok || v != "opus" {
		t.Errorf("model = %q %v", v, ok)
	}
	if v, ok := getKwarg(args, "export"); !ok || v != "csv" {
		t.Errorf("export = %q %v", v, ok)
	}
	if _, ok := getKwarg(args, "missing"); ok {
		t.Error("missing should be absent")
	}
	if !hasFlag([]string{"--graph"}, "graph") {
		t.Error("hasFlag graph")
	}
	if exportKind([]string{"--export", "json"}) != "json" {
		t.Error("exportKind json")
	}
	if exportKind([]string{"--export", "xml"}) != "" {
		t.Error("exportKind invalid should be empty")
	}
	if kwargInt([]string{"--input", "42"}, "input") != 42 {
		t.Error("kwargInt")
	}
}

func TestHelpText(t *testing.T) {
	h := helpText()
	for _, want := range []string{"PROXY COMMANDS", "ANALYTICS COMMANDS", "usage ingest", "hook claude"} {
		if !strings.Contains(h, want) {
			t.Errorf("help missing %q", want)
		}
	}
}
