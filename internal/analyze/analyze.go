// Package analyze implements the language-independent analysis
// framework: it drives per-stage Engine.Analyze, keeps the cross-block
// symbol table and dependency checks, and assembles a report.
package analyze

import (
	"fmt"
	"io"
	"sort"

	"github.com/changguo1998/macaronic/internal/engine"
	"github.com/changguo1998/macaronic/internal/ir"
)

// Issue is one check finding, grouped by stage/var/line for the
// report output.
type Issue struct {
	Stage int
	Var   string
	Line  int
	Msg   string
}

// Report is the ordered issue list.
type Report struct {
	Issues []Issue
}

// OK reports whether the program has no issues.
func (r Report) OK() bool { return len(r.Issues) == 0 }

// Print renders issues to w, each on one line, in deterministic
// (stage, var, line) order. OK programs print nothing.
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
		fmt.Fprintf(w, "%s: %s\n", issuePrefix(it), it.Msg)
	}
}

// issuePrefix renders "stage N [line L] [var X]" for the report.
func issuePrefix(it Issue) string {
	p := fmt.Sprintf("stage %d", it.Stage)
	if it.Line > 0 {
		p += fmt.Sprintf(" line %d", it.Line)
	}
	if it.Var != "" {
		p += fmt.Sprintf(" var %q", it.Var)
	}
	return p
}

// Symbol tracks one contract variable through the pipeline.
type Symbol struct {
	Type ir.BasicType
	Name string
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
func (a Analyzer) Run(p *ir.Program) Report {
	var iss []Issue
	// sym: name -> type, populated as writes are seen.
	sym := map[string]ir.BasicType{}
	for i := range p.Stages {
		st := &p.Stages[i]
		eng, ok := a.Engines(st.Lang)
		if !ok {
			iss = append(iss, Issue{
				Stage: st.Index, Line: st.StartLine,
				Msg: fmt.Sprintf("no engine for language %q", st.Lang),
			})
			continue
		}
		read, write, err := eng.Analyze(st, p.Contract)
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
	}
	return Report{Issues: iss}
}
