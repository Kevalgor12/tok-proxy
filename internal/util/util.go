// Package util holds the generic, domain-free helpers the rest of tok builds on:
// paths, file I/O, formatting, and time math. Nothing here knows about commands or hooks.
package util

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// TokHome is the single directory tok keeps everything in (~/.tok), for data and config.
//
// On Windows this deliberately avoids %LOCALAPPDATA%/%APPDATA%. Store/MSIX-packaged apps
// (Claude Desktop, Store VS Code) run sandboxed, and Windows redirects those folders into the
// package's private LocalCache - so a hook spawned by Claude would write savings somewhere the
// terminal never reads. The home directory isn't redirected, so ~/.tok is the same in both.
// Override with TOK_HOME.
func TokHome() string {
	if h := os.Getenv("TOK_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tok")
}

func DataDir() string   { return TokHome() }
func ConfigDir() string { return TokHome() }

// EnsureDir makes a directory (and parents), ignoring the error - a later write will fail
// loudly if it mattered, and a missing dir is never worth crashing over.
func EnsureDir(dir string) { _ = os.MkdirAll(dir, 0o755) }

func FileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ReadFileIfExists returns the contents and true, or "" and false if the file can't be read.
func ReadFileIfExists(p string) (string, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// WriteFileSafe creates parent dirs and writes the file, logging (not returning) failures.
func WriteFileSafe(p, content string) bool {
	EnsureDir(filepath.Dir(p))
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		AppendErrorLog("writeFile", err)
		return false
	}
	return true
}

// ChmodIfPosix sets the file mode on Unix and no-ops on Windows, where mode bits don't apply.
func ChmodIfPosix(p string, mode os.FileMode) {
	if runtime.GOOS == "windows" {
		return
	}
	_ = os.Chmod(p, mode)
}

// AppendErrorLog records a background failure to ~/.tok/errors.log. It must never crash.
func AppendErrorLog(scope string, err error) {
	defer func() { _ = recover() }()
	EnsureDir(DataDir())
	f, e := os.OpenFile(filepath.Join(DataDir(), "errors.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if e != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] [%s] %v\n", NowIso(), scope, err)
}

// NowIso returns the current UTC time as an ISO-8601 string with milliseconds, matching
// JavaScript's Date.toISOString() so timestamps from the old Node build stay comparable.
func NowIso() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// ShortHash is a stable 16-char content fingerprint used by the output cache.
func ShortHash(input string) string {
	sum := sha1.Sum([]byte(input))
	return hex.EncodeToString(sum[:])[:16]
}

// Unique returns the input with duplicates removed, preserving first-seen order.
func Unique[T comparable](in []T) []T {
	seen := make(map[T]struct{}, len(in))
	out := make([]T, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
