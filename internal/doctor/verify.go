package doctor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/constants"
	"github.com/Kevalgor12/tok-proxy/internal/hook"
	"github.com/Kevalgor12/tok-proxy/internal/store"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

const awarenessFilename = "tok-awareness.md"

var verifyHookVersionRe = regexp.MustCompile(`tok-hook-version:\s*([\w.-]+)`)

type toolStatus struct {
	name       string
	installed  string // "yes" | "no" | "not-detected"
	version    string
	hookPath   string
	registered *bool
	hookProbe  string // "pass" | "fail" | "skipped" | ""
	mode       string // transparent | instruction | unknown
	note       string
}

// RunVerify reports the hook status of every supported tool, plus whether the recorded hook
// version matches this build.
func RunVerify(s *store.Store) string {
	var tools []toolStatus

	// Claude Code - transparent (settings.json command hook).
	claudeHome := filepath.Join(home(), ".claude")
	if claudeCmd := hook.ReadRegisteredClaudeCommand(); claudeCmd != "" {
		pass, _, _ := hook.ProbeClaudeHook()
		tools = append(tools, toolStatus{
			name: "Claude Code", installed: "yes", hookPath: claudeCmd,
			registered: boolPtr(true), hookProbe: passFail(pass), mode: "transparent",
		})
	} else if util.FileExists(claudeHome) {
		tools = append(tools, toolStatus{name: "Claude Code", installed: "no", mode: "transparent",
			note: "detected but hook not registered - run: tok init --claude"})
	} else {
		tools = append(tools, toolStatus{name: "Claude Code", installed: "not-detected", mode: "unknown"})
	}

	// Cursor - instruction rule (~/.cursor/rules/tok.mdc). Cursor's hooks can only allow/deny/ask,
	// never rewrite a command, so tok can't intercept it transparently.
	tools = append(tools, instructionStatus("Cursor", filepath.Join(home(), ".cursor", "rules", "tok.mdc")))

	// Instruction-mode tools.
	tools = append(tools, instructionStatus("Copilot (VS Code)", vscodeAwarenessPath()))
	tools = append(tools, instructionStatus("Gemini CLI", filepath.Join(home(), ".gemini", awarenessFilename)))
	tools = append(tools, instructionStatus("Cline / Roo Code", filepath.Join(home(), ".cline", awarenessFilename)))
	// Antigravity - transparent (PreToolUse "overwrite" hook in the global customization config).
	tools = append(tools, antigravityStatus(filepath.Join(home(), ".gemini", "config", "hooks.json")))
	// Windsurf - instruction rule inside its shared global-rules file.
	tools = append(tools, ruleStatus("Windsurf", filepath.Join(home(), ".codeium", "windsurf", "memories", "global_rules.md")))

	lines := []string{"Hook status:"}
	for _, t := range tools {
		switch t.installed {
		case "not-detected":
			lines = append(lines, "  -    "+util.Pad(t.name, 20, false)+" not detected on this system")
			continue
		case "no":
			lines = append(lines, "  FAIL "+util.Pad(t.name, 20, false)+" "+firstNonEmpty(t.note, "not installed"))
			continue
		}
		v := ""
		if t.version != "" {
			v = " v" + t.version
		}
		modeTag := ""
		switch t.mode {
		case "transparent":
			modeTag = "[transparent]"
		case "instruction":
			modeTag = "[instruction]"
		}
		lines = append(lines, "  OK   "+util.Pad(t.name, 20, false)+" "+modeTag+v+"   "+t.hookPath)
		if t.registered != nil && !*t.registered {
			lines = append(lines, "         WARN: hook script exists but not registered with the AI tool - re-run tok init")
		}
		switch t.hookProbe {
		case "pass":
			lines = append(lines, "         Probe:   PASS (hook produced expected rewrite)")
		case "fail":
			lines = append(lines, "         Probe:   FAIL (hook output does not match expected protocol - run: tok hook-test)")
		}
	}

	hookV, _ := s.GetMeta("hook_version")
	lines = append(lines, "")
	switch {
	case hookV != "" && hookV != constants.Version:
		lines = append(lines, "WARN: hooks recorded as v"+hookV+" but tok is v"+constants.Version+". Run: tok init  to refresh.")
	case hookV != "":
		lines = append(lines, "Hooks are current (v"+hookV+").")
	default:
		lines = append(lines, "No hook version recorded yet - run: tok init")
	}
	return strings.Join(lines, "\n")
}

func readHookVersion(p string) string {
	c, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	if m := verifyHookVersionRe.FindStringSubmatch(string(c)); m != nil {
		return m[1]
	}
	return ""
}

func vscodeAwarenessPath() string {
	candidates := []string{
		filepath.Join(os.Getenv("APPDATA"), "Code", "User", awarenessFilename),
		filepath.Join(home(), "Library", "Application Support", "Code", "User", awarenessFilename),
		filepath.Join(home(), ".config", "Code", "User", awarenessFilename),
	}
	for _, c := range candidates {
		if util.FileExists(c) {
			return c
		}
	}
	return candidates[0]
}

func instructionStatus(name, mdPath string) toolStatus {
	if util.FileExists(mdPath) {
		return toolStatus{name: name, installed: "yes", version: readHookVersion(mdPath), hookPath: tildify(mdPath), mode: "instruction"}
	}
	return toolStatus{name: name, installed: "not-detected", mode: "instruction"}
}

// ruleStatus reports whether tok's managed rule block is present in a shared rules file
// (Windsurf) - the file itself may exist holding only the user's own rules.
func ruleStatus(name, rulesPath string) toolStatus {
	if c, ok := util.ReadFileIfExists(rulesPath); ok && strings.Contains(c, "<!-- tok:start -->") {
		return toolStatus{name: name, installed: "yes", hookPath: tildify(rulesPath), mode: "instruction"}
	}
	return toolStatus{name: name, installed: "not-detected", mode: "instruction"}
}

// antigravityStatus reports tok's transparent PreToolUse hook in Antigravity's global
// customization config (~/.gemini/config/hooks.json), keyed under "tok".
func antigravityStatus(hooksPath string) toolStatus {
	if c, ok := util.ReadFileIfExists(hooksPath); ok && strings.Contains(c, "hook antigravity") {
		return toolStatus{name: "Antigravity", installed: "yes", hookPath: tildify(hooksPath),
			registered: boolPtr(true), mode: "transparent"}
	}
	geminiDir := filepath.Join(home(), ".gemini")
	if util.FileExists(filepath.Join(home(), ".antigravity")) || util.FileExists(filepath.Join(home(), ".antigravity-ide")) || util.FileExists(geminiDir) {
		return toolStatus{name: "Antigravity", installed: "no", mode: "transparent",
			note: "detected but hook not registered - run: tok init --antigravity"}
	}
	return toolStatus{name: "Antigravity", installed: "not-detected", mode: "transparent"}
}

func tildify(p string) string {
	h := home()
	if h != "" && strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}

func boolPtr(b bool) *bool { return &b }

func passFail(pass bool) string {
	if pass {
		return "pass"
	}
	return "fail"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
