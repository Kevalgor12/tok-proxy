// Package filter holds tok's compression primitives: language-aware source stripping,
// output line dedup, and JSON structure extraction. Pure functions, no I/O.
package filter

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Kevalgor12/tok-proxy/internal/config"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

var langByExt = map[string]string{
	"ts": "ts", "tsx": "tsx",
	"js": "js", "jsx": "jsx", "mjs": "js", "cjs": "js",
	"py":   "py",
	"go":   "go",
	"rs":   "rs",
	"java": "java",
	"cs":   "cs",
	"cpp":  "cpp", "cc": "cpp", "cxx": "cpp", "hpp": "cpp", "hxx": "cpp",
	"c": "c", "h": "c",
	"rb": "rb",
}

func LangFromPath(p string) string {
	dot := strings.LastIndex(p, ".")
	if dot < 0 {
		return ""
	}
	return langByExt[strings.ToLower(p[dot+1:])]
}

var (
	cLike    = map[string]bool{"ts": true, "tsx": true, "js": true, "jsx": true, "go": true, "rs": true, "java": true, "cs": true, "cpp": true, "c": true}
	hashLang = map[string]bool{"py": true, "rb": true}

	blockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	blankRunRe     = regexp.MustCompile(`\n{3,}`)
)

// FilterCode strips comments (and, at the aggressive level, function bodies) from source.
func FilterCode(source, lang string, level config.FilterLevel) string {
	if level == config.FilterNone {
		return source
	}

	out := source
	switch {
	case cLike[lang]:
		out = blockCommentRe.ReplaceAllString(out, "")
		out = stripLineComments(out, "//")
	case hashLang[lang]:
		out = stripLineComments(out, "#")
	default:
		out = blockCommentRe.ReplaceAllString(out, "")
		out = stripLineComments(out, "//")
		out = stripLineComments(out, "#")
	}
	out = collapseBlanks(out)

	if level == config.FilterMinimal {
		return out
	}
	return aggressiveFilter(out, lang)
}

func stripLineComments(src, prefix string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if idx := findCommentIdx(line, prefix); idx >= 0 {
			lines[i] = strings.TrimRight(line[:idx], " \t\r\f\v")
		}
	}
	return strings.Join(lines, "\n")
}

// findCommentIdx returns the column where `prefix` begins outside any string literal,
// or -1. Scans bytes: string delimiters and comment markers are all ASCII, so multi-byte
// runes elsewhere on the line can't be mistaken for them.
func findCommentIdx(line, prefix string) int {
	var inSingle, inDouble, inBacktick bool
	for i := 0; i < len(line); i++ {
		c := line[i]
		var prev byte
		if i > 0 {
			prev = line[i-1]
		}
		switch {
		case c == '\'' && prev != '\\' && !inDouble && !inBacktick:
			inSingle = !inSingle
		case c == '"' && prev != '\\' && !inSingle && !inBacktick:
			inDouble = !inDouble
		case c == '`' && prev != '\\' && !inSingle && !inDouble:
			inBacktick = !inBacktick
		case !inSingle && !inDouble && !inBacktick:
			if strings.HasPrefix(line[i:], prefix) {
				return i
			}
		}
	}
	return -1
}

func collapseBlanks(src string) string {
	return blankRunRe.ReplaceAllString(src, "\n\n")
}

func aggressiveFilter(src, lang string) string {
	switch {
	case cLike[lang]:
		return aggressiveCLike(src)
	case lang == "py":
		return aggressivePython(src)
	case lang == "rb":
		return aggressiveRuby(src)
	default:
		return src
	}
}

var (
	keepersRe   = regexp.MustCompile(`^(import\b|export\b|from\b|const\b|let\b|var\b|type\b|interface\b|enum\b|namespace\b|declare\b|use\b|using\b|package\b)`)
	sigStartRe  = regexp.MustCompile(`^(export\s+)?(default\s+)?(async\s+)?(function|class|interface|type|enum)\b`)
	methodSigRe = regexp.MustCompile(`^[a-zA-Z_$][\w$]*\s*\([^)]*\)\s*(:\s*[^{]+)?\s*\{?\s*$`)
	arrowFnRe   = regexp.MustCompile(`=>\s*\{?\s*$`)
	closerRe    = regexp.MustCompile(`^[}\])]+;?\s*$`)
)

func aggressiveCLike(src string) string {
	lines := strings.Split(src, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if sigStartRe.MatchString(trimmed) && strings.Contains(trimmed, "{") {
			end := findMatchingBrace(lines, i, strings.Index(line, "{"))
			out = append(out, collapseToBraceStub(line))
			i = end + 1
			continue
		}

		if sigStartRe.MatchString(trimmed) && !strings.Contains(trimmed, "{") {
			out = append(out, line)
			i++
			for i < len(lines) && !strings.Contains(lines[i], "{") && !strings.HasSuffix(lines[i], ";") {
				out = append(out, lines[i])
				i++
			}
			if i < len(lines) && strings.Contains(lines[i], "{") {
				end := findMatchingBrace(lines, i, strings.Index(lines[i], "{"))
				out = append(out, collapseToBraceStub(lines[i]))
				i = end + 1
			}
			continue
		}

		if methodSigRe.MatchString(trimmed) && strings.Contains(trimmed, "{") {
			end := findMatchingBrace(lines, i, strings.Index(line, "{"))
			out = append(out, collapseToBraceStub(line))
			i = end + 1
			continue
		}

		if arrowFnRe.MatchString(trimmed) && strings.Contains(trimmed, "{") {
			end := findMatchingBrace(lines, i, strings.LastIndex(line, "{"))
			out = append(out, collapseToBraceStub(line))
			i = end + 1
			continue
		}

		if keepersRe.MatchString(trimmed) {
			out = append(out, line)
			i++
			continue
		}

		if trimmed == "" || closerRe.MatchString(trimmed) {
			out = append(out, line)
			i++
			continue
		}

		i++
	}
	return collapseBlanks(strings.Join(out, "\n"))
}

// collapseToBraceStub replaces everything from the first `{` to end of line with `{ ... }`.
func collapseToBraceStub(line string) string {
	if i := strings.Index(line, "{"); i >= 0 {
		return line[:i] + "{ ... }"
	}
	return line
}

func findMatchingBrace(lines []string, startLine, startCol int) int {
	depth := 0
	started := false
	for li := startLine; li < len(lines); li++ {
		line := lines[li]
		from := 0
		if li == startLine {
			from = startCol
		}
		for ci := from; ci < len(line); ci++ {
			switch line[ci] {
			case '{':
				depth++
				started = true
			case '}':
				depth--
				if started && depth == 0 {
					return li
				}
			}
		}
	}
	return len(lines) - 1
}

var (
	pySigRe    = regexp.MustCompile(`^(\s*)(async\s+)?(def|class)\s+`)
	pyColonRe  = regexp.MustCompile(`:\s*$`)
	pyImportRe = regexp.MustCompile(`^(import |from )`)
	pyConstRe  = regexp.MustCompile(`^[A-Z_]+\s*=`)
	nonSpaceRe = regexp.MustCompile(`\S`)
)

func aggressivePython(src string) string {
	lines := strings.Split(src, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		line := lines[i]
		if m := pySigRe.FindStringSubmatch(line); m != nil {
			baseIndent := len(m[1])
			out = append(out, pyColonRe.ReplaceAllString(line, ":")+" ...")
			i++
			for i < len(lines) {
				next := lines[i]
				if strings.TrimSpace(next) == "" {
					i++
					continue
				}
				loc := nonSpaceRe.FindStringIndex(next)
				indent := len(next)
				if loc != nil {
					indent = loc[0]
				}
				if indent <= baseIndent {
					break
				}
				i++
			}
			continue
		}
		if pyImportRe.MatchString(line) || pyConstRe.MatchString(strings.TrimSpace(line)) {
			out = append(out, line)
		} else if strings.TrimSpace(line) == "" {
			out = append(out, line)
		}
		i++
	}
	return collapseBlanks(strings.Join(out, "\n"))
}

var (
	rbSigRe  = regexp.MustCompile(`^(def |class |module )`)
	rbEndRe  = regexp.MustCompile(`^\s*end\b`)
	rbKeepRe = regexp.MustCompile(`^(require|include|extend|attr_)`)
)

func aggressiveRuby(src string) string {
	lines := strings.Split(src, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if rbSigRe.MatchString(trimmed) {
			out = append(out, line)
			i++
			for i < len(lines) && !rbEndRe.MatchString(lines[i]) {
				i++
			}
			if i < len(lines) {
				out = append(out, lines[i])
				i++
			}
			continue
		}
		if rbKeepRe.MatchString(trimmed) || trimmed == "" {
			out = append(out, line)
		}
		i++
	}
	return collapseBlanks(strings.Join(out, "\n"))
}

// ---- summaries + dedup + structure -----------------------------------------

var (
	reTsxProps   = regexp.MustCompile(`interface\s+\w*Props|type\s+\w*Props\s*=|\bprops\.\w+`)
	reHooks      = regexp.MustCompile(`\buse[A-Z]\w+\s*\(`)
	reComponent  = regexp.MustCompile(`export\s+(default\s+)?(function|const)\s+[A-Z]`)
	reReturnJSX  = regexp.MustCompile(`\breturn\s*\(\s*<`)
	reClass      = regexp.MustCompile(`\bclass\s+\w+`)
	reInterface  = regexp.MustCompile(`\binterface\s+\w+`)
	reExport     = regexp.MustCompile(`\bexport\s+(default\s+|const\s+|function\s+|class\s+|interface\s+|type\s+|enum\s+|let\s+|var\s+)`)
	reFunction   = regexp.MustCompile(`\bfunction\s+\w+|\bconst\s+\w+\s*=\s*(async\s+)?\(`)
	reAsync      = regexp.MustCompile(`\basync\s+(function|\()`)
	reImportLine = regexp.MustCompile(`(?m)^import\s+`)
	rePyClass    = regexp.MustCompile(`(?m)^\s*class\s+\w+`)
	rePyDef      = regexp.MustCompile(`(?m)^\s*(async\s+)?def\s+\w+`)
	rePyImport   = regexp.MustCompile(`(?m)^(import\s+|from\s+\w)`)
	reGoFunc     = regexp.MustCompile(`(?m)^func\s+`)
	reGoType     = regexp.MustCompile(`(?m)^type\s+\w+`)
	reGoImport   = regexp.MustCompile(`(?m)^import\s+`)
)

func SmartSummary(source, lang string) string {
	clean := util.StripAnsi(source)
	lineCount := len(strings.Split(clean, "\n"))

	switch {
	case lang == "tsx" || lang == "jsx":
		props := count(clean, reTsxProps)
		hooks := count(clean, reHooks)
		isComponent := reComponent.MatchString(clean) || reReturnJSX.MatchString(clean)
		renders := "JSX content"
		if isComponent {
			renders = "a component"
		}
		return fmt.Sprintf("React component: %d props, %d hooks, renders %s\n%d lines total", props, hooks, renders, lineCount)

	case lang == "ts" || lang == "js":
		classes := count(clean, reClass)
		interfaces := count(clean, reInterface)
		exports := count(clean, reExport)
		functions := count(clean, reFunction)
		asyncFns := count(clean, reAsync)
		imports := count(clean, reImportLine)
		if classes > 0 {
			return fmt.Sprintf("TypeScript class: %d classes, %d interfaces, %d imports\n%d functions (%d async), %d lines", classes, interfaces, imports, functions, asyncFns, lineCount)
		}
		return fmt.Sprintf("Node module: %d exports, %d async functions, %d imports\n%d functions, %d lines", exports, asyncFns, imports, functions, lineCount)

	case lang == "py":
		return fmt.Sprintf("Python module: %d classes, %d functions, %d imports\n%d lines total", count(clean, rePyClass), count(clean, rePyDef), count(clean, rePyImport), lineCount)

	case lang == "go":
		return fmt.Sprintf("Go file: %d functions, %d types, %d import blocks\n%d lines total", count(clean, reGoFunc), count(clean, reGoType), count(clean, reGoImport), lineCount)

	default:
		name := lang
		if name == "" {
			name = "text"
		}
		return fmt.Sprintf("%s file: %d lines, %d bytes", name, lineCount, len(clean))
	}
}

func count(s string, re *regexp.Regexp) int { return len(re.FindAllString(s, -1)) }

var (
	tsRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?`)
	idRe = regexp.MustCompile(`(?i)\b[0-9a-f]{8,}\b`)
)

// DeduplicateLines collapses repeated log lines (ignoring timestamps and hex IDs) into a
// single line with an occurrence count, ordered most-frequent first.
func DeduplicateLines(raw string) string {
	lines := strings.Split(util.StripAnsi(raw), "\n")

	type entry struct {
		display string
		count   int
	}
	var order []string
	counts := map[string]*entry{}

	for _, line := range lines {
		normalized := idRe.ReplaceAllString(tsRe.ReplaceAllString(line, "<TS>"), "<ID>")
		normalized = strings.TrimSpace(normalized)
		if normalized == "" {
			continue
		}
		if e, ok := counts[normalized]; ok {
			e.count++
		} else {
			counts[normalized] = &entry{display: line, count: 1}
			order = append(order, normalized)
		}
	}

	entries := make([]*entry, len(order))
	for i, k := range order {
		entries[i] = counts[k]
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].count > entries[j].count })

	out := make([]string, len(entries))
	for i, e := range entries {
		if e.count >= 2 {
			out[i] = fmt.Sprintf("%s (×%d)", e.display, e.count)
		} else {
			out[i] = e.display
		}
	}
	return strings.Join(out, "\n")
}

// ExtractStructure reduces a decoded JSON value to its shape: arrays keep one sample
// element, objects keep their keys, and scalars become their type name.
func ExtractStructure(value any) any { return extractStructure(value, 0) }

func extractStructure(value any, depth int) any {
	if depth > 6 {
		return "<...>"
	}
	switch v := value.(type) {
	case nil:
		return "null"
	case []any:
		if len(v) == 0 {
			return []any{}
		}
		return []any{extractStructure(v[0], depth+1)}
	case map[string]any:
		out := map[string]any{}
		for k, val := range v {
			out[k] = extractStructure(val, depth+1)
		}
		return out
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	default:
		return "object"
	}
}
