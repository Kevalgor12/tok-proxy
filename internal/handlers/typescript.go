package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

func HandleTsc(args []string, ultra bool) Result {
	r := run.Run("tsc", args)
	raw := combined(r)
	filtered := groupTscErrors(raw, ultra)

	if filtered == "" {
		if r.ExitCode == 0 {
			if ultra {
				filtered = "✓"
			} else {
				filtered = "✓ no errors"
			}
		} else {
			filtered = raw
		}
	}
	return Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: "tsc", ExecMs: r.ExecMs}
}

var tscErrLineRe = regexp.MustCompile(`^(.+?)\((\d+),(\d+)\):\s+error\s+(TS\d+):\s+(.+)`)

// groupTscErrors folds "file(line,col): error TSxxxx: msg" lines into per-file, per-code
// counts, preserving first-seen order (Go maps don't, so files/codes are tracked in slices).
func groupTscErrors(raw string, ultra bool) string {
	clean := util.StripAnsi(raw)

	type codeInfo struct {
		msg   string
		count int
	}
	type fileErrs struct {
		codes  []string
		byCode map[string]*codeInfo
	}
	var fileOrder []string
	files := map[string]*fileErrs{}
	total := 0

	for _, line := range strings.Split(clean, "\n") {
		m := tscErrLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		file, code, msg := m[1], m[4], m[5]
		total++
		fe, ok := files[file]
		if !ok {
			fe = &fileErrs{byCode: map[string]*codeInfo{}}
			files[file] = fe
			fileOrder = append(fileOrder, file)
		}
		if info, ok := fe.byCode[code]; ok {
			info.count++
		} else {
			fe.byCode[code] = &codeInfo{msg: msg, count: 1}
			fe.codes = append(fe.codes, code)
		}
	}

	if total == 0 {
		if ultra {
			return "✓"
		}
		return "✓ no errors"
	}

	if ultra {
		var parts []string
		for _, file := range fileOrder {
			fe := files[file]
			short := file
			if i := strings.LastIndexAny(file, `/\`); i >= 0 {
				short = file[i+1:]
			}
			var codeStrs []string
			for _, code := range fe.codes {
				codeStrs = append(codeStrs, fmt.Sprintf("%s×%d", code, fe.byCode[code].count))
			}
			parts = append(parts, short+":"+strings.Join(codeStrs, ","))
		}
		return fmt.Sprintf("%dE/%dF: %s", total, len(fileOrder), strings.Join(limit(parts, 5), " "))
	}

	var out []string
	for _, file := range fileOrder {
		fe := files[file]
		fileTotal := 0
		for _, code := range fe.codes {
			fileTotal += fe.byCode[code].count
		}
		out = append(out, fmt.Sprintf("%s: %d %s", file, fileTotal, plural(fileTotal, "error", "errors")))
		for _, code := range fe.codes {
			info := fe.byCode[code]
			out = append(out, fmt.Sprintf("  %s (×%d) %s", code, info.count, info.msg))
		}
	}
	out = append(out, "", fmt.Sprintf("Total: %d %s in %d %s",
		total, plural(total, "error", "errors"), len(fileOrder), plural(len(fileOrder), "file", "files")))
	return strings.Join(out, "\n")
}
