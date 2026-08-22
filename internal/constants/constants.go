// Package constants holds the cross-cutting values: the version and the hook
// exit-code protocol. Anything domain-specific lives beside the package that owns it.
package constants

// Version is the tok release line. The Go rewrite starts the 0.4.x series.
const Version = "0.4.4"

// Exit codes for `tok rewrite` - the contract the shell hook depends on. Kept identical
// to the Node version so a hook installed before the migration keeps working.
const (
	ExitAllow     = 0 // a rule matched: emit the rewrite and auto-approve it
	ExitNoRewrite = 1 // no rule matched: run the command unchanged
	ExitDeny      = 2 // a deny rule matched: hand off to the tool's native deny
	ExitAsk       = 3 // a rule matched: emit the rewrite but keep the user prompt
)
