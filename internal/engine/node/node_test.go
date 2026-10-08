package node

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/changguo1998/macaronic/internal/codec"
	"github.com/changguo1998/macaronic/internal/ir"
)

// testContract covers every scalar and one list type.
var testContract = ir.Contract{
	"count": ir.Int, "price": ir.Float, "ok": ir.Bool, "msg": ir.Str,
	"values": ir.BasicType("int[]"),
}

// TestAnalyzeStatementModel pins the inference rules: assignment is a
// write, a bare reference is a read, and in-place mutation (compound
// assignment, ++, element assignment, Array mutators) is both.
func TestAnalyzeStatementModel(t *testing.T) {
	cases := []struct {
		name  string
		body  []string
		reads []string
		writs []string
	}{
		{
			name:  "plain assignment is a write",
			body:  []string{"count = 1"},
			writs: []string{"count"},
		},
		{
			name:  "declaration with initializer is a shadow error, not a write",
			body:  []string{"let count = 1"},
			writs: nil,
		},
		{
			name:  "reference is a read",
			body:  []string{"console.log(count)"},
			reads: []string{"count"},
		},
		{
			name:  "compound assignment reads and writes",
			body:  []string{"count += 1"},
			reads: []string{"count"}, writs: []string{"count"},
		},
		{
			name:  "increment reads and writes",
			body:  []string{"count++"},
			reads: []string{"count"}, writs: []string{"count"},
		},
		{
			name:  "array element assignment reads and writes",
			body:  []string{"values[0] = 9"},
			reads: []string{"values"}, writs: []string{"values"},
		},
		{
			name:  "array mutator reads and writes",
			body:  []string{"values.push(4)"},
			reads: []string{"values"}, writs: []string{"values"},
		},
		{
			name:  "string and float and bool writes",
			body:  []string{`msg = "hi"`, "price = 1.5", "ok = true"},
			writs: []string{"msg", "price", "ok"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := &ir.Stage{Index: 1, Lang: "node", StartLine: 1, Body: tc.body}
			a := (Engine{}).AnalyzeDetailed(st, testContract)
			if len(a.Diagnostics) != 0 {
				if tc.name != "declaration with initializer is a shadow error, not a write" {
					t.Fatalf("unexpected diagnostics: %+v", a.Diagnostics)
				}
				return
			}
			for _, v := range tc.reads {
				if !a.Reads[v] {
					t.Errorf("reads = %v, want %q", a.Reads, v)
				}
			}
			for _, v := range tc.writs {
				if !a.Writes[v] {
					t.Errorf("writes = %v, want %q", a.Writes, v)
				}
			}
		})
	}
}

// TestAnalyzeShadowIsAnError checks the let/const/var guard: a local
// binding for a contract name would hide the injected variable.
func TestAnalyzeShadowIsAnError(t *testing.T) {
	for _, decl := range []string{"let count = 1", "const count = 1", "var count"} {
		st := &ir.Stage{Index: 1, Lang: "node", StartLine: 4, Body: []string{"x = 1", decl}}
		a := (Engine{}).AnalyzeDetailed(st, testContract)
		if len(a.Diagnostics) != 1 {
			t.Fatalf("%q: diagnostics = %+v, want exactly one", decl, a.Diagnostics)
		}
		d := a.Diagnostics[0]
		if d.Var != "count" || !strings.Contains(d.Msg, "shadows the injected binding") {
			t.Errorf("%q: diagnostic = %+v", decl, d)
		}
		if d.Span == nil || d.Span.StartLine != 2 {
			t.Errorf("%q: span = %+v, want body line 2", decl, d.Span)
		}
	}
}

// TestEmitScalarPlumbing pins the generated shape: CommonJS file name,
// strict mode, inline codec and the shared failure messages.
func TestEmitScalarPlumbing(t *testing.T) {
	stageDir, stateDir := t.TempDir(), t.TempDir()
	st := &ir.Stage{Index: 3, Lang: "node", StartLine: 5, Body: []string{
		"count = count + 1",
		`msg = "hi"`,
	}}
	if err := (Engine{}).Emit(st, testContract, stageDir, stateDir, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(stageDir, genFile))
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	for _, want := range []string{
		"'use strict';",
		"const _macFs = require('fs');",
		"writeBigInt64LE",
		"let count;",
		`try { count = _macRead("` + stateDir + `/count.macint", "int"); } catch (e) {`,
		"cannot read contract variable",
		`is unset at epilogue`,
		"cannot write contract variable",
		"count = count + 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated %s missing %q:\n%s", genFile, want, out)
		}
	}
	if strings.Contains(out, "require('child_process')") {
		t.Errorf("node engine must not shell out to macaronic codec:\n%s", out)
	}
}

// TestEmitListPlumbing checks the list path: empty-list leniency,
// element type plumbing and the NUL rule shared with the CLI codec.
func TestEmitListPlumbing(t *testing.T) {
	stageDir, stateDir := t.TempDir(), t.TempDir()
	st := &ir.Stage{Index: 1, Lang: "node", StartLine: 5, Body: []string{"values.push(4)"}}
	if err := (Engine{}).Emit(st, testContract, stageDir, stateDir, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(stageDir, genFile))
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	for _, want := range []string{
		`values = _macRead("` + stateDir + `/values.macint[]", "int[]")`,
		"if (typeof values === 'undefined') values = [];",
		`_macWrite("` + stateDir + `/values.macint[]", "int[]", values)`,
		"NUL string element",
		"list too long",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated %s missing %q:\n%s", genFile, want, out)
		}
	}
}

// TestParseDiagnostics covers the two stderr shapes probed on Node
// v24: a thrown error and a syntax error.
func TestParseDiagnostics(t *testing.T) {
	thrown := []byte("/tmp/stage1/run.cjs:2\n" +
		"  throw new Error('boom');\n" +
		"  ^\n\n" +
		"Error: boom\n" +
		"    at boom (/tmp/stage1/run.cjs:2:9)\n" +
		"    at Object.<anonymous> (/tmp/stage1/run.cjs:4:1)\n\n" +
		"Node.js v24.16.0\n")
	syntax := []byte("/tmp/stage1/run.cjs:1\n" +
		"const a = ;\n" +
		"          ^\n\n" +
		"SyntaxError: Unexpected token ';'\n")
	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"thrown error", thrown, "/tmp/stage1/run.cjs:2: Error: boom"},
		{"syntax error", syntax, "/tmp/stage1/run.cjs:1: SyntaxError: Unexpected token ';'"},
		{"unrelated stderr", []byte("nothing to see here\n"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (Engine{}).ParseDiagnostics(tc.in)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("diagnostics = %+v, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0].Msg != tc.want {
				t.Fatalf("diagnostics = %+v, want %q", got, tc.want)
			}
		})
	}
}

// TestRunCommandAndPreflight pins the argv and the runtime probe, which
// must agree (the analyzer reports `node` as missing).
func TestRunCommandAndPreflight(t *testing.T) {
	argv := (Engine{}).RunCommand("/tmp/stage2")
	want := []string{"node", "/tmp/stage2/run.cjs"}
	if len(argv) != 2 || argv[0] != want[0] || argv[1] != want[1] {
		t.Fatalf("RunCommand = %v, want %v", argv, want)
	}
	if req := (Engine{}).RequiredCommands(); len(req) != 1 || req[0] != "node" {
		t.Fatalf("RequiredCommands = %v, want [node]", req)
	}
}

// TestE2ENodeWritesAndReads runs the inject-execute loop for real: one
// node stage writes every supported type, the next reads them back and
// prints. State files are verified from the Go side through the shared
// codec, proving the embedded JS codec matches the CLI layout.
func TestE2ENodeWritesAndReads(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	st1Dir, st2Dir := filepath.Join(root, "stage1"), filepath.Join(root, "stage2")
	for _, d := range []string{stateDir, st1Dir, st2Dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	contract := ir.Contract{
		"count": ir.Int, "price": ir.Float, "ok": ir.Bool, "msg": ir.Str,
		"values": ir.BasicType("int[]"), "words": ir.BasicType("str[]"),
	}

	// Stage 1 writes everything, including arrays built by mutation so
	// the read+write inference is exercised.
	st1 := &ir.Stage{Index: 1, Lang: "node", StartLine: 4, Body: []string{
		"count = 41",
		"price = 2.5",
		"ok = true",
		`msg = "hello from node"`,
		"values = [3, 4]",
		`words = ["a b", "c"]`,
	}}
	runStage(t, st1Dir, st1, contract, stateDir)

	// The Go codec must read what JS wrote.
	assertState(t, stateDir, "count.macint", ir.Int, int64(41))
	assertState(t, stateDir, "msg.macstr", ir.Str, "hello from node")
	assertState(t, stateDir, "values.macint[]", ir.BasicType("int[]"), []int64{3, 4})
	assertState(t, stateDir, "words.macstr[]", ir.BasicType("str[]"), []string{"a b", "c"})

	// Stage 2 reads them back, mutates two of them in place (read+write
	// inference) and prints.
	st2 := &ir.Stage{Index: 2, Lang: "node", StartLine: 12, Body: []string{
		`console.log("count=" + count + " price=" + price + " ok=" + ok + " msg=" + msg` +
			` + " values=" + values.join(",") + " words=" + words.join("|"))`,
		"count++",
		"values.push(5)",
	}}
	out := runStage(t, st2Dir, st2, contract, stateDir)
	want := "count=41 price=2.5 ok=true msg=hello from node values=3,4 words=a b|c\n"
	if out != want {
		t.Errorf("stage2 output = %q, want %q", out, want)
	}
	assertState(t, stateDir, "count.macint", ir.Int, int64(42))
	assertState(t, stateDir, "values.macint[]", ir.BasicType("int[]"), []int64{3, 4, 5})
}

// TestE2EGuardsAreLoud covers the two M20-style failures: an unset
// scalar at the epilogue and an unreadable state file at the prologue.
func TestE2EGuardsAreLoud(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	cases := []struct {
		name string
		st   *ir.Stage
		want string
	}{
		{
			name: "unset scalar at epilogue",
			st: &ir.Stage{Index: 1, Lang: "node", StartLine: 4, Body: []string{
				"count = 1", "count = undefined",
			}},
			want: `macaronic: stage 1: contract variable "count" is unset at epilogue`,
		},
		{
			name: "missing list state file",
			st: &ir.Stage{Index: 2, Lang: "node", StartLine: 4, Body: []string{
				"console.log(values.length)",
			}},
			want: `macaronic: stage 2: cannot read contract variable "values" (int[])`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			stateDir := filepath.Join(root, "state")
			stageDir := filepath.Join(root, "stage")
			for _, d := range []string{stateDir, stageDir} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := (Engine{}).Emit(tc.st, testContract, stageDir, stateDir, nil); err != nil {
				t.Fatal(err)
			}
			exe := (Engine{}).RunCommand(stageDir)
			cmd := exec.Command(exe[0], exe[1:]...)
			cmd.Dir = stageDir
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			err := cmd.Run()
			if err == nil {
				t.Fatalf("stage succeeded, want a loud failure:\n%s", out.String())
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("output = %q, want %q", out.String(), tc.want)
			}
		})
	}
}

// TestIntMustBeAnInteger pins the int coercion rule: JS numbers are the
// user-facing type, but a non-integer must not be silently truncated.
func TestIntMustBeAnInteger(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	root := t.TempDir()
	stateDir, stageDir := filepath.Join(root, "state"), filepath.Join(root, "stage")
	for _, d := range []string{stateDir, stageDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	st := &ir.Stage{Index: 1, Lang: "node", StartLine: 4, Body: []string{"count = 1.5"}}
	if err := (Engine{}).Emit(st, testContract, stageDir, stateDir, nil); err != nil {
		t.Fatal(err)
	}
	exe := (Engine{}).RunCommand(stageDir)
	cmd := exec.Command(exe[0], exe[1:]...)
	cmd.Dir = stageDir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err == nil {
		t.Fatalf("non-integer int write succeeded:\n%s", out.String())
	}
	want := `macaronic: stage 1: cannot write contract variable "count" (int)`
	if !strings.Contains(out.String(), want) || !strings.Contains(out.String(), "not an integer") {
		t.Errorf("output = %q, want %q plus the cause", out.String(), want)
	}
}

// TestEmitRecordsSourceMap checks that user body lines map back to the
// original .mac lines. Injected lines carry no entry, matching the
// python engine: a synthetic line cannot be back-mapped, so the raw
// stderr is shown instead of a guessed line number.
func TestEmitRecordsSourceMap(t *testing.T) {
	sm := ir.SourceMap{}
	st := &ir.Stage{Index: 1, Lang: "node", StartLine: 10, Body: []string{"count = 1", "count++"}}
	if err := (Engine{}).Emit(st, testContract, t.TempDir(), t.TempDir(), &sm); err != nil {
		t.Fatal(err)
	}
	seen := map[int]ir.OriginKind{}
	for _, e := range sm {
		seen[e.SourceLine] = e.Kind
	}
	for _, line := range []int{11, 12} {
		kind, ok := seen[line]
		if !ok || kind != ir.OrigSource {
			t.Errorf("source line %d: kind = %v (present %v), want OrigSource", line, kind, ok)
		}
	}
	if _, ok := seen[0]; ok {
		t.Errorf("source map records a synthetic entry (SourceLine 0): %v", sm)
	}
	if len(seen) != 2 {
		t.Errorf("source map has %d distinct source lines, want 2 (user body only)", len(seen))
	}
}

// runStage emits one stage and runs it through its own RunCommand,
// returning combined output. It fails the test if the stage does not
// exit cleanly.
func runStage(t *testing.T, stageDir string, st *ir.Stage, c ir.Contract, stateDir string) string {
	t.Helper()
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (Engine{}).Emit(st, c, stageDir, stateDir, nil); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	exe := (Engine{}).RunCommand(stageDir)
	cmd := exec.Command(exe[0], exe[1:]...)
	cmd.Dir = stageDir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("stage run: %v\n%s", err, out.String())
	}
	return out.String()
}

// assertState reads one state file with the Go codec and compares it
// with the value JS was expected to have written.
func assertState(t *testing.T, stateDir, file string, typ ir.BasicType, want any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(stateDir, file))
	if err != nil {
		t.Fatalf("state %s: %v", file, err)
	}
	got, err := codec.Read(bytes.NewReader(raw), typ)
	if err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}
	switch w := want.(type) {
	case []int64:
		g, ok := got.([]int64)
		if !ok || len(g) != len(w) {
			t.Fatalf("%s = %#v, want %#v", file, got, want)
		}
		for i := range w {
			if g[i] != w[i] {
				t.Fatalf("%s = %#v, want %#v", file, got, want)
			}
		}
	case []string:
		g, ok := got.([]string)
		if !ok || len(g) != len(w) {
			t.Fatalf("%s = %#v, want %#v", file, got, want)
		}
		for i := range w {
			if g[i] != w[i] {
				t.Fatalf("%s = %#v, want %#v", file, got, want)
			}
		}
	default:
		if got != want {
			t.Fatalf("%s = %#v, want %#v", file, got, want)
		}
	}
}
