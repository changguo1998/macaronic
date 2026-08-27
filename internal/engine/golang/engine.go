// Package golang implements the Go language engine. A stage is
// generated as a self-contained main package: the engine wraps user
// statements in package main + func main, declares contract variables
// with their Go types, injects prologue (read state files) / epilogue
// (write state files) using self-contained codec helpers, then runs
// `go build` to produce a binary executed at run time.
package golang

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/changguo1998/macaronic/internal/engine"
	"github.com/changguo1998/macaronic/internal/ir"
)

var idRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// Engine is the go engine.
type Engine struct{}

// Name implements engine.Engine.
func (Engine) Name() string { return "go" }

// Analyze implements engine.Engine.
//
// Dependency rules:
//   - a contract variable assigned with `=` is a write; with compound
//     ops (+=) or ++/-- it is a read-modify-write (both sets);
//     any other occurrence is a read.
//   - `y := ..` introduces a new local binding; if y is a contract
//     variable it shadows the contract and is reported as an error.
func (e Engine) Analyze(st *ir.Stage, c ir.Contract) (ir.VarSet, ir.VarSet, error) {
	a := e.AnalyzeDetailed(st, c)
	return a.Reads, a.Writes, a.Error(st)
}

// AnalyzeDetailed scans Go identifiers and records body-relative spans.
func (e Engine) AnalyzeDetailed(st *ir.Stage, c ir.Contract) engine.Analysis {
	a := analyzeBodyDetailed(st.Body, c, st.Index)
	return a
}

// analyzeBody performs read/write/shadow inference over the user body.
func analyzeBody(lines []string, c ir.Contract) (reads, writes ir.VarSet, shadow string) {
	a := analyzeBodyDetailed(lines, c, 0)
	if len(a.Diagnostics) > 0 {
		shadow = a.Diagnostics[0].Var
	}
	return a.Reads, a.Writes, shadow
}

func analyzeBodyDetailed(lines []string, c ir.Contract, stage int) engine.Analysis {
	a := engine.Analysis{
		Reads:      ir.VarSet{},
		Writes:     ir.VarSet{},
		ReadSpans:  map[string]*ir.Span{},
		WriteSpans: map[string]*ir.Span{},
	}
	for i, line := range lines {
		if isCommentOnly(line) {
			continue
		}
		ids := idRe.FindAllStringIndex(line, -1)
		for _, id := range ids {
			name := line[id[0]:id[1]]
			if _, ok := c[name]; !ok {
				continue
			}
			write, readAlso, newBind := identOp(line, id[1])
			span := lineSpan(i, id[0], len(name))
			if newBind {
				a.Diagnostics = []ir.Diagnostic{{Var: name, Span: span,
					Msg: fmt.Sprintf("go stage%d: `:=` new binding %q shadows contract variable", stage, name)}}
				return a
			}
			if write {
				a.Writes[name] = true
				rememberSpan(a.WriteSpans, name, span)
				if readAlso {
					a.Reads[name] = true
					rememberSpan(a.ReadSpans, name, span)
				}
			} else {
				a.Reads[name] = true
				rememberSpan(a.ReadSpans, name, span)
			}
		}
	}
	return a
}

func lineSpan(bodyLine, col, width int) *ir.Span {
	return &ir.Span{StartLine: bodyLine + 1, StartCol: col + 1,
		EndLine: bodyLine + 1, EndCol: col + width + 1}
}

func rememberSpan(spans map[string]*ir.Span, name string, span *ir.Span) {
	if _, exists := spans[name]; !exists {
		spans[name] = span
	}
}

// identOp inspects the text following identifier at index end of line
// and reports whether the identifier is written, whether the write
// also reads it (compound ops, ++/-- are read-modify-write), and
// whether it is a `:=` new binding.
func identOp(line string, end int) (write, readAlso, newBind bool) {
	rest := strings.TrimLeft(line[end:], " \t")
	if rest == "" {
		return false, false, false
	}
	switch {
	case strings.HasPrefix(rest, ":="):
		return true, false, true
	case strings.HasPrefix(rest, "++"), strings.HasPrefix(rest, "--"):
		return true, true, false
	case len(rest) >= 2 && (isAssignOpByte(rest[0]) || rest[0] == '<' || rest[0] == '>') && rest[1] == '=':
		return true, true, false
	case rest[0] == '=':
		if len(rest) > 1 && rest[1] == '=' {
			return false, false, false // comparison ==
		}
		return true, false, false
	}
	return false, false, false
}

func isAssignOpByte(b byte) bool {
	switch b {
	case '+', '-', '*', '/', '%', '&', '|', '^':
		return true
	}
	return false
}

func isCommentOnly(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*")
}
