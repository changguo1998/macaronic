// Package sh implements the macaronic POSIX shell engine: it infers
// contract-variable reads/writes in a sh block and injects read and
// write code that goes through the `macaronic codec` helper (never
// pure sh binary parsing, since NUL bytes cannot be held in any shell
// variable).
//
// Only scalars are supported. POSIX sh has neither array syntax nor
// process substitution (verified: `a=(1 2 3)` and `< <(...)` are both
// syntax errors under dash), so a one-dimensional list cannot be
// carried across blocks at all. AnalyzeDetailed therefore rejects a
// list-typed contract variable used in a sh block instead of emitting
// code that would silently drop the write — the same "error, never a
// silent skip" stance the python engine takes for a missing type
// annotation. See docs/architecture.md §12.
package sh

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

// genFile is the generated script name (relative to stageDir) that
// RunCommand invokes and that source-map entries and sh diagnostics
// reference.
const genFile = "run.sh"

// epilogueUnsetMsg is the shared injected failure message: every
// dialect emits the same text so one symptom reads the same everywhere
// (docs/architecture.md §12). sh has no list support, so it needs only
// the scalar one.
const epilogueUnsetMsg = "macaronic: stage %d: contract variable %q is unset at epilogue"

// Engine is the POSIX sh backend.
type Engine struct{}

// Name implements engine.Engine.
func (Engine) Name() string { return "sh" }

// RequiredCommands implements engine.RuntimeChecker: run.sh is invoked
// through sh (see RunCommand), so sh must be on PATH.
func (Engine) RequiredCommands() []string { return []string{"sh"} }

// writeRe matches a sh assignment at line start.
var writeRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=`)

// rawRe matches $name (without braces).
var rawRe = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

// braceRe matches ${name}.
var braceRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// subscriptRe matches ${name[...]} reads. POSIX sh has no arrays, so
// this only fires for list-typed contract variables, which
// AnalyzeDetailed then rejects.
var subscriptRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\[[^]]*\]\}`)

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// isWordChar reports whether b continues an identifier.
func isWordChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' ||
		b >= '0' && b <= '9' || b == '_'
}

// Analyze implements engine.Engine.
//
// Reads are $name / ${name}; writes are leading `name=...`. Only
// contract variable names count; the contract type drives the
// conversion (sh itself is untyped). No shadowing to report for sh:
// assigning a contract variable IS the intended write.
func (Engine) Analyze(st *ir.Stage, c ir.Contract) (ir.VarSet, ir.VarSet, error) {
	a := (Engine{}).AnalyzeDetailed(st, c)
	return a.Reads, a.Writes, a.Error(st)
}

// AnalyzeDetailed scans sh usage and records the first body-relative
// span for every inferred read/write, plus the list-type rejection
// diagnostic when the stage touches a list-typed contract variable.
func (Engine) AnalyzeDetailed(st *ir.Stage, c ir.Contract) engine.Analysis {
	a := engine.Analysis{
		Reads:      ir.VarSet{},
		Writes:     ir.VarSet{},
		ReadSpans:  map[string]*ir.Span{},
		WriteSpans: map[string]*ir.Span{},
	}
	for i, line := range st.Body {
		if m := writeRe.FindStringSubmatchIndex(line); len(m) > 0 {
			name := line[m[2]:m[3]]
			if _, ok := c[name]; ok {
				a.Writes[name] = true
				rememberSpan(a.WriteSpans, name, lineSpan(i, m[2], len(name)))
			}
		}
		for name := range readBuiltinVars(line, c) {
			a.Writes[name] = true
			rememberSpan(a.WriteSpans, name, lineSpan(i, strings.Index(line, name), len(name)))
		}
		for name := range arithmeticVars(line, c) {
			a.Reads[name] = true
			rememberSpan(a.ReadSpans, name, lineSpan(i, arithmeticIndex(line, name), len(name)))
		}
		for _, idx := range rawRe.FindAllStringSubmatchIndex(line, -1) {
			name := line[idx[2]:idx[3]]
			if idx[3] < len(line) && isWordChar(line[idx[3]]) {
				continue
			}
			if _, ok := c[name]; ok {
				a.Reads[name] = true
				rememberSpan(a.ReadSpans, name, lineSpan(i, idx[2], len(name)))
			}
		}
		for _, idx := range braceRe.FindAllStringSubmatchIndex(line, -1) {
			name := line[idx[2]:idx[3]]
			if _, ok := c[name]; ok {
				a.Reads[name] = true
				rememberSpan(a.ReadSpans, name, lineSpan(i, idx[2], len(name)))
			}
		}
		for _, idx := range subscriptRe.FindAllStringSubmatchIndex(line, -1) {
			name := line[idx[2]:idx[3]]
			if _, ok := c[name]; ok {
				a.Reads[name] = true
				rememberSpan(a.ReadSpans, name, lineSpan(i, idx[2], len(name)))
			}
		}
	}
	return rejectLists(a, c)
}

// rejectLists turns the first list-typed contract variable this stage
// reads or writes into a blocking diagnostic. Because the diagnostic
// carries a Span, the framework reports the original .mac line and
// suppresses the M12 "may not be injected" warning for this stage.
//
// Names are visited in sorted order so multi-list stages report
// deterministically.
func rejectLists(a engine.Analysis, c ir.Contract) engine.Analysis {
	for _, name := range sortedUnion(a.Reads, a.Writes) {
		t, ok := c[name]
		if !ok || !ir.IsList(t) {
			continue
		}
		span := a.ReadSpans[name]
		if span == nil {
			span = a.WriteSpans[name]
		}
		a.Diagnostics = []ir.Diagnostic{{
			Var:  name,
			Span: span,
			Msg: fmt.Sprintf("sh: contract variable %q has list type %s, which a sh block cannot carry (POSIX sh has no arrays); move it to another block or split it into scalar variables",
				name, t),
		}}
		return a
	}
	return a
}

func scan(lines []string, c ir.Contract) (reads, writes ir.VarSet) {
	a := (Engine{}).AnalyzeDetailed(&ir.Stage{Body: lines}, c)
	return a.Reads, a.Writes
}

func lineSpan(bodyLine, col, width int) *ir.Span {
	if col < 0 {
		col = 0
	}
	return &ir.Span{StartLine: bodyLine + 1, StartCol: col + 1,
		EndLine: bodyLine + 1, EndCol: col + width + 1}
}

func rememberSpan(spans map[string]*ir.Span, name string, span *ir.Span) {
	if span != nil {
		if _, exists := spans[name]; !exists {
			spans[name] = span
		}
	}
}

func arithmeticIndex(line, name string) int {
	start := strings.Index(line, "$((")
	if start >= 0 {
		if p := strings.Index(line[start+3:], name); p >= 0 {
			return start + 3 + p
		}
	}
	return strings.Index(line, name)
}

// readBuiltinVars returns contract variables populated by sh read.
func readBuiltinVars(line string, c ir.Contract) ir.VarSet {
	vars := ir.VarSet{}
	t := strings.TrimSpace(line)
	if len(t) < 4 || t[:4] != "read" || (len(t) > 4 && isWordChar(t[4])) {
		return vars
	}
	fields := strings.Fields(t[4:])
	valueFlags := map[string]bool{"d": true, "n": true, "p": true, "t": true, "u": true}
	skip := false
	for _, field := range fields {
		if skip {
			skip = false
			continue
		}
		if strings.HasPrefix(field, "-") {
			flag := strings.TrimPrefix(field, "-")
			if len(flag) == 1 && valueFlags[flag] {
				skip = true
			}
			continue
		}
		if !identRe.MatchString(field) || identRe.FindString(field) != field {
			break
		}
		if _, ok := c[field]; ok {
			vars[field] = true
		}
	}
	return vars
}

// arithmeticVars finds bare contract identifiers inside $(( ... )).
func arithmeticVars(line string, c ir.Contract) ir.VarSet {
	vars := ir.VarSet{}
	for start := 0; ; {
		i := strings.Index(line[start:], "$(")
		if i < 0 {
			break
		}
		i += start
		if i+3 > len(line) || line[i:i+3] != "$("+"(" {
			start = i + 2
			continue
		}
		end := strings.Index(line[i+3:], "))")
		if end < 0 {
			end = len(line) - i - 3
		}
		end += i + 3
		for _, idx := range identRe.FindAllStringIndex(line[i+3:end], -1) {
			name := line[i+3+idx[0] : i+3+idx[1]]
			if _, ok := c[name]; ok {
				vars[name] = true
			}
		}
		if end >= len(line) {
			break
		}
		start = end + 2
	}
	return vars
}

// Emit implements engine.Engine: writes stageDir/run.sh containing a
// read prologue, the verbatim user body, and a write epilogue, all
// going through the codec helper CLI. For determinism, injected
// variable orders are sorted by name.
//
// Emit is only reached after a passing check, so list-typed variables
// cannot appear here; the guard below is defence in depth and fails
// loudly rather than emitting code that would drop the value.
func (Engine) Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string,
	sm *ir.SourceMap) error {

	reads, writes := scan(st.Body, c)
	for _, name := range sortedUnion(reads, writes) {
		if t, ok := c[name]; ok && ir.IsList(t) {
			return fmt.Errorf("sh emit: %s: list type %s is not supported",
				name, t)
		}
	}
	genLine := 0

	var b strings.Builder
	write := func(s string) {
		b.WriteString(s)
		if sm != nil {
			(*sm)[key(genFile, genLine+1)] = ir.SourceMapEntry{
				SourceLine: 0, Kind: ir.OrigSynthetic,
			}
		}
		genLine++
	}

	write("#!/bin/sh\n")
	write("set -eu\n")
	write("\n")

	// Prologue: load contract variables into sh variables.
	for _, name := range sortedUnion(reads) {
		f := filepath.Join(stateDir, stateFileName(name, c[name]))
		write(fmt.Sprintf("%s=$(macaronic codec read %q %s)\n",
			name, f, string(c[name])))
	}

	// Verbatim user body, each line mapped back to source.
	for i, l := range st.Body {
		write(l + "\n")
		if sm != nil {
			(*sm)[key(genFile, genLine)] = ir.SourceMapEntry{
				SourceLine: st.StartLine + 1 + i, Kind: ir.OrigSource,
			}
		}
	}

	// Epilogue: persist contract variables. A declared write the body
	// never assigned is an error, as in every dialect (M20): the old
	// "${name-}" default silently wrote an empty str.
	for _, name := range sortedUnion(writes) {
		f := filepath.Join(stateDir, stateFileName(name, c[name]))
		write(fmt.Sprintf("[ -n \"${%s+x}\" ] || { echo '%s' >&2; exit 1; }\n",
			name, fmt.Sprintf(epilogueUnsetMsg, st.Index, name)))
		write(fmt.Sprintf("macaronic codec write %q %s \"$%s\"\n",
			f, string(c[name]), name))
	}

	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("sh emit: mkdir: %v", err)
	}
	return os.WriteFile(filepath.Join(stageDir, genFile), []byte(b.String()), 0o755)
}

// RunCommand implements engine.Engine.
func (Engine) RunCommand(stageDir string) []string {
	return []string{"sh", genFile}
}

// diagRe matches sh diagnostics. dash prints "file: N: message" while
// /bin/sh on some systems is bash, which prints "file: line N:
// message"; both are accepted so back-mapping works either way.
var diagRe = regexp.MustCompile(`^(?:\./)?([^:]+): (?:line )?(\d+): (.*)$`)

// ParseDiagnostics implements engine.Engine, parsing sh stderr lines
// like "run.sh: 7: nosuchcmd: not found" (dash) or
// "run.sh: line 7: nosuchcmd: command not found" (bash as sh). The
// returned Diagnostic encodes the generated location as
// "genFile:genLine: message" (Span stays nil); the runner splits it and
// resolves genLine through the source-map.
func (Engine) ParseDiagnostics(stderr []byte) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, line := range strings.Split(string(stderr), "\n") {
		m := diagRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, ir.Diagnostic{
			Msg: fmt.Sprintf("%s:%s:%s", m[1], m[2], m[3]),
		})
	}
	return out
}

// stateFileName returns the state file name for a variable: name + type.
func stateFileName(name string, t ir.BasicType) string {
	return name + ".mac" + string(t)
}

// key builds the same source-map map key used by internal/sourcemap.
func key(genFile string, genLine int) string {
	return genFile + ":" + strconv.Itoa(genLine)
}

// sortedUnion returns the sorted union of the given variable sets.
func sortedUnion(sets ...ir.VarSet) []string {
	seen := ir.VarSet{}
	for _, s := range sets {
		for k := range s {
			seen[k] = true
		}
	}
	ks := make([]string, 0, len(seen))
	for k := range seen {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
