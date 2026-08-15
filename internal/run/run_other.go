//go:build !windows

package run

import "os/exec"

// buildCmd execs the command directly - no shell, matching the Node posix path.
func buildCmd(cmd string, args []string) *exec.Cmd {
	return exec.Command(cmd, args...)
}
