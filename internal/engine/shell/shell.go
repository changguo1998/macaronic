// Package shell implements the macaronic shell engine: it infers
// contract-variable reads/writes in a Bash block and injects read and
// write code that goes through the `macaronic codec` helper (never
// pure Bash binary parsing, since NUL bytes are not addressable in
// Bash).
package shell

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
// RunCommand invokes and that source-map entries and Bash diagnostics
// reference.
const genFile = "run.sh"

// Engine is the Bash backend.
type Engine struct{}

// Name implements engine.Engine.
func (Engine) Name() string { return "shell" }

// writeRe matches a Bash assignment at line start.
var writeRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=`)

// rawRe matches $name (without braces).
var rawRe = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

// braceRe matches ${name}.
var braceRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// arrayBraceRe matches ${name[@]} and ${name[index]} reads.
var arrayBraceRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\[[^]]*\]\}`)

var shellIdentRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// isWordChar reports whether b continues an identifier.
func isWordChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' ||
		b >= '0' && b <= '9' || b == '_'
}

// Analyze implements engine.Engine.
//
// Reads are $name / ${name}; writes are leading `name=...`. Only
// contract variable names count; the contract type drives the
// conversion (Bash itself is untyped). No shadowing to report for
// shell (assigning a contract variable IS the intended write).
func (Engine) Analyze(st *ir.Stage, c ir.Contract) (ir.VarSet, ir.VarSet, error) {
	a := (Engine{}).AnalyzeDetailed(st, c)
	return a.Reads, a.Writes, nil
}

// AnalyzeDetailed scans shell usage and records the first body-relative span
// for every inferred read/write. Shell has no static diagnostics of its own.
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
		for _, idx := range arrayBraceRe.FindAllStringSubmatchIndex(line, -1) {
			name := line[idx[2]:idx[3]]
			if _, ok := c[name]; ok {
				a.Reads[name] = true
				rememberSpan(a.ReadSpans, name, lineSpan(i, idx[2], len(name)))
			}
		}
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

// readBuiltinVars returns contract variables populated by Bash read.
func readBuiltinVars(line string, c ir.Contract) ir.VarSet {
	vars := ir.VarSet{}
	t := strings.TrimSpace(line)
	if len(t) < 4 || t[:4] != "read" || (len(t) > 4 && isWordChar(t[4])) {
		return vars
	}
	fields := strings.Fields(t[4:])
	valueFlags := map[string]bool{"a": true, "d": true, "n": true, "N": true, "p": true, "t": true, "u": true}
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
		if !shellIdentRe.MatchString(field) || shellIdentRe.FindString(field) != field {
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
		for _, idx := range shellIdentRe.FindAllStringIndex(line[i+3:end], -1) {
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
func (Engine) Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string,
	sm *ir.SourceMap) error {

	reads, writes := scan(st.Body, c)
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

	write("#!/usr/bin/env bash\n")
	write("set -eu\n")
	write("\n")

	// Prologue: load contract variables into shell variables.
	for _, name := range sortedVars(reads) {
		f := filepath.Join(stateDir, stateFileName(name, c[name]))
		if ir.IsList(c[name]) {
			write(fmt.Sprintf("mapfile -d '' -t %s < <(macaronic codec read-list %q %s)\n",
				name, f, string(c[name])))
		} else {
			write(fmt.Sprintf("%s=$(macaronic codec read %q %s)\n",
				name, f, string(c[name])))
		}
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

	// Epilogue: persist contract variables.
	for _, name := range sortedVars(writes) {
		f := filepath.Join(stateDir, stateFileName(name, c[name]))
		if ir.IsList(c[name]) {
			write(fmt.Sprintf("macaronic codec write-list %q %s \"${%s[@]}\"\n",
				f, string(c[name]), name))
		} else {
			write(fmt.Sprintf("macaronic codec write %q %s \"${%s-}\"\n",
				f, string(c[name]), name))
		}
	}

	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("shell emit: mkdir: %v", err)
	}
	return os.WriteFile(filepath.Join(stageDir, genFile), []byte(b.String()), 0o755)
}

// RunCommand implements engine.Engine.
func (Engine) RunCommand(stageDir string) []string {
	return []string{"bash", genFile}
}

// diagRe matches Bash diagnostics of the form "file: line N: message".
var diagRe = regexp.MustCompile(`^(?:\./)?([^:]+): line (\d+): (.*)$`)

// ParseDiagnostics implements engine.Engine, parsing Bash stderr
// lines like "run.sh: line 7: foo: command not found". The returned
// Diagnostic encodes the generated location as
// "genFile:genLine: message" (Span stays nil); the M9 runner splits
// it and resolves genLine through the source-map.
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

func sortedVars(v ir.VarSet) []string {
	ks := make([]string, 0, len(v))
	for k := range v {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
