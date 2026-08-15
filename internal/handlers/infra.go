package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// Infrastructure-as-code: pulumi + terraform. Their plans are enormous resource
// dumps; the actionable part is the change summary (+create / ~update / -delete)
// plus any errors.

func HandlePulumi(args []string, ultra bool) Result {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("pulumi", args)
	raw := combined(r)
	filtered := summarizePulumi(raw, ultra, r.ExitCode)
	return finalizeRaw(filtered, r, raw, "pulumi "+firstNonEmpty(sub, "cmd"))
}

func HandleTerraform(args []string, ultra bool) Result {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	r := run.Run("terraform", args)
	raw := combined(r)
	filtered := summarizeTerraform(raw, ultra, r.ExitCode)
	return finalizeRaw(filtered, r, raw, "terraform "+firstNonEmpty(sub, "cmd"))
}

var (
	pulumiCreateRe = regexp.MustCompile(`[+]\s*(\d+)\s*(?:to create|created)`)
	pulumiUpdateRe = regexp.MustCompile(`[~]\s*(\d+)\s*(?:to update|updated|changed)`)
	pulumiDelRe    = regexp.MustCompile(`[-]\s*(\d+)\s*(?:to delete|deleted)`)
	pulumiErrRe    = regexp.MustCompile(`(?i)^\s*error:`)
	tfPlanRe       = regexp.MustCompile(`Plan:\s+(\d+)\s+to add,\s+(\d+)\s+to change,\s+(\d+)\s+to destroy`)
	tfApplyRe      = regexp.MustCompile(`Apply complete!\s+Resources:\s+(\d+)\s+added,\s+(\d+)\s+changed,\s+(\d+)\s+destroyed`)
	tfErrRe        = regexp.MustCompile(`^(Error:|╷|│ Error)`)
	tfNoChangeRe   = regexp.MustCompile(`(?i)No changes`)
)

func summarizePulumi(raw string, ultra bool, exitCode int) string {
	clean := util.StripAnsi(raw)
	create := firstGroupInt(pulumiCreateRe, clean)
	update := firstGroupInt(pulumiUpdateRe, clean)
	del := firstGroupInt(pulumiDelRe, clean)

	var errors []string
	for _, l := range strings.Split(clean, "\n") {
		if pulumiErrRe.MatchString(l) {
			errors = append(errors, l)
		}
	}
	if len(errors) > 0 {
		if ultra {
			return fmt.Sprintf("✗%derr", len(errors))
		}
		return "✗ pulumi failed:\n" + strings.Join(limit(errors, 6), "\n")
	}
	if create+update+del == 0 {
		if exitCode != 0 {
			return util.Truncate(clean, ternInt(ultra, 3, 12))
		}
		return "✓ no changes"
	}
	if ultra {
		return fmt.Sprintf("+%d~%d-%d", create, update, del)
	}
	return fmt.Sprintf("Resources: +%d create, ~%d update, -%d delete", create, update, del)
}

func summarizeTerraform(raw string, ultra bool, exitCode int) string {
	clean := util.StripAnsi(raw)
	m := tfPlanRe.FindStringSubmatch(clean)
	if m == nil {
		m = tfApplyRe.FindStringSubmatch(clean)
	}
	var errors []string
	for _, l := range strings.Split(clean, "\n") {
		if tfErrRe.MatchString(l) {
			errors = append(errors, l)
		}
	}
	if len(errors) > 0 {
		if ultra {
			return fmt.Sprintf("✗%derr", len(errors))
		}
		return "✗ terraform error:\n" + strings.Join(limit(errors, 6), "\n")
	}
	if m == nil {
		if tfNoChangeRe.MatchString(clean) {
			return "✓ no changes"
		}
		if exitCode == 0 {
			if ultra {
				return "✓"
			}
			return "✓ ok"
		}
		return util.Truncate(clean, ternInt(ultra, 3, 12))
	}
	if ultra {
		return fmt.Sprintf("+%s~%s-%s", m[1], m[2], m[3])
	}
	return fmt.Sprintf("Plan: +%s add, ~%s change, -%s destroy", m[1], m[2], m[3])
}

func firstGroupInt(re *regexp.Regexp, s string) int {
	if m := re.FindStringSubmatch(s); m != nil {
		return atoi(m[1])
	}
	return 0
}

func ternInt(cond bool, a, b int) int {
	if cond {
		return a
	}
	return b
}
