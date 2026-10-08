// Package zsh implements the macaronic zsh engine: it infers
// contract-variable reads/writes in a zsh block and injects read and
// write code that goes through the `macaronic codec` helper (never
// pure zsh binary parsing, since NUL bytes cannot be held in any shell
// variable).
//
// Scalars and one-dimensional lists are both supported. Two zsh
// specific hazards were measured before writing this engine and are
// handled explicitly:
//
//   - zsh has no `mapfile`, so a list prologue reads elements with
//     `while IFS= read -r -d ”` and appends them to the array.
//   - zsh enables `nomatch` by default, so an unquoted `[]` (the list
//     type `int[]`, or a state path ending in `[]`) is a failed glob.
//     Inside a process substitution that failure does NOT set the outer
//     exit status, so it silently yielded an empty array and exit 0.
//     Every state path and type argument is therefore quoted.
//   - `${name[@]}` on an unset array is a hard `parameter not set`
//     error under `set -u`, where bash expands to zero arguments. A
//     write-only list never gets a prologue, so the list epilogue
//     defines an empty array first.
package zsh

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
// RunCommand invokes and that source-map entries and zsh diagnostics
// reference.
const genFile = "run.sh"

// listItemVar is the scratch variable a list prologue reads elements
// into. The name is deliberately unlikely to collide with user code.
const listItemVar = "__macaronic_item"

// Injected failure messages. Every dialect emits the same text so one
// symptom reads the same everywhere (docs/architecture.md §12).
const (
	listReadMsg      = "macaronic: stage %d: cannot read contract variable %q (%s)"
	epilogueUnsetMsg = "macaronic: stage %d: contract variable %q is unset at epilogue"
)

// scratchName is the per-stage scratch file one list prologue reads
// through; stageDir is rebuilt on every build.
func scratchName(name string) string { return ".prologue-" + name + ".tmp" }

// Engine is the zsh backend.
type Engine struct{}

// Name implements engine.Engine.
func (Engine) Name() string { return "zsh" }

// RequiredCommands implements engine.RuntimeChecker: run.sh is invoked
// through zsh (see RunCommand), so zsh must be on PATH.
func (Engine) RequiredCommands() []string { return []string{"zsh"} }

// writeRe matches a zsh assignment at line start.
var writeRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=`)

// appendRe matches a `name+=...` append at line start. An append reads
// the existing value and writes the result back, so the variable is
// both read and written. This matters even more in zsh than in bash,
// because zsh arrays are appended to with `name+=(...)`.
var appendRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\+=`)

// rawRe matches $name (without braces).
var rawRe = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

// braceRe matches ${name}.
var braceRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// subscriptRe matches ${name[@]} and ${name[index]} reads.
var subscriptRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\[[^]]*\]\}`)

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// isWordChar reports whether b continues an identifier.
func isWordChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' ||
		b >= '0' && b <= '9' || b == '_'
}

// Analyze implements engine.Engine.
//
// Reads are $name / ${name}; writes are leading `name=...` and
// leading `name+=...`. Only contract variable names count; the
// contract type drives the conversion (zsh itself is untyped). No
// shadowing to report for zsh: assigning a contract variable IS the
// intended write.
func (Engine) Analyze(st *ir.Stage, c ir.Contract) (ir.VarSet, ir.VarSet, error) {
	a := (Engine{}).AnalyzeDetailed(st, c)
	return a.Reads, a.Writes, a.Error(st)
}

// AnalyzeDetailed scans zsh usage and records the first body-relative
// span for every inferred read/write. zsh has no static diagnostics of
// its own.
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
		if m := appendRe.FindStringSubmatchIndex(line); len(m) > 0 {
			name := line[m[2]:m[3]]
			if _, ok := c[name]; ok {
				a.Reads[name] = true
				a.Writes[name] = true
				rememberSpan(a.ReadSpans, name, lineSpan(i, m[2], len(name)))
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

// readBuiltinVars returns contract variables populated by zsh read.
// zsh read has no -a flag (an array is read with -A), so the value
// flags differ from bash's.
func readBuiltinVars(line string, c ir.Contract) ir.VarSet {
	vars := ir.VarSet{}
	t := strings.TrimSpace(line)
	if len(t) < 4 || t[:4] != "read" || (len(t) > 4 && isWordChar(t[4])) {
		return vars
	}
	fields := strings.Fields(t[4:])
	valueFlags := map[string]bool{"d": true, "k": true, "p": true, "t": true, "u": true}
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
// Every state path and type argument is quoted: zsh's default nomatch
// turns an unquoted `[]` into a failed glob, and inside the list
// prologue's process substitution that failure is silent (empty array,
// exit 0) rather than loud.
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

	write("#!/usr/bin/env zsh\n")
	write("set -eu\n")
	write("\n")

	// Prologue: load contract variables into zsh variables.
	for _, name := range sortedUnion(reads) {
		f := filepath.Join(stateDir, stateFileName(name, c[name]))
		if ir.IsList(c[name]) {
			// zsh has no mapfile: read NUL-separated elements one at a
			// time and append them. Both the path and the type are
			// quoted (see the nomatch note in the package comment).
			// Process substitution would hide a read-list failure (the
			// array silently ends up empty, exit code stays 0), so the
			// output goes through a stage-private scratch file whose
			// exit status is checked.
			tmp := filepath.Join(stageDir, scratchName(name))
			write(fmt.Sprintf("if ! macaronic codec read-list %q %q > %q; then\n",
				f, string(c[name]), tmp))
			write(fmt.Sprintf("  rm -f %q\n", tmp))
			write(fmt.Sprintf("  echo '%s' >&2\n",
				fmt.Sprintf(listReadMsg, st.Index, name, c[name])))
			write("  exit 1\n")
			write("fi\n")
			write(fmt.Sprintf("%s=()\n", name))
			write(fmt.Sprintf("while IFS= read -r -d '' %s; do %s+=(\"$%s\"); done < %q\n",
				listItemVar, name, listItemVar, tmp))
			write(fmt.Sprintf("rm -f %q\n", tmp))
		} else {
			write(fmt.Sprintf("%s=$(macaronic codec read %q %q)\n",
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
	for _, name := range sortedUnion(writes) {
		f := filepath.Join(stateDir, stateFileName(name, c[name]))
		if ir.IsList(c[name]) {
			// A write-only list has no prologue, so the array may be
			// unset; under set -u zsh treats "${name[@]}" on an unset
			// array as a hard error where bash yields zero arguments.
			// Defining it empty keeps the two dialects consistent.
			write(fmt.Sprintf("(( ${+%s} )) || %s=()\n", name, name))
			write(fmt.Sprintf("macaronic codec write-list %q %q \"${%s[@]}\"\n",
				f, string(c[name]), name))
		} else {
			// A declared write the body never assigned is an error, as
			// in every dialect (M20): the message is the shared one.
			write(fmt.Sprintf("(( ${+%s} )) || { echo '%s' >&2; exit 1; }\n",
				name, fmt.Sprintf(epilogueUnsetMsg, st.Index, name)))
			write(fmt.Sprintf("macaronic codec write %q %q \"$%s\"\n",
				f, string(c[name]), name))
		}
	}

	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("zsh emit: mkdir: %v", err)
	}
	return os.WriteFile(filepath.Join(stageDir, genFile), []byte(b.String()), 0o755)
}

// RunCommand implements engine.Engine.
func (Engine) RunCommand(stageDir string) []string {
	return []string{"zsh", genFile}
}

// diagRe matches zsh diagnostics of the form "file:N: message".
var diagRe = regexp.MustCompile(`^(?:\./)?([^:]+):(\d+): (.*)$`)

// ParseDiagnostics implements engine.Engine, parsing zsh stderr lines
// like "run.sh:7: command not found: nosuchcmd". The returned
// Diagnostic encodes the generated location as
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
