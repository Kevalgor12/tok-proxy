package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/filter"
	"github.com/Kevalgor12/tok-proxy/internal/run"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

func since(t time.Time) int { return int(time.Since(t).Milliseconds()) }

func firstPositional(args []string, def string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return def
}

// ---- ls --------------------------------------------------------------------

func HandleLs(args []string, ultra bool, cfg config.Config) Result {
	target := firstPositional(args, ".")
	start := time.Now()
	return Result{
		Filtered: formatTree(target, ultra, cfg),
		Exit:     0,
		Raw:      readDirRaw(target),
		CmdType:  "ls",
		ExecMs:   since(start),
	}
}

func readDirRaw(target string) string {
	entries, err := os.ReadDir(target)
	if err != nil {
		return err.Error()
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return strings.Join(names, "\n")
}

type lsNode struct {
	name      string
	isDir     bool
	fileCount int
	children  []*lsNode
}

func formatTree(dirPath string, ultra bool, cfg config.Config) string {
	noise := map[string]bool{}
	for _, d := range cfg.NoiseDirectories {
		noise[d] = true
	}
	maxDepth := cfg.Filters.Ls.MaxDepth
	totalFiles, totalDirs := 0, 0

	var walk func(p string, depth int) *lsNode
	walk = func(p string, depth int) *lsNode {
		name := filepath.Base(p)
		if name == "" {
			name = p
		}
		node := &lsNode{name: name, isDir: true}
		if depth > maxDepth {
			return node
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return node
		}
		var files, dirs []os.DirEntry
		for _, e := range entries {
			switch {
			case e.IsDir():
				if !noise[e.Name()] {
					dirs = append(dirs, e)
				}
			case e.Type().IsRegular():
				files = append(files, e)
			}
		}
		node.fileCount = len(files)
		totalFiles += len(files)
		for _, d := range dirs {
			totalDirs++
			node.children = append(node.children, walk(filepath.Join(p, d.Name()), depth+1))
		}
		if len(files) <= 5 {
			for _, f := range files {
				node.children = append(node.children, &lsNode{name: f.Name()})
			}
		}
		return node
	}
	root := walk(dirPath, 0)

	if ultra {
		var items []string
		extra := 0
		for _, c := range root.children {
			if c.isDir {
				if len(items) < 6 {
					items = append(items, fmt.Sprintf("%s/%d", c.name, c.fileCount))
				} else {
					extra++
				}
			} else if len(items) < 6 {
				items = append(items, c.name)
			}
		}
		if extra > 0 {
			items = append(items, fmt.Sprintf("[+%d dirs]", extra))
		}
		return strings.Join(items, " ")
	}

	rootName := root.name
	if rootName == "." {
		if abs, err := filepath.Abs(dirPath); err == nil {
			rootName = abs
		}
	}
	lines := []string{rootName + "/"}
	renderTree(root, "", &lines)
	lines = append(lines, "", fmt.Sprintf("Total: %d files in %d directories", totalFiles, totalDirs))
	return strings.Join(lines, "\n")
}

func renderTree(node *lsNode, prefix string, lines *[]string) {
	for i, child := range node.children {
		last := i == len(node.children)-1
		branch, nextPrefix := "├─", prefix+"│  "
		if last {
			branch, nextPrefix = "└─", prefix+"   "
		}
		switch {
		case child.isDir && child.fileCount > 5:
			*lines = append(*lines, fmt.Sprintf("%s%s %s/ (%d files)", prefix, branch, child.name, child.fileCount))
		case child.isDir:
			*lines = append(*lines, fmt.Sprintf("%s%s %s/", prefix, branch, child.name))
			renderTree(child, nextPrefix, lines)
		default:
			*lines = append(*lines, fmt.Sprintf("%s%s %s", prefix, branch, child.name))
		}
	}
}

// ---- cat / smart / json ----------------------------------------------------

func HandleCat(args []string, ultra bool, cfg config.Config) Result {
	file := firstPositional(args, "")
	start := time.Now()
	if file == "" {
		return Result{Filtered: "usage: tok cat <file>", Exit: 2, CmdType: "cat", ExecMs: since(start)}
	}
	raw, ok := util.ReadFileIfExists(file)
	if !ok || raw == "" {
		return Result{Filtered: "cannot read: " + file, Exit: 1, CmdType: "cat", ExecMs: since(start)}
	}
	level := pickLevel(args, cfg.Filters.Cat.DefaultLevel, ultra)
	filtered := util.Truncate(filter.FilterCode(raw, filter.LangFromPath(file), level), cfg.Filters.Cat.MaxLines)
	return Result{Filtered: filtered, Exit: 0, Raw: raw, CmdType: "cat", ExecMs: since(start)}
}

func pickLevel(args []string, def config.FilterLevel, ultra bool) config.FilterLevel {
	switch {
	case ultra, slices.Contains(args, "--aggressive"):
		return config.FilterAggressive
	case slices.Contains(args, "--minimal"):
		return config.FilterMinimal
	case slices.Contains(args, "--none"):
		return config.FilterNone
	default:
		return def
	}
}

func HandleSmart(args []string, _ bool) Result {
	file := firstPositional(args, "")
	start := time.Now()
	if file == "" {
		return Result{Filtered: "usage: tok smart <file>", Exit: 2, CmdType: "smart", ExecMs: since(start)}
	}
	raw, ok := util.ReadFileIfExists(file)
	if !ok || raw == "" {
		return Result{Filtered: "cannot read: " + file, Exit: 1, CmdType: "smart", ExecMs: since(start)}
	}
	return Result{Filtered: filter.SmartSummary(raw, filter.LangFromPath(file)), Exit: 0, Raw: raw, CmdType: "smart", ExecMs: since(start)}
}

func HandleJson(args []string, _ bool) Result {
	file := firstPositional(args, "")
	start := time.Now()
	if file == "" {
		return Result{Filtered: "usage: tok json <file>", Exit: 2, CmdType: "json", ExecMs: since(start)}
	}
	raw, ok := util.ReadFileIfExists(file)
	if !ok || raw == "" {
		return Result{Filtered: "cannot read: " + file, Exit: 1, CmdType: "json", ExecMs: since(start)}
	}
	var parsed any
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		return Result{Filtered: "invalid JSON: " + file, Exit: 1, Raw: raw, CmdType: "json", ExecMs: since(start)}
	}
	b, err := json.MarshalIndent(filter.ExtractStructure(parsed), "", "  ")
	if err != nil {
		return Result{Filtered: "invalid JSON: " + file, Exit: 1, Raw: raw, CmdType: "json", ExecMs: since(start)}
	}
	return Result{Filtered: string(b), Exit: 0, Raw: raw, CmdType: "json", ExecMs: since(start)}
}

// ---- grep ------------------------------------------------------------------

func HandleGrep(args []string, ultra bool, cfg config.Config) Result {
	cmd := "grep"
	if _, err := exec.LookPath("rg"); err == nil {
		cmd = "rg"
	}
	useArgs := append([]string{}, args...)

	// grep needs -H to print the filename even for a single-file target; without it the
	// "file:line:content" grouping regex only sees "line:content" and reports 0 matches.
	var positionals []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			positionals = append(positionals, a)
		}
	}
	singleFileTargets := 0
	if len(positionals) >= 2 {
		for _, p := range positionals[1:] {
			if fileExists(p) {
				singleFileTargets++
			}
		}
	}
	looksLikeSingleFile := len(positionals) >= 2 && singleFileTargets == 1

	if cmd == "grep" {
		if !slices.Contains(useArgs, "-rn") && !slices.Contains(useArgs, "-n") {
			useArgs = append([]string{"-rn"}, useArgs...)
		}
		if looksLikeSingleFile && !slices.Contains(useArgs, "-H") {
			useArgs = append([]string{"-H"}, useArgs...)
		}
	}

	r := run.Run(cmd, useArgs)
	raw := combined(r)
	filtered := groupByFile(raw, ultra, cfg.Filters.Grep.MaxMatches)
	if filtered == "" {
		if r.ExitCode == 0 {
			filtered = "0 matches"
		} else {
			filtered = raw
		}
	}
	return Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: "grep", ExecMs: r.ExecMs}
}

var grepLineRe = regexp.MustCompile(`^(.+?):(\d+):`)

func groupByFile(raw string, ultra bool, maxMatches int) string {
	clean := util.StripAnsi(raw)
	var fileOrder []string
	byFile := map[string][]int{}
	total := 0
	for _, line := range strings.Split(clean, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := grepLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		total++
		if _, ok := byFile[m[1]]; !ok {
			fileOrder = append(fileOrder, m[1])
		}
		byFile[m[1]] = append(byFile[m[1]], atoi(m[2]))
	}
	if total == 0 {
		if ultra {
			return "0"
		}
		return "0 matches"
	}

	if ultra {
		var parts []string
		for _, file := range fileOrder {
			short := file
			if i := strings.LastIndexAny(file, `/\`); i >= 0 {
				short = file[i+1:]
			}
			parts = append(parts, fmt.Sprintf("%s:%d", short, len(byFile[file])))
		}
		return strings.Join(limit(parts, 8), " ")
	}

	var out []string
	shown := 0
	for _, file := range fileOrder {
		ls := byFile[file]
		if shown+len(ls) > maxMatches {
			out = append(out, fmt.Sprintf("[+%d more matches]", total-shown))
			break
		}
		var lnums []string
		for _, n := range limit(ls, 10) {
			lnums = append(lnums, fmt.Sprintf("L%d", n))
		}
		more := ""
		if len(ls) > 10 {
			more = fmt.Sprintf(", +%d more", len(ls)-10)
		}
		out = append(out, fmt.Sprintf("%s: %d %s (%s%s)", file, len(ls), plural(len(ls), "match", "matches"), strings.Join(lnums, ", "), more))
		shown += len(ls)
	}
	return strings.Join(out, "\n")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// ---- find ------------------------------------------------------------------

func HandleFind(args []string, ultra bool) Result {
	start := time.Now()
	var lines []string
	var raw string
	exit := 0

	// The Windows `find` is a substring search with totally different args, so fall back to
	// a portable walker there (and whenever an arg our walker doesn't implement shows up).
	if runtime.GOOS == "windows" || hasUnsupportedFindArg(args) {
		dir, pattern := parseFindArgs(args)
		lines = walkFiles(dir, pattern)
		raw = strings.Join(lines, "\n")
	} else {
		r := run.Run("find", args)
		raw = combined(r)
		exit = r.ExitCode
		for _, l := range strings.Split(util.StripAnsi(raw), "\n") {
			if strings.TrimSpace(l) != "" {
				lines = append(lines, l)
			}
		}
	}

	var filtered string
	switch {
	case ultra:
		filtered = fmt.Sprintf("%d files", len(lines))
	case len(lines) > 50:
		filtered = fmt.Sprintf("%d files found\n%s\n[+%d more]", len(lines), strings.Join(limit(lines, 50), "\n"), len(lines)-50)
	case len(lines) == 0:
		filtered = "0 files"
	default:
		filtered = strings.Join(lines, "\n")
	}
	return Result{Filtered: filtered, Exit: exit, Raw: raw, CmdType: "find", ExecMs: since(start)}
}

func hasUnsupportedFindArg(args []string) bool {
	supported := map[string]bool{"-name": true, "-iname": true}
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !supported[a] {
			return true
		}
	}
	return false
}

func parseFindArgs(args []string) (string, *regexp.Regexp) {
	dir := "."
	var pattern *regexp.Regexp
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-name" || a == "-iname":
			glob := ""
			if i+1 < len(args) {
				i++
				glob = args[i]
			}
			pattern = globToRegex(glob, a == "-iname")
		case !strings.HasPrefix(a, "-") && i == 0:
			dir = a
		}
	}
	return dir, pattern
}

func globToRegex(glob string, ci bool) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for _, ch := range glob {
		switch {
		case ch == '*':
			b.WriteString(".*")
		case ch == '?':
			b.WriteString(".")
		case strings.ContainsRune(`.+^${}()|[]\`, ch):
			b.WriteByte('\\')
			b.WriteRune(ch)
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteString("$")
	flags := ""
	if ci {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + b.String())
	if err != nil {
		return nil
	}
	return re
}

func walkFiles(dir string, pattern *regexp.Regexp) []string {
	var results []string
	queue := []string{dir}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		entries, err := os.ReadDir(cur)
		if err != nil {
			continue
		}
		for _, e := range entries {
			full := filepath.Join(cur, e.Name())
			switch {
			case e.IsDir():
				if e.Name() != "node_modules" && e.Name() != ".git" {
					queue = append(queue, full)
				}
			case e.Type().IsRegular():
				if pattern == nil || pattern.MatchString(e.Name()) {
					results = append(results, full)
				}
			}
		}
	}
	return results
}

// ---- diff ------------------------------------------------------------------

func HandleDiff(args []string, ultra bool) Result {
	r := run.Run("diff", args)
	raw := combined(r)
	lines := strings.Split(util.StripAnsi(raw), "\n")
	added, removed := 0, 0
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, ">") || (strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++")):
			added++
		case strings.HasPrefix(line, "<") || (strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---")):
			removed++
		}
	}
	filtered := fmt.Sprintf("+%d -%d (%d diff lines)", added, removed, len(lines))
	if ultra {
		filtered = fmt.Sprintf("+%d-%d", added, removed)
	}
	return Result{Filtered: filtered, Exit: r.ExitCode, Raw: raw, CmdType: "diff", ExecMs: r.ExecMs}
}
