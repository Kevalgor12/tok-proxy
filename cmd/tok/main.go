package main

import (
	"fmt"
	"os"

	"github.com/Kevalgor12/tok-proxy/internal/constants"
)

// Entry point. Full command dispatch is ported in a later phase (see MIGRATION.md).
// For now it wires up version/help so the module builds and runs end to end.
func main() {
	args := os.Args[1:]

	switch {
	case len(args) == 0, args[0] == "-h", args[0] == "--help":
		fmt.Print(help())
	case args[0] == "version", args[0] == "--version":
		fmt.Printf("tok %s\n", constants.Version)
	default:
		fmt.Fprintf(os.Stderr, "tok: %q is not ported yet (Go migration in progress)\n", args[0])
		os.Exit(constants.ExitNoRewrite)
	}
}

func help() string {
	return fmt.Sprintf("tok %s - CLI proxy that reduces LLM token consumption\n\n"+
		"Being rewritten in Go, one subsystem per phase (see MIGRATION.md).\n"+
		"Available now: version, help.\n", constants.Version)
}
