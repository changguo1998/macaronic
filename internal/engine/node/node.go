// Package node implements the macaronic Node.js engine: it infers
// contract-variable reads/writes in a JavaScript block and injects
// read/write code that talks to the shared state ABI directly through
// Buffer (no `macaronic codec` subprocess, which a shell block needs
// because shells cannot address NUL bytes).
//
// The generated file is run.cjs, never run.js: probe-verified that a
// .js file inside a directory whose package.json declares
// "type": "module" is parsed as ESM, where require() is unavailable.
//
// Contract types follow the shell dialects: JavaScript is untyped, so
// the contract table is authoritative and no annotation is required.
// `int` is presented as a JS number and moved through BigInt on the
// wire; values beyond 2^53 lose precision (docs/architecture.md §12).
package node

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
// RunCommand invokes and that source-map entries and Node diagnostics
// reference.
const genFile = "run.cjs"

// Injected failure messages. The read one is the shared M20 text every
// dialect emits (docs/architecture.md §12); the write one is node-only,
// because the other dialects surface write failures from a separate
// codec process.
const (
	readMsg  = "macaronic: stage %d: cannot read contract variable %q (%s)"
	writeMsg = "macaronic: stage %d: cannot write contract variable %q (%s)"
	unsetMsg = "macaronic: stage %d: contract variable %q is unset at epilogue"
)

// Engine is the Node.js backend.
type Engine struct{}

// Name implements engine.Engine.
func (Engine) Name() string { return "node" }

// RequiredCommands implements engine.RuntimeChecker: run.cjs is invoked
// through node (see RunCommand), so node must be on PATH. This also
// catches the common case of a version manager (fnm / nvm / asdf)
// whose shims are missing from a non-interactive shell's PATH.
func (Engine) RequiredCommands() []string { return []string{"node"} }

// declRe matches a JS declaration at line start: `let x`, `const x`,
// `var x`, with or without an initializer.
func declRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*(?:let|const|var)\s+` + regexp.QuoteMeta(name) + `\b`)
}

// assignRe matches a plain assignment `x = ...` (not `==`, not `+=`).
// The declaration keywords are optional: `let x = 1` also writes.
func assignRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*(?:(?:let|const|var)\s+)?` +
		regexp.QuoteMeta(name) + `\s*=[^=]`)
}

// augRe matches a compound assignment `x += ...` and friends.
func augRe(name string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(name) +
		`\s*(?:\+=|-=|\*=|/=|%=|\*\*=|&=|\|=|\^=|<<=|>>=|>>>=)`)
}

// incRe matches `x++`, `x--`, `++x`, `--x`.
func incRe(name string) *regexp.Regexp {
	q := regexp.QuoteMeta(name)
	return regexp.MustCompile(`(?:\b` + q + `\s*(?:\+\+|--)|(?:\+\+|--)\s*` + q + `\b)`)
}

// subscriptAssignRe matches an in-place element assignment `x[i] = ...`
// or `x[i] += ...`: the old array is consumed as well as produced.
func subscriptAssignRe(name string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(name) +
		`\s*\[[^\]]*\]\s*(?:=[^=]|\+=|-=|\*=|/=|%=)`)
}

// mutatorRe matches the Array methods that modify the receiver in
// place. Without treating these as writes, a block whose only
// modification is `values.push(1)` would never persist its array.
func mutatorRe(name string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(name) +
		`\s*\.\s*(?:push|pop|shift|unshift|splice|sort|reverse|fill|copyWithin)\s*\(`)
}

// varRefRe matches a contract var as a whole identifier token.
func varRefRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
}

// Analyze infers read/write sets for one block.
//
// Model (mirrors the Go engine's statement rules, minus the type
// annotation Python requires):
//   - `let|const|var x` for a contract name -> shadowing error: the
//     block would create a local binding that hides the injected one.
//   - `x = ...` -> write.
//   - `x += ...`, `x++`, `x[i] = ...`, `x.push(...)` -> read + write.
//   - any other occurrence -> read.
func (Engine) Analyze(st *ir.Stage, c ir.Contract) (ir.VarSet, ir.VarSet, error) {
	a := (Engine{}).AnalyzeDetailed(st, c)
	return a.Reads, a.Writes, a.Error(st)
}

// AnalyzeDetailed scans the block and records source spans for inferred
// reads/writes and static diagnostics.
func (Engine) AnalyzeDetailed(st *ir.Stage, c ir.Contract) engine.Analysis {
	a := engine.Analysis{
		Reads:      ir.VarSet{},
		Writes:     ir.VarSet{},
		ReadSpans:  map[string]*ir.Span{},
		WriteSpans: map[string]*ir.Span{},
	}
	for _, name := range sortedContractNames(c) {
		ref, decl := varRefRe(name), declRe(name)
		assign, aug := assignRe(name), augRe(name)
		inc, sub := incRe(name), subscriptAssignRe(name)
		mut := mutatorRe(name)
		for i, line := range st.Body {
			if !ref.MatchString(line) {
				continue
			}
			col := strings.Index(line, name)
			if decl.MatchString(line) {
				a.Diagnostics = append(a.Diagnostics, ir.Diagnostic{
					Var:  name,
					Span: lineSpan(i, col, len(name)),
					Msg:  fmt.Sprintf("node: contract variable %q declared with let/const/var shadows the injected binding", name),
				})
				return a
			}
			switch {
			case inc.MatchString(line) || aug.MatchString(line) ||
				sub.MatchString(line) || mut.MatchString(line):
				a.Reads[name] = true
				a.Writes[name] = true
				rememberSpan(a.ReadSpans, name, lineSpan(i, col, len(name)))
				rememberSpan(a.WriteSpans, name, lineSpan(i, col, len(name)))
			case assign.MatchString(line):
				a.Writes[name] = true
				rememberSpan(a.WriteSpans, name, lineSpan(i, col, len(name)))
				// `x = x + 1` (any right-hand mention of x) consumes the
				// value an earlier stage produced, so it is a read too.
				if eq := strings.Index(line, "="); eq >= 0 && ref.MatchString(line[eq+1:]) {
					a.Reads[name] = true
					rememberSpan(a.ReadSpans, name, lineSpan(i, col, len(name)))
				}
			default:
				a.Reads[name] = true
				rememberSpan(a.ReadSpans, name, lineSpan(i, col, len(name)))
			}
		}
	}
	return a
}

// Emit writes run.cjs: header, inline codec, prologue reads, the
// verbatim user body (source-mapped) and epilogue writes.
func (Engine) Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string,
	sm *ir.SourceMap) error {

	if sm != nil && *sm == nil {
		*sm = ir.SourceMap{}
	}
	reads, writes, err := (Engine{}).Analyze(st, c)
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
			(*sm)[key(genFile, line)] = ir.SourceMapEntry{
				SourceLine: srcLine, Kind: kind}
		}
	}

	emit("// generated by macaronic - do not edit", 0, ir.OrigSynthetic)
	emit("'use strict';", 0, ir.OrigSynthetic)
	for _, l := range codecLines {
		emit(l, 0, ir.OrigSynthetic)
	}

	// Prologue: write-only variables still need a declaration, because
	// the generated file runs in strict mode.
	for _, name := range sortedKeys(reads) {
		path := filepath.Join(stateDir, stateFileName(name, c[name]))
		emit(fmt.Sprintf("let %s;", name), 0, ir.OrigSynthetic)
		emit(fmt.Sprintf("try { %s = _macRead(%q, %q); } catch (e) { _macFail(%q + e.message); }",
			name, path, string(c[name]),
			fmt.Sprintf(readMsg+": ", st.Index, name, c[name])), 0, ir.OrigSynthetic)
	}
	for _, name := range sortedKeys(writes) {
		if reads[name] {
			continue
		}
		emit(fmt.Sprintf("let %s;", name), 0, ir.OrigSynthetic)
	}

	// User code (OrigSource per body line).
	for i, l := range st.Body {
		emit(l, st.StartLine+1+i, ir.OrigSource)
	}

	// Epilogue: lists stay lenient (an unset array is an empty list, as
	// in bash/zsh); scalars must have been assigned (shared M20 text).
	for _, name := range sortedKeys(writes) {
		path := filepath.Join(stateDir, stateFileName(name, c[name]))
		if ir.IsList(c[name]) {
			emit(fmt.Sprintf("if (typeof %s === 'undefined') %s = [];", name, name), 0, ir.OrigSynthetic)
		} else {
			emit(fmt.Sprintf("if (typeof %s === 'undefined' || %s === null) _macFail(%q);",
				name, name, fmt.Sprintf(unsetMsg, st.Index, name)), 0, ir.OrigSynthetic)
		}
		emit(fmt.Sprintf("try { _macWrite(%q, %q, %s); } catch (e) { _macFail(%q + e.message); }",
			path, string(c[name]), name,
			fmt.Sprintf(writeMsg+": ", st.Index, name, c[name])), 0, ir.OrigSynthetic)
	}

	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("node emit: mkdir: %v", err)
	}
	return os.WriteFile(filepath.Join(stageDir, genFile), []byte(b.String()), 0o644)
}

// codecLines is the inline state-file codec. Its layout must match
// internal/codec exactly (architecture §10): int64 LE, float64 LE,
// bool 1 byte, str 4-byte length + UTF-8, list 4-byte count + elements.
var codecLines = []string{
	"const _macFs = require('fs');",
	"",
	"function _macFail(msg) {",
	"  console.error(msg);",
	"  process.exit(1);",
	"}",
	"",
	"function _macSize(typ, v) {",
	"  if (typ === 'int' || typ === 'float') return 8;",
	"  if (typ === 'bool') return 1;",
	"  if (typ === 'str') return 4 + Buffer.byteLength(v, 'utf8');",
	"  throw new Error('unknown type ' + typ);",
	"}",
	"",
	"function _macReadScalar(buf, off, typ) {",
	"  if (typ === 'int') return Number(buf.readBigInt64LE(off));",
	"  if (typ === 'float') return buf.readDoubleLE(off);",
	"  if (typ === 'bool') return buf.readUInt8(off) !== 0;",
	"  if (typ === 'str') {",
	"    const n = buf.readUInt32LE(off);",
	"    return buf.toString('utf8', off + 4, off + 4 + n);",
	"  }",
	"  throw new Error('unknown type ' + typ);",
	"}",
	"",
	"function _macRead(path, typ) {",
	"  const buf = _macFs.readFileSync(path);",
	"  if (!typ.endsWith('[]')) return _macReadScalar(buf, 0, typ);",
	"  const elem = typ.slice(0, -2);",
	"  const n = buf.readUInt32LE(0);",
	"  if (n > 1048576) throw new Error('list too long');",
	"  const out = [];",
	"  let off = 4;",
	"  for (let i = 0; i < n; i++) {",
	"    const v = _macReadScalar(buf, off, elem);",
	"    if (elem === 'str' && v.includes('\\u0000')) throw new Error('NUL string element');",
	"    out.push(v);",
	"    off += _macSize(elem, v);",
	"  }",
	"  return out;",
	"}",
	"",
	"function _macWriteScalar(typ, v) {",
	"  if (typ === 'int') {",
	"    if (typeof v !== 'number' || !Number.isInteger(v)) {",
	"      throw new Error('value ' + JSON.stringify(v) + ' is not an integer');",
	"    }",
	"    const b = Buffer.alloc(8);",
	"    b.writeBigInt64LE(BigInt(v), 0);",
	"    return b;",
	"  }",
	"  if (typ === 'float') {",
	"    const b = Buffer.alloc(8);",
	"    b.writeDoubleLE(Number(v), 0);",
	"    return b;",
	"  }",
	"  if (typ === 'bool') {",
	"    const b = Buffer.alloc(1);",
	"    b.writeUInt8(v ? 1 : 0, 0);",
	"    return b;",
	"  }",
	"  if (typ === 'str') {",
	"    const s = String(v);",
	"    if (s.includes('\\u0000')) throw new Error('NUL string element');",
	"    const body = Buffer.from(s, 'utf8');",
	"    const head = Buffer.alloc(4);",
	"    head.writeUInt32LE(body.length, 0);",
	"    return Buffer.concat([head, body]);",
	"  }",
	"  throw new Error('unknown type ' + typ);",
	"}",
	"",
	"function _macWrite(path, typ, v) {",
	"  let data;",
	"  if (!typ.endsWith('[]')) {",
	"    data = _macWriteScalar(typ, v);",
	"  } else {",
	"    if (!Array.isArray(v)) throw new Error('expected an array');",
	"    if (v.length > 1048576) throw new Error('list too long');",
	"    const elem = typ.slice(0, -2);",
	"    const head = Buffer.alloc(4);",
	"    head.writeUInt32LE(v.length, 0);",
	"    const parts = [head];",
	"    for (const item of v) parts.push(_macWriteScalar(elem, item));",
	"    data = Buffer.concat(parts);",
	"  }",
	"  _macFs.writeFileSync(path, data);",
	"}",
	"",
}

// RunCommand implements engine.Engine.
func (Engine) RunCommand(stageDir string) []string {
	return []string{"node", filepath.Join(stageDir, genFile)}
}

// genLineRe matches Node's error header and stack frames, both of which
// carry "file:line" (probe-verified for thrown errors and syntax
// errors alike): the header is `<path>:<line>`, a frame is
// `    at fn (<path>:<line>:<col>)`.
var genLineRe = regexp.MustCompile(`^(?:.*?\()?([^\s()]+\.cjs):(\d+)(?::\d+)?\)?$`)

// errLineRe matches the exception line Node prints after the failing
// snippet, e.g. "TypeError: x is not a function".
var errLineRe = regexp.MustCompile(`^([A-Za-z]*Error(?::| \[).*)$`)

// ParseDiagnostics extracts the failing generated line and message from
// Node's stderr. The header line names the file and line; the message
// comes from the first exception line.
func (Engine) ParseDiagnostics(stderr []byte) []ir.Diagnostic {
	gen, line, msg := "", 0, ""
	for _, l := range strings.Split(string(stderr), "\n") {
		if gen == "" {
			if m := genLineRe.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
				gen, line = m[1], atoi(m[2])
			}
			continue
		}
		if msg == "" {
			if m := errLineRe.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
				msg = m[1]
			}
		}
	}
	if gen == "" || line == 0 {
		return nil
	}
	if msg == "" {
		msg = "node: stage failed"
	}
	return []ir.Diagnostic{{
		Msg: fmt.Sprintf("%s:%d: %s", gen, line, msg),
		Span: &ir.Span{StartLine: line, StartCol: 1,
			EndLine: line, EndCol: 1},
	}}
}

// lineSpan converts a body-relative line/column into a span.
func lineSpan(bodyLine, col, width int) *ir.Span {
	if col < 0 {
		col = 0
	}
	return &ir.Span{StartLine: bodyLine + 1, StartCol: col + 1,
		EndLine: bodyLine + 1, EndCol: col + width + 1}
}

// rememberSpan keeps the first span seen for a name.
func rememberSpan(spans map[string]*ir.Span, name string, span *ir.Span) {
	if span != nil {
		if _, exists := spans[name]; !exists {
			spans[name] = span
		}
	}
}

// stateFileName is the shared state-file naming contract (§10).
func stateFileName(name string, t ir.BasicType) string {
	return name + ".mac" + string(t)
}

// key is the source-map key for one generated line.
func key(genFile string, genLine int) string {
	return genFile + ":" + strconv.Itoa(genLine)
}

// sortedContractNames returns the contract names in deterministic order.
func sortedContractNames(c ir.Contract) []string {
	names := make([]string, 0, len(c))
	for name := range c {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// sortedKeys returns the set members in deterministic order.
func sortedKeys(m ir.VarSet) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
