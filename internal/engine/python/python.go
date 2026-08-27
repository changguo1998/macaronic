// Package python implements the Python language engine: contract
// variables must carry type annotations (x: int), missing annotation
// is a check error; Emit injects prologue reads / epilogue writes
// using an inline codec, and ParseDiagnostics understands Python
// tracebacks.
package python

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/changguo1998/macaronic/internal/engine"
	"github.com/changguo1998/macaronic/internal/ir"
)

// Engine is the Python backend.
type Engine struct{}

// Name returns "python".
func (Engine) Name() string { return "python" }

// varRefRe matches a contract var as a whole identifier token.
func varRefRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
}

const pythonAnnoType = `(?:int|float|bool|str|list\[(?:int|float|bool|str)\])`

// annoRe matches a declaration line "name: type" (optionally followed
// by "= ...").
func annoRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) +
		`\s*:\s*` + pythonAnnoType + `\s*(=|$)`)
}

// assignRe matches an annotated assignment "name: type = ...".
func assignRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) +
		`\s*:\s*` + pythonAnnoType + `\s*=`)
}

// plainAssignRe matches a non-annotated plain assignment
// "name = ..." (excludes "==" via character class).
func plainAssignRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) + `\s*=[^=]`)
}

// augmentedAssignRe matches "name += ..." and friends.
func augmentedAssignRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) +
		`\s*(?:\+=|-=|\*=|/=|//=|%=|&=|\|=|\^=|<<=|>>=)`)
}

// subscriptAssignRe matches an in-place assignment such as v[i] = x.
// Subscript mutation consumes the old value as well as producing a new one.
func subscriptAssignRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) +
		`\s*\[[^]]*\]\s*(?:=[^=]|(?:\+=|-=|\*=|/=|%=))`)
}

var defStartRe = regexp.MustCompile(`^\s*def\s+[A-Za-z_][A-Za-z0-9_]*\s*\(`)

// Analyze scans the block for contract-variable usage. Each contract
// variable referenced anywhere must have an annotation line; a bare
// use without annotation is a check error carrying the first used
// line. Sets follow the model: a variable is a write when the block
// has an annotated assignment “v: type = expr“ (not loaded from
// state; the stage produces it); otherwise, when referenced with a
// declaration “v: type“ or in expressions, it is a read and requires
// an annotation somewhere in the block.
func (Engine) Analyze(st *ir.Stage, c ir.Contract) (ir.VarSet, ir.VarSet, error) {
	a := (Engine{}).AnalyzeDetailed(st, c)
	return a.Reads, a.Writes, a.Error(st)
}

// AnalyzeDetailed scans the block and records source spans for inferred
// reads/writes and static diagnostics. Spans are body-relative.
func (Engine) AnalyzeDetailed(st *ir.Stage, c ir.Contract) engine.Analysis {
	a := engine.Analysis{
		Reads:      ir.VarSet{},
		Writes:     ir.VarSet{},
		ReadSpans:  map[string]*ir.Span{},
		WriteSpans: map[string]*ir.Span{},
	}
	names := make([]string, 0, len(c))
	for v := range c {
		names = append(names, v)
	}
	sort.Strings(names)
	// Function parameters create local bindings. Detect contract-name
	// parameters before the normal annotation check so shadowing wins.
	for _, v := range names {
		if span := defParamShadowSpan(st.Body, v); span != nil {
			a.Diagnostics = []ir.Diagnostic{{Var: v, Span: span,
				Msg: fmt.Sprintf("python: contract variable %q used as a function parameter shadows the contract binding", v)}}
			return a
		}
	}
	for _, v := range names {
		refRe := varRefRe(v)
		anno := annoRe(v)
		ass := assignRe(v)
		hasAnno := false
		hasWrite := false
		firstIsWrite := false
		firstRef := -1
		firstWrite := -1
		for i, ln := range st.Body {
			if !refRe.MatchString(ln) {
				continue
			}
			if firstRef < 0 {
				firstRef = i
				end := logicalSpan(st.Body, i)
				firstIsWrite = (ass.MatchString(ln) || plainAssignRe(v).MatchString(ln)) &&
					!rhsHasRefSpan(st.Body, i, end, v)
			}
			if anno.MatchString(ln) {
				hasAnno = true
			}
			if ass.MatchString(ln) || plainAssignRe(v).MatchString(ln) ||
				augmentedAssignRe(v).MatchString(ln) || subscriptAssignRe(v).MatchString(ln) {
				hasWrite = true
				if firstWrite < 0 {
					firstWrite = i
				}
			}
			if subscriptAssignRe(v).MatchString(ln) {
				a.Reads[v] = true
				if _, ok := a.ReadSpans[v]; !ok {
					a.ReadSpans[v] = lineSpan(ln, i, v)
				}
			}
		}
		if firstRef < 0 {
			continue
		}
		if !hasAnno {
			a.Diagnostics = []ir.Diagnostic{{Var: v, Span: lineSpan(st.Body[firstRef], firstRef, v),
				Msg: fmt.Sprintf("python: contract variable %q used without type annotation. Add `%s: %s`", v, v, c[v])}}
			return a
		}
		if !firstIsWrite {
			a.Reads[v] = true
			if _, ok := a.ReadSpans[v]; !ok {
				a.ReadSpans[v] = lineSpan(st.Body[firstRef], firstRef, v)
			}
		}
		if hasWrite {
			a.Writes[v] = true
			if firstWrite >= 0 {
				a.WriteSpans[v] = lineSpan(st.Body[firstWrite], firstWrite, v)
			}
		}
	}
	return a
}

// lineSpan returns a body-relative span for the first textual occurrence of name.
func lineSpan(line string, bodyLine int, name string) *ir.Span {
	col := strings.Index(line, name)
	if col < 0 {
		col = 0
	}
	return &ir.Span{StartLine: bodyLine + 1, StartCol: col + 1,
		EndLine: bodyLine + 1, EndCol: col + len(name) + 1}
}

// rhsHasRef reports whether v appears after the first '=' on the
// line, i.e. on the right-hand side of an assignment.

// rhsHasRefSpan extends RHS detection across a parenthesized logical line.
func rhsHasRefSpan(lines []string, start, end int, v string) bool {
	if start < 0 || start >= len(lines) {
		return false
	}
	i := strings.Index(lines[start], "=")
	if i < 0 {
		return false
	}
	text := lines[start][i+1:]
	for j := start + 1; j <= end && j < len(lines); j++ {
		text += "\n" + lines[j]
	}
	return varRefRe(v).MatchString(text)
}

// logicalSpan returns the final physical line of a parenthesized statement.
func logicalSpan(lines []string, start int) int {
	end := start
	depth := 0
	for ; end < len(lines); end++ {
		depth += delimiterBalance(lines[end])
		if depth <= 0 && end >= start {
			return end
		}
	}
	return len(lines) - 1
}

func delimiterBalance(line string) int {
	return strings.Count(line, "(") + strings.Count(line, "[") + strings.Count(line, "{") -
		strings.Count(line, ")") - strings.Count(line, "]") - strings.Count(line, "}")
}

// defParamShadow reports whether name is a parameter of any def statement.
// defParamShadow reports whether name is a parameter of any def statement.

// defParamShadowSpan locates the first parameter that shadows name.
func defParamShadowSpan(lines []string, name string) *ir.Span {
	paramRe := regexp.MustCompile(`(?:^|,)\s*\*{0,2}` + regexp.QuoteMeta(name) + `\s*(?:,|=|:|$)`)
	for i, line := range lines {
		if !defStartRe.MatchString(line) {
			continue
		}
		start := strings.Index(line, "(")
		if start < 0 {
			continue
		}
		end := matchingParenEnd(lines, i, start)
		if end < 0 {
			continue
		}
		text := lines[i][start+1:]
		for j := i + 1; j <= end && j < len(lines); j++ {
			text += "\n" + lines[j]
		}
		close := strings.LastIndex(text, ")")
		if close >= 0 {
			text = text[:close]
		}
		match := paramRe.FindStringIndex(text)
		if match == nil {
			continue
		}
		pos := match[0]
		lineOffset := strings.Count(text[:pos], "\n")
		col := pos
		if nl := strings.LastIndex(text[:pos], "\n"); nl >= 0 {
			col = pos - nl - 1
		}
		if lineOffset == 0 {
			col += start + 1
		}
		return &ir.Span{StartLine: i + lineOffset + 1, StartCol: col + 1,
			EndLine: i + lineOffset + 1, EndCol: col + len(name) + 1}
	}
	return nil
}

func matchingParenEnd(lines []string, line, col int) int {
	depth := 0
	for i := line; i < len(lines); i++ {
		text := lines[i]
		if i == line {
			text = text[col:]
		}
		for _, r := range text {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return i
				}
			}
		}
	}
	return -1
}

// genFile is the emitted Python file name.
const genFile = "run.py"

// stateFileName is the state file naming convention shared by all
// engines: <name>.mac<type> (e.g. count.macint). Cross-engine state
// interop depends on this exact form.
func stateFileName(name string, t ir.BasicType) string {
	return name + ".mac" + string(t)
}

// Emit writes stageDir/run.py: prologue reads, user body, epilogue
// writes. State files live at stateDir/<var> and are encoded with the
// inline codec helpers below. Source-map entries: user lines are
// OrigSource, everything generated is OrigSynthetic.
func (e Engine) Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string, sm *ir.SourceMap) error {
	if sm != nil && *sm == nil {
		*sm = ir.SourceMap{}
	}
	reads, writes, err := e.Analyze(st, c)
	if err != nil {
		return err
	}

	var b strings.Builder
	line := 0
	emit := func(s string, srcLine int, kind ir.OriginKind) {
		b.WriteString(s)
		b.WriteString("\n")
		line++
		if sm != nil && srcLine > 0 {
			(*sm)[fmt.Sprintf("%s:%d", genFile, line)] = ir.SourceMapEntry{
				SourceLine: srcLine, Kind: kind}
		}
	}

	// Header + inline codec (synthetic).
	emit("# generated by macaronic - do not edit", 0, ir.OrigSynthetic)
	emit("import struct", 0, ir.OrigSynthetic)
	synthetic := []string{
		"def _mac_read_scalar(f, typ):",
		"    if typ == 'int':",
		"        return struct.unpack('<q', f.read(8))[0]",
		"    if typ == 'float':",
		"        return struct.unpack('<d', f.read(8))[0]",
		"    if typ == 'bool':",
		"        return struct.unpack('<?', f.read(1))[0]",
		"    if typ == 'str':",
		"        n = struct.unpack('<I', f.read(4))[0]",
		"        return f.read(n).decode('utf-8')",
		"    raise ValueError(typ)",
		"",
		"def _mac_read(path, typ):",
		"    with open(path, 'rb') as f:",
		"        if typ.endswith('[]'):",
		"            n = struct.unpack('<I', f.read(4))[0]",
		"            if n > 1048576:",
		"                raise ValueError('list too long')",
		"            values = []",
		"            for _ in range(n):",
		"                value = _mac_read_scalar(f, typ[:-2])",
		"                if typ[:-2] == 'str' and '\\x00' in value:",
		"                    raise ValueError('NUL string element')",
		"                values.append(value)",
		"            return values",
		"        return _mac_read_scalar(f, typ)",
		"",
		"def _mac_write_scalar(typ, v):",
		"    if typ == 'int':",
		"        return struct.pack('<q', int(v))",
		"    if typ == 'float':",
		"        return struct.pack('<d', float(v))",
		"    if typ == 'bool':",
		"        return struct.pack('<?', bool(v))",
		"    if typ == 'str':",
		"        b = str(v).encode('utf-8')",
		"        return struct.pack('<I', len(b)) + b",
		"    raise ValueError(typ)",
		"",
		"def _mac_write(path, typ, v):",
		"    if typ.endswith('[]'):",
		"        if len(v) > 1048576:",
		"            raise ValueError('list too long')",
		"        if any('\\x00' in str(x) for x in v):",
		"            raise ValueError('NUL string element')",
		"        data = struct.pack('<I', len(v))",
		"        for item in v:",
		"            data += _mac_write_scalar(typ[:-2], item)",
		"    else:",
		"        data = _mac_write_scalar(typ, v)",
		"    with open(path, 'wb') as f:",
		"        f.write(data)",
		"",
	}
	for _, l := range synthetic {
		emit(l, 0, ir.OrigSynthetic)
	}

	// Prologue reads (deterministic variable order).
	vars := sortedKeys(reads)
	if len(vars) > 0 {
		emit("", 0, ir.OrigSynthetic)
	}
	for _, name := range vars {
		emit(fmt.Sprintf("%s = _mac_read(%q, %q)", name,
			filepath.Join(stateDir, stateFileName(name, c[name])), string(c[name])), 0, ir.OrigSynthetic)
	}

	// User code (OrigSource per body line).
	for i, ln := range st.Body {
		emit(ln, st.StartLine+1+i, ir.OrigSource)
	}

	// Epilogue writes.
	vars = sortedKeys(writes)
	if len(vars) > 0 {
		emit("", 0, ir.OrigSynthetic)
	}
	for _, name := range vars {
		emit(fmt.Sprintf("_mac_write(%q, %q, %s)", filepath.Join(stateDir, stateFileName(name, c[name])),
			string(c[name]), name), 0, ir.OrigSynthetic)
	}

	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stageDir, genFile), []byte(b.String()), 0o644)
}

// RunCommand returns the argv invoking the generated file.
func (Engine) RunCommand(stageDir string) []string {
	return []string{"python3", filepath.Join(stageDir, "run.py")}
}

// fileLineRe matches a traceback frame line like
//
//	File "run.py", line 3, in <module>
var fileLineRe = regexp.MustCompile(`^\s*File "([^"]+)", line (\d+)`)

// ParseDiagnostics extracts (genFile, line, message) from a Python
// traceback. The last File frame defines the failing generated line;
// the trailing exception line supplies the message.
func (Engine) ParseDiagnostics(stderr []byte) []ir.Diagnostic {
	var out []ir.Diagnostic
	lines := strings.Split(string(stderr), "\n")
	gen, line := "", 0
	for _, l := range lines {
		if m := fileLineRe.FindStringSubmatch(l); m != nil {
			gen, line = m[1], atoi(m[2])
			continue
		}
		if strings.HasPrefix(l, "Traceback") || strings.HasPrefix(l, "  ") ||
			l == "" || gen == "" {
			continue
		}
		if !strings.HasPrefix(l, "File ") {
			// top exception line
			msg := strings.TrimSpace(l)
			out = append(out, ir.Diagnostic{
				Msg: fmt.Sprintf("%s:%d: %s", gen, line, msg),
				Span: &ir.Span{StartLine: line, StartCol: 1,
					EndLine: line, EndCol: 1},
			})
			return out
		}
	}
	if line == 0 {
		return nil
	}
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func sortedKeys(m ir.VarSet) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
