// Package run executes the real shell command behind a tok proxy call and captures its
// output, plus the tee (save full output of short failures) and hook-version check.
package run

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Kevalgor12/tok-proxy/internal/constants"
	"github.com/Kevalgor12/tok-proxy/internal/store"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	ExecMs   int
}

// Run executes the command and captures stdout/stderr/exit code. On Windows it goes
// through cmd.exe (matching the Node shell path); elsewhere it execs directly.
func Run(cmd string, args []string) Result {
	start := time.Now()
	c := buildCmd(cmd, args)
	var stdout, stderr strings.Builder
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	execMs := int(time.Since(start).Milliseconds())

	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			// spawn error (command not found, etc.)
			util.AppendErrorLog("runner.spawn", err)
			exitCode = 1
		}
	}
	return Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode, ExecMs: execMs}
}

func teeDir() string { return filepath.Join(util.DataDir(), "tee") }

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// MaybeTee saves the full output to ~/.tok/tee when a command fails but its filtered
// output is too short to debug from, and points the caller at the file.
func MaybeTee(cmdType string, exitCode int, filteredOutput, rawCombined string) string {
	if exitCode == 0 || len(filteredOutput) >= 500 {
		return filteredOutput
	}
	util.EnsureDir(teeDir())
	name := fmt.Sprintf("%d_%s.log", time.Now().Unix(), unsafeName.ReplaceAllString(cmdType, "-"))
	teePath := filepath.Join(teeDir(), name)
	if err := os.WriteFile(teePath, []byte(rawCombined), 0o644); err != nil {
		util.AppendErrorLog("maybeTee", err)
		return filteredOutput
	}
	return fmt.Sprintf("%s\n[Full output: %s]", filteredOutput, teePath)
}

// CleanOldTeeFiles removes tee logs older than a day.
func CleanOldTeeFiles() {
	entries, err := os.ReadDir(teeDir())
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(teeDir(), e.Name()))
	}
}

// CheckHookVersion warns once a day if the installed hooks predate this build.
func CheckHookVersion(s *store.Store) {
	if last, ok := s.GetMeta("last_hook_check"); ok {
		if t, err := time.Parse(time.RFC3339Nano, last); err == nil && time.Since(t) < 24*time.Hour {
			return
		}
	}
	s.SetMeta("last_hook_check", util.NowIso())

	hookV, ok := s.GetMeta("hook_version")
	if !ok || hookV == "" {
		return
	}
	if hookV != constants.Version {
		fmt.Fprintf(os.Stderr, "tok: hooks are outdated (v%s -> v%s). Run: tok init\n", hookV, constants.Version)
	}
}
