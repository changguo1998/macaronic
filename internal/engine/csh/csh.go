// Package csh implements the macaronic C shell engine: it infers
// contract-variable reads/writes in a csh block and injects read and
// write code that goes through the `macaronic codec` helper.
//
// Only scalars are supported, for two measured reasons:
//
//   - The codec's list wire format is NUL-separated, and csh cannot
//     hold a NUL in a variable. Feeding the stream to an array splits
//     on whitespace, so an element containing a space silently becomes
//     two elements (`("a" "b b")` read back as 3 elements). Silent
//     corruption is worse than a refusal, so AnalyzeDetailed rejects
//     list-typed contract variables in a csh block.
//   - `[]` is a glob metacharacter in csh. An unmatched glob makes csh
//     skip the command entirely (`No match.`), so the list state path
//     `values.macint[]` and the type argument `int[]` must be quoted;
//     scalar state paths and types are quoted the same way for
//     uniformity.
//
// Two further csh facts shape this engine:
//
//   - csh has no `set -e`. `RunCommand` therefore invokes `tcsh -e`
//     ("exit on any error"): without it a failed command inside the
//     block is ignored and the script still exits 0, which would
//     report a broken stage as successful. The `tcsh` binary is
//     required rather than bsd-csh (which lacks -e); see
//     RequiredCommands.
//   - csh errors carry no file name and no line number for any error
//     class (verified across command-not-found, syntax, undefined
//     variable, division by zero, bad subscript and bad redirection,
//     with `-x` echoing only command text). ParseDiagnostics therefore
//     returns nothing and the runner falls back to raw stderr, so
//     runtime failures are reported as stage-level failures with no
//     guessed line. Static diagnostics still carry exact .mac lines,
//     because those come from AnalyzeDetailed spans rather than from
//     the interpreter.
package csh

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
// RunCommand invokes and that source-map entries reference.
const genFile = "run.csh"

// Engine is the C shell backend.
type Engine struct{}

// Name implements engine.Engine.
func (Engine) Name() string { return "csh" }

// RequiredCommands implements engine.RuntimeChecker. run.csh is
// invoked as `tcsh -e` (see RunCommand), so tcsh is what must be on
// PATH — not csh, which on some systems is bsd-csh and lacks -e.
func (Engine) RequiredCommands() []string { return []string{"tcsh"} }

// setRe matches a csh `set name = value` assignment. csh allows
// whitespace around `=` and does not require it, so `set name=value`
// is accepted too.
var setRe = regexp.MustCompile(`^[ \t]*set[ \t]+([A-Za-z_][A-Za-z0-9_]*)[ \t]*=`)

// atRe matches a csh `@ name = expr` arithmetic assignment, including
// the increment/decrement and op-assign forms.
var atRe = regexp.MustCompile(`^[ \t]*@[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*(?:=|\+\+|--|\+=|-=|\*=|/=|%=)`)

// rawRe matches $name (with or without braces).
var rawRe = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

// braceRe matches ${name}.
var braceRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// metaRe matches the csh meta-variable forms $?name (is-set test) and
// $#name (array length). Both need the value to have been injected, so
// both count as reads.
var metaRe = regexp.MustCompile(`\$[?#]([A-Za-z_][A-Za-z0-9_]*)`)

// subscriptRe matches ${name[...]} reads.
var subscriptRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\[[^]]*\]\}`)

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// isWordChar reports whether b continues an identifier.
func isWordChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' ||
		b >= '0' && b <= '9' || b == '_'
}

// Analyze implements engine.Engine.
//
// Reads are $name / ${name} / $?name / $#name / $name[...]; writes are
// leading `set name = ...` and `@ name ...`. Only contract variable
// names count; the contract type drives the conversion (csh itself is
// untyped). No shadowing to report for csh: assigning a contract
// variable IS the intended write.
func (Engine) Analyze(st *ir.Stage, c ir.Contract) (ir.VarSet, ir.VarSet, error) {
	a := (Engine{}).AnalyzeDetailed(st, c)
	return a.Reads, a.Writes, a.Error(st)
}

// AnalyzeDetailed scans csh usage and records the first body-relative
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
		for _, re := range []*regexp.Regexp{setRe, atRe} {
			if m := re.FindStringSubmatchIndex(line); len(m) > 0 {
				name := line[m[2]:m[3]]
				if _, ok := c[name]; ok {
					a.Writes[name] = true
					rememberSpan(a.WriteSpans, name, lineSpan(i, m[2], len(name)))
				}
			}
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
		for _, idx := range metaRe.FindAllStringSubmatchIndex(line, -1) {
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
			Msg: fmt.Sprintf("csh: contract variable %q has list type %s, which a csh block cannot carry (csh splits the NUL-separated list stream on whitespace, silently splitting elements that contain spaces); move it to another block or split it into scalar variables",
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

// Emit implements engine.Engine: writes stageDir/run.csh containing a
// read prologue, the verbatim user body, and a write epilogue. For
// determinism, injected variable orders are sorted by name.
//
// The prologue wraps the codec call in backticks inside double quotes
// so a value containing spaces survives command substitution, and
// passes the state path and type single-quoted so the `[]` in a
// (rejected but still defensive) list name cannot be taken as a glob.
//
// Emit is only reached after a passing check, so list-typed variables
// cannot appear here; the guard below is defence in depth and fails
// loudly rather than emitting code that would drop the value.
func (Engine) Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string,
	sm *ir.SourceMap) error {

	reads, writes := scan(st.Body, c)
	for _, name := range sortedUnion(reads, writes) {
		if t, ok := c[name]; ok && ir.IsList(t) {
			return fmt.Errorf("csh emit: %s: list type %s is not supported",
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

	// csh has no `set -e`; the shebang documents the file, and
	// RunCommand supplies `tcsh -e` for fail-fast behaviour.
	write("#!/usr/bin/env tcsh\n")
	write("\n")

	// Prologue: load contract variables into csh variables.
	for _, name := range sortedUnion(reads) {
		f := filepath.Join(stateDir, stateFileName(name, c[name]))
		write(fmt.Sprintf("set %s = \"`macaronic codec read '%s' '%s'`\"\n",
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

	// Epilogue: persist contract variables. csh has no ${name-}
	// default-operator, so a write target the body never assigned is a
	// loud `Undefined variable.` under `tcsh -e` instead of bash's
	// silent empty string. That difference is deliberate and documented
	// in docs/architecture.md §12.
	for _, name := range sortedUnion(writes) {
		f := filepath.Join(stateDir, stateFileName(name, c[name]))
		write(fmt.Sprintf("macaronic codec write '%s' '%s' \"$%s\"\n",
			f, string(c[name]), name))
	}

	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("csh emit: mkdir: %v", err)
	}
	return os.WriteFile(filepath.Join(stageDir, genFile), []byte(b.String()), 0o755)
}

// RunCommand implements engine.Engine. `-e` (exit on any error) is
// required: without it csh continues past a failing command and still
// exits 0, turning a broken stage into a reported success.
func (Engine) RunCommand(stageDir string) []string {
	return []string{"tcsh", "-e", genFile}
}

// ParseDiagnostics implements engine.Engine.
//
// It returns nothing by design. csh emits no file name and no line
// number for any error class — measured for command-not-found, syntax
// errors, undefined variables, division by zero, bad subscripts and
// bad redirection, and confirmed by the tcsh manual, which documents
// no such facility; `-x` echoes only the command text. Without a
// generated line there is nothing to resolve through the source-map,
// so the runner's fallback shows the raw stderr under a stage-level
// failure. Fabricating a line by matching echoed command text against
// the generated file was rejected: development-plan.md M14 requires
// falling back rather than emitting an untrustworthy line number.
func (Engine) ParseDiagnostics(stderr []byte) []ir.Diagnostic {
	return nil
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
