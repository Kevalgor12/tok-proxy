//go:build windows

package run

import (
	"os/exec"
	"strings"
	"syscall"
)

// buildCmd runs the command through cmd.exe. The raw command line is set via SysProcAttr
// so Go's own arg quoting doesn't mangle the already-quoted shell string.
func buildCmd(cmd string, args []string) *exec.Cmd {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, cmd)
	for _, a := range args {
		parts = append(parts, quoteWinArg(a))
	}
	c := exec.Command("cmd")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /d /s /c ` + strings.Join(parts, " ")}
	return c
}

func quoteWinArg(a string) string {
	if a == "" {
		return `""`
	}
	if !strings.ContainsAny(a, " \t\"&<>|^()") {
		return a
	}
	return `"` + strings.ReplaceAll(a, `"`, `""`) + `"`
}
