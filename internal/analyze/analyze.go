// Package analyze implements the language-independent analysis
// framework: it drives per-stage Engine.Analyze, keeps the cross-block
// symbol table and dependency checks, and assembles a report.
package analyze

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/changguo1998/macaronic/internal/engine"
	"github.com/changguo1998/macaronic/internal/ir"
)

// Severity classifies one check finding. The zero value is SevError,
// so Issue literals that omit the field remain blocking.
type Severity int

const (
	// SevError is a blocking finding: `check` reports it and exits
	// non-zero, `build` aborts.
	SevError Severity = iota
	// SevWarning is an advisory finding: printed by `check` but does
	// not block `check` or `build` (architecture §6: 宁可漏报不误报).
	SevWarning
)

func (s Severity) String() string {
	if s == SevWarning {
		return "warning"
	}
	return "error"
}

// Issue is one check finding, grouped by stage/var/line for the
// report output. Stage 0 denotes a program-level finding that belongs
// to no single stage.
type Issue struct {
	Stage int
	Var   string
	Line  int
	Msg   string
	Severity
}

// Report is the ordered issue list.
type Report struct {
	Issues []Issue
}

// OK reports whether the program has no blocking errors. Warnings are
// advisory and never block (M11).
func (r Report) OK() bool { return !r.HasErrors() }

// HasErrors reports whether any issue has error severity.
func (r Report) HasErrors() bool {
	for _, it := range r.Issues {
		if it.Severity == SevError {
			return true
		}
	}
	return false
}

// Print renders issues to w, each on one line, in deterministic
// (stage, var, line) order. Programs with no findings print nothing.
func (r Report) Print(w io.Writer) {
	iss := append([]Issue(nil), r.Issues...)
	sort.SliceStable(iss, func(i, j int) bool {
		a, b := iss[i], iss[j]
		if a.Stage != b.Stage {
			return a.Stage < b.Stage
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Var < b.Var
	})
	for _, it := range iss {
		fmt.Fprintf(w, "%s: %s: %s\n", it.Severity, issuePrefix(it), it.Msg)
	}
}

// issuePrefix renders "stage N [line L] [var X]" for the report,
// omitting clauses whose values are unset (Stage 0 = program-level).
func issuePrefix(it Issue) string {
	var parts []string
	if it.Stage > 0 {
		parts = append(parts, fmt.Sprintf("stage %d", it.Stage))
	}
	if it.Line > 0 {
		parts = append(parts, fmt.Sprintf("line %d", it.Line))
	}
	if it.Var != "" {
		parts = append(parts, fmt.Sprintf("var %q", it.Var))
	}
	return strings.Join(parts, " ")
}

// Analyzer holds machinery-independent state and engine lookup.
type Analyzer struct {
	// Engines resolves a stage language to its engine. In tests this
	// points at a mock registry; in production use engine.Get.
	Engines func(lang string) (engine.Engine, bool)
}

// Run analyzes the program in stage order.
//
// Dependency rules (architecture §6):
//   - read before any write -> error (with contract type in message)
//   - a later write overwrites safely -> OK (multiple writers fine)
//   - each stage emits shadow/pause diagnostics through Analyze's
//     error return; the framework reports them as issues (never
//     silently swallows)
//
// Program-level warnings (M11):
//   - a contract variable that is neither inferred by any stage nor
//     lexically present in any stage body -> "declared but unused"
//     warning (typo catcher). Lexical presence is a raw-text scan, so
//     a name inside a comment or string also counts as present.
//
// Per-stage warnings (M12):
//   - a contract name present in a stage's source but not inferred as
//     read or write in that stage -> warning that the value may not be
//     injected (suppressed when the engine errored for that stage).
func (a Analyzer) Run(p *ir.Program) Report {
	var iss []Issue
	// sym: name -> type, populated as writes are seen.
	sym := map[string]ir.BasicType{}
	// inferred: union of every stage's inferred read/write sets.
	// observed: union of contract names lexically present in bodies.
	inferred := ir.VarSet{}
	observed := ir.VarSet{}
	for i := range p.Stages {
		st := &p.Stages[i]
		obsStage := observedNames(st, p.Contract)
		mergeVarSets(observed, obsStage)
		eng, ok := a.Engines(st.Lang)
		if !ok {
			iss = append(iss, Issue{
				Stage: st.Index, Line: st.StartLine,
				Msg: fmt.Sprintf("no engine for language %q", st.Lang),
			})
			continue
		}
		read, write, err := eng.Analyze(st, p.Contract)
		mergeVarSets(inferred, read, write)
		// Record this stage's writes first: intra-stage order is the
		// user's business, so a stage that itself produces a var may
		// read it too (cross-block dependency check only).
		for v := range write {
			sym[v] = p.Contract[v]
		}
		for v := range read {
			if _, ok := sym[v]; !ok {
				iss = append(iss, Issue{
					Stage: st.Index, Var: v, Line: st.StartLine,
					Msg: fmt.Sprintf("read of %q before any write (type %s)",
						v, p.Contract[v]),
				})
			}
		}
		if err != nil {
			iss = append(iss, Issue{
				Stage: st.Index, Line: st.StartLine, Msg: err.Error(),
			})
		}
		// M12: names present in this stage's source but not inferred here
		// as read or write may silently miss prologue injection. Suppressed
		// when the engine errored for this stage: that error already
		// explains the stage (no duplicate noise).
		if err == nil {
			for _, v := range sortedContractKeys(p.Contract) {
				if obsStage[v] && !read[v] && !write[v] {
					iss = append(iss, Issue{
						Stage: st.Index, Var: v, Line: st.StartLine,
						Severity: SevWarning,
						Msg:      fmt.Sprintf("contract variable %q appears in this stage's source but was not inferred as read or write; the value may not be injected - review this stage", v),
					})
				}
			}
		}
	}
	// Program-level unused-contract warning (deterministic key order).
	for _, v := range sortedContractKeys(p.Contract) {
		if !inferred[v] && !observed[v] {
			iss = append(iss, Issue{
				Var: v, Severity: SevWarning,
				Msg: fmt.Sprintf("contract variable %q declared but never read, written, or referenced by any stage", v),
			})
		}
	}
	return Report{Issues: iss}
}

// tokenRe matches name as a whole identifier token.
func tokenRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
}

// observedNames returns the contract variable names that appear as
// whole identifier tokens anywhere in the stage body. It is a raw-text
// lexical scan, so it also matches names inside comments and string
// literals — intentionally over-approximating usage, so the unused
// warning never fires for a name that at least appears in the source.
func observedNames(st *ir.Stage, c ir.Contract) ir.VarSet {
	obs := ir.VarSet{}
	for name := range c {
		re := tokenRe(name)
		for _, ln := range st.Body {
			if re.MatchString(ln) {
				obs[name] = true
				break
			}
		}
	}
	return obs
}

// mergeVarSets copies every entry of srcs into dst.
func mergeVarSets(dst ir.VarSet, srcs ...ir.VarSet) {
	for _, src := range srcs {
		for k := range src {
			dst[k] = true
		}
	}
}

func sortedContractKeys(c ir.Contract) []string {
	ks := make([]string, 0, len(c))
	for k := range c {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
