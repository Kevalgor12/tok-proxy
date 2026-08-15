package doctor

import (
	"encoding/json"
	"os"
	"os/exec"
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

	// Cursor - transparent (script in ~/.cursor/hooks/).
	cursorHome := filepath.Join(home(), ".cursor")
	cursorHook := filepath.Join(cursorHome, "hooks", "tok-rewrite.sh")
	cursorCfg := filepath.Join(cursorHome, "hooks.json")
	if util.FileExists(cursorHook) {
		reg := isCursorRegistered(cursorCfg)
		tools = append(tools, toolStatus{
			name: "Cursor", installed: "yes", version: readHookVersion(cursorHook),
			hookPath: "~/.cursor/hooks/tok-rewrite.sh", registered: boolPtr(reg),
			hookProbe: probeCursorHook(cursorHook), mode: "transparent",
		})
	} else if util.FileExists(cursorHome) {
		tools = append(tools, toolStatus{name: "Cursor", installed: "no", mode: "transparent",
			note: "detected but hook missing - run: tok init --cursor"})
	} else {
		tools = append(tools, toolStatus{name: "Cursor", installed: "not-detected", mode: "unknown"})
	}

	// Instruction-mode tools.
	tools = append(tools, instructionStatus("Copilot (VS Code)", vscodeAwarenessPath()))
	tools = append(tools, instructionStatus("Gemini CLI", filepath.Join(home(), ".gemini", awarenessFilename)))
	tools = append(tools, instructionStatus("Windsurf", filepath.Join(home(), ".codeium", "windsurf", awarenessFilename)))
	tools = append(tools, instructionStatus("Cline / Roo Code", filepath.Join(home(), ".cline", awarenessFilename)))

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

func isCursorRegistered(cfgPath string) bool {
	raw, ok := util.ReadFileIfExists(cfgPath)
	if !ok {
		return false
	}
	var cfg map[string]any
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return false
	}
	var list []any
	if l, ok := cfg["preToolUse"].([]any); ok {
		list = append(list, l...)
	}
	if hooks, ok := cfg["hooks"].(map[string]any); ok {
		if l, ok := hooks["preToolUse"].([]any); ok {
			list = append(list, l...)
		}
	}
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := m["command"].(string)
		if m["id"] == "tok-rewrite" || strings.Contains(cmd, "tok-rewrite.sh") {
			return true
		}
	}
	return false
}

// probeCursorHook runs the Cursor hook script with a fake git-status payload and checks it
// emits a tok rewrite. Returns "skipped" only when bash itself is unavailable.
func probeCursorHook(hookPath string) string {
	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"git status"}}`)
	out, err := cmd.Output()
	if err != nil {
		if _, lookErr := exec.LookPath("bash"); lookErr != nil {
			return "skipped"
		}
		return "fail"
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "fail"
	}
	var parsed struct {
		UpdatedInput struct {
			Command string `json:"command"`
		} `json:"updated_input"`
	}
	if json.Unmarshal([]byte(s), &parsed) != nil {
		return "fail"
	}
	if strings.HasPrefix(parsed.UpdatedInput.Command, "tok ") {
		return "pass"
	}
	return "fail"
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
