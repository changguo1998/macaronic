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
	r, w := scan(st.Body, c)
	return r, w, nil
}

func scan(lines []string, c ir.Contract) (reads, writes ir.VarSet) {
	reads, writes = ir.VarSet{}, ir.VarSet{}
	for _, line := range lines {
		if m := writeRe.FindStringSubmatch(line); len(m) > 0 {
			if _, ok := c[m[1]]; ok {
				writes[m[1]] = true
			}
		}
		for name := range readBuiltinVars(line, c) {
			writes[name] = true
		}
		for name := range arithmeticVars(line, c) {
			reads[name] = true
		}
		for _, idx := range rawRe.FindAllStringSubmatchIndex(line, -1) {
			name := line[idx[2]:idx[3]]
			// skip partial matches like $name inside $name2
			if idx[3] < len(line) && isWordChar(line[idx[3]]) {
				continue
			}
			if _, ok := c[name]; ok {
				reads[name] = true
			}
		}
		for _, idx := range braceRe.FindAllStringSubmatchIndex(line, -1) {
			if _, ok := c[line[idx[2]:idx[3]]]; ok {
				reads[line[idx[2]:idx[3]]] = true
			}
		}
	}
	return reads, writes
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

	write("#!/bin/sh\n")
	write("set -eu\n")
	write("\n")

	// Prologue: load contract variables into shell variables.
	for _, name := range sortedVars(reads) {
		f := filepath.Join(stateDir, stateFileName(name, c[name]))
		write(fmt.Sprintf("%s=$(macaronic codec read %q %s)\n",
			name, f, string(c[name])))
	}

	// Verbatim user body, each line mapped back to source.
	for i, l := range st.Body {
		write(l + "\n")
		if sm != nil {
			(*sm)[key(genFile, genLine)] = ir.SourceMapEntry{
				SourceLine: st.StartLine + i, Kind: ir.OrigSource,
			}
		}
	}

	// Epilogue: persist contract variables.
	for _, name := range sortedVars(writes) {
		f := filepath.Join(stateDir, stateFileName(name, c[name]))
		write(fmt.Sprintf("macaronic codec write %q %s \"${%s-}\"\n",
			f, string(c[name]), name))
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
