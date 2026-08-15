package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/filter"
	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

var (
	twoSpaceRe   = regexp.MustCompile(`\s{2,}`)
	dockerUpRe   = regexp.MustCompile(`\bUp\b`)
	dockerStatRe = regexp.MustCompile(`\b(Up|Exited|Created)\b`)
	logErrRe     = regexp.MustCompile(`(?i)\b(error|err|failed|exception|panic)\b`)
)

func HandleDocker(args []string, ultra bool) Result {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("docker", args)
	raw := combined(r)
	var filtered string

	switch sub {
	case "ps":
		filtered = formatPs(raw, ultra)
	case "images":
		filtered = formatImages(raw, ultra)
	case "logs":
		filtered = formatLogs(raw, ultra)
	case "compose":
		filtered = formatCompose(raw, ultra)
	default:
		filtered = strings.TrimSpace(util.StripAnsi(raw))
	}

	cmdType := "docker " + firstNonEmpty(sub, "cmd")
	return finalizeRaw(filtered, r, raw, cmdType)
}

func HandleKubectl(args []string, ultra bool) Result {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("kubectl", args)
	raw := combined(r)
	var filtered string

	switch sub {
	case "logs":
		filtered = formatLogs(raw, ultra)
	case "get":
		filtered = formatKubectlGet(raw, ultra)
	default:
		filtered = strings.TrimSpace(util.StripAnsi(raw))
	}

	return finalizeRaw(filtered, r, raw, "kubectl "+firstNonEmpty(sub, "cmd"))
}

func formatPs(raw string, ultra bool) string {
	data := dataRows(util.StripAnsi(raw))
	running := 0
	for _, l := range data {
		if dockerUpRe.MatchString(l) {
			running++
		}
	}
	stopped := len(data) - running
	if ultra {
		return fmt.Sprintf("%d↑/%d↓", running, stopped)
	}
	if len(data) == 0 {
		return "no containers"
	}
	out := []string{fmt.Sprintf("%d %s: %d running, %d stopped",
		len(data), plural(len(data), "container", "containers"), running, stopped)}
	for _, line := range limit(data, 10) {
		tokens := twoSpaceRe.Split(line, -1)
		if len(tokens) >= 2 {
			id := tokens[0]
			if len(id) > 12 {
				id = id[:12]
			}
			status := ""
			for _, t := range tokens {
				if dockerStatRe.MatchString(t) {
					status = t
					break
				}
			}
			out = append(out, fmt.Sprintf("  %s %s %s", id, tokens[1], status))
		}
	}
	return strings.Join(out, "\n")
}

func formatImages(raw string, ultra bool) string {
	data := dataRows(util.StripAnsi(raw))
	if ultra {
		return fmt.Sprintf("%d images", len(data))
	}
	if len(data) == 0 {
		return "no images"
	}
	out := []string{fmt.Sprintf("%d %s:", len(data), plural(len(data), "image", "images"))}
	for _, line := range limit(data, 15) {
		tokens := twoSpaceRe.Split(line, -1)
		if len(tokens) >= 2 {
			tag := tokens[1]
			if tag == "" {
				tag = "latest"
			}
			out = append(out, fmt.Sprintf("  %s:%s", tokens[0], tag))
		}
	}
	return strings.Join(out, "\n")
}

func formatLogs(raw string, ultra bool) string {
	lines := nonEmptyLines(filter.DeduplicateLines(raw))
	var errors []string
	for _, l := range lines {
		if logErrRe.MatchString(l) {
			errors = append(errors, l)
		}
	}
	if ultra {
		var top []string
		for _, l := range limit(lines, 3) {
			top = append(top, clip(l, 60))
		}
		return fmt.Sprintf("%dL %dE | %s", len(lines), len(errors), strings.Join(top, " | "))
	}
	if len(lines) == 0 {
		return "no logs"
	}
	out := []string{fmt.Sprintf("%d unique %s (%d errors)",
		len(lines), plural(len(lines), "log line", "log lines"), len(errors))}
	if len(errors) > 0 {
		out = append(out, "", "Errors:")
		for _, e := range limit(errors, 10) {
			out = append(out, "  "+e)
		}
	}
	out = append(out, "", "Top messages:")
	for _, l := range limit(lines, 10) {
		out = append(out, "  "+l)
	}
	return strings.Join(out, "\n")
}

func formatCompose(raw string, ultra bool) string {
	lines := nonEmptyLines(util.StripAnsi(raw))
	if ultra {
		return fmt.Sprintf("compose: %dL", len(lines))
	}
	return strings.Join(lastN(lines, 30), "\n")
}

func formatKubectlGet(raw string, ultra bool) string {
	data := dataRows(util.StripAnsi(raw))
	if ultra {
		return fmt.Sprintf("%d resources", len(data))
	}
	return fmt.Sprintf("%d %s\n%s", len(data), plural(len(data), "resource", "resources"),
		strings.Join(limit(data, 20), "\n"))
}

// dataRows returns the non-empty lines of a table with the header row dropped.
func dataRows(clean string) []string {
	lines := nonEmptyLines(clean)
	if len(lines) <= 1 {
		return nil
	}
	return lines[1:]
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
