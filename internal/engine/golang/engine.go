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
    reads, writes, shadow := analyzeBody(st.Body, c)
    if shadow != "" {
        return reads, writes, fmt.Errorf(
            "go stage%d: `:=` new binding %q shadows contract variable",
            st.Index, shadow)
    }
    return reads, writes, nil
}

// analyzeBody performs read/write/shadow inference over the user body.
func analyzeBody(lines []string, c ir.Contract) (reads, writes ir.VarSet, shadow string) {
    reads = ir.VarSet{}
    writes = ir.VarSet{}
    for _, line := range lines {
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
            if newBind {
                shadow = name
                return
            }
            if write {
                writes[name] = true
                if readAlso {
                    reads[name] = true
                }
            } else {
                reads[name] = true
            }
        }
    }
    return
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
