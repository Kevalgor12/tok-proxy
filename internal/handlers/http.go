package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/filter"
	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// HTTP fetchers (curl/wget) and env. curl/wget bodies are only compressed when
// large (JSON -> structure, otherwise dedup+truncate); env is redacted to keys
// only so secrets never reach the model.

func HandleCurl(args []string, ultra bool) Result {
	r := run.Run("curl", args)
	raw := combined(r)
	filtered := summarizeBody(firstNonEmpty(r.Stdout, raw), ultra)
	return finalizeRaw(filtered, r, raw, "curl")
}

var (
	wgetSavedRe  = regexp.MustCompile(`saved\s+\[(\d+)(?:/\d+)?\]`)
	wgetStatusRe = regexp.MustCompile(`HTTP request sent.*?\s(\d{3})\b`)
	wgetErrRe    = regexp.MustCompile(`(?i)error|failed|unable`)
	envVarRe     = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=`)
	htmlDocRe    = regexp.MustCompile(`(?i)^\s*<(!doctype|html)`)
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
	whitespaceRe = regexp.MustCompile(`\s+`)
	htmlTitleRe  = regexp.MustCompile(`(?i)<title>([^<]+)</title>`)
)

func HandleWget(args []string, ultra bool) Result {
	r := run.Run("wget", args)
	raw := combined(r)
	clean := util.StripAnsi(raw)
	var filtered string

	switch {
	case wgetSavedRe.MatchString(clean):
		size := util.FormatBytes(atoi(wgetSavedRe.FindStringSubmatch(clean)[1]))
		if ultra {
			filtered = "✓" + size
		} else {
			filtered = "✓ downloaded " + size
			if s := wgetStatusRe.FindStringSubmatch(clean); s != nil {
				filtered += " (HTTP " + s[1] + ")"
			}
		}
	case r.ExitCode != 0:
		var errs []string
		for _, l := range strings.Split(clean, "\n") {
			if wgetErrRe.MatchString(l) {
				errs = append(errs, l)
			}
		}
		filtered = strings.Join(limit(errs, 5), "\n")
		if filtered == "" {
			filtered = util.Truncate(clean, 8)
		}
	default:
		if ultra {
			filtered = "✓"
		} else {
			filtered = "✓ ok"
		}
	}
	return finalizeRaw(filtered, r, raw, "wget")
}

// HandleEnv shows variable NAMES only. Values commonly hold tokens and secrets, so
// we never echo them to the model.
func HandleEnv(args []string, ultra bool) Result {
	bin := "env"
	realArgs := args
	if len(args) > 0 && args[0] == "__printenv__" {
		bin = "printenv"
		realArgs = args[1:]
	}
	r := run.Run(bin, realArgs)
	raw := combined(r)

	var keys []string
	for _, l := range strings.Split(util.StripAnsi(r.Stdout), "\n") {
		if m := envVarRe.FindStringSubmatch(l); m != nil {
			keys = append(keys, m[1])
		}
	}
	sort.Strings(keys)

	var filtered string
	switch {
	case len(keys) == 0:
		filtered = util.Truncate(strings.TrimSpace(util.StripAnsi(raw)), 10)
	case ultra:
		filtered = fmt.Sprintf("%d vars", len(keys))
	default:
		filtered = fmt.Sprintf("%d environment variables (values redacted):\n%s", len(keys), strings.Join(keys, ", "))
	}
	if filtered == "" {
		filtered = "ok"
	}
	return Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: "env", ExecMs: r.ExecMs}
}

func summarizeBody(body string, ultra bool) string {
	clean := strings.TrimSpace(util.StripAnsi(body))
	if clean == "" {
		if ultra {
			return "(empty)"
		}
		return "(empty response)"
	}

	// JSON -> collapse to a key/type skeleton when large.
	var parsed any
	if json.Unmarshal([]byte(clean), &parsed) == nil {
		switch parsed.(type) {
		case map[string]any, []any:
			size := len(clean)
			if size <= 800 {
				return clean
			}
			structure := jsonStringify(filter.ExtractStructure(parsed), !ultra)
			return fmt.Sprintf("JSON response (%s), structure:\n%s",
				util.FormatBytes(size), util.Truncate(structure, ternInt(ultra, 20, 60)))
		}
	}

	// HTML -> strip tags, note size.
	if htmlDocRe.MatchString(clean) {
		text := strings.TrimSpace(whitespaceRe.ReplaceAllString(htmlTagRe.ReplaceAllString(clean, " "), " "))
		head := fmt.Sprintf("HTML (%s)", util.FormatBytes(len(clean)))
		if t := htmlTitleRe.FindStringSubmatch(clean); t != nil {
			head += fmt.Sprintf(` - "%s"`, strings.TrimSpace(t[1]))
		}
		return fmt.Sprintf("%s: %s", head, util.Truncate(text, ternInt(ultra, 3, 8)))
	}

	lines := strings.Split(clean, "\n")
	if len(lines) <= ternInt(ultra, 8, 40) {
		return clean
	}
	return util.Truncate(filter.DeduplicateLines(clean), ternInt(ultra, 8, 40))
}

// jsonStringify mirrors JSON.stringify: no HTML escaping, optional 2-space indent,
// and no trailing newline.
func jsonStringify(v any, indent bool) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return ""
	}
	return strings.TrimRight(buf.String(), "\n")
}
