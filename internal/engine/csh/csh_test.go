// Package csh tests the macaronic C shell engine.
package csh

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/changguo1998/macaronic/internal/codec"
	"github.com/changguo1998/macaronic/internal/ir"
)

var testContract = ir.Contract{
	"count":  ir.Int,
	"msg":    ir.Str,
	"flag":   ir.Bool,
	"values": ir.ListOf(ir.Int),
	"unused": ir.Int,
}

func testStage(body ...string) *ir.Stage {
	return &ir.Stage{Index: 1, Lang: "csh", StartLine: 10, EndLine: 10 + len(body),
		Body: body}
}

// TestAnalyzeReadsWrites covers the csh shapes: `set name = ...` and
// `@ name = ...` writes, plus $name / ${name} / $?name / $#name reads.
func TestAnalyzeReadsWrites(t *testing.T) {
	st := testStage(
		"set count = 5",
		"@ count = $count + 1",
		"echo ${msg}",
		"if ( $?flag ) echo defined",
		"echo $unused",
		"set localOnly = 9", // not in contract
	)
	reads, writes, err := (Engine{}).Analyze(st, testContract)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"count", "msg", "flag", "unused"} {
		if !reads[want] {
			t.Errorf("missing read %q; reads = %v", want, reads)
		}
	}
	for _, want := range []string{"count"} {
		if !writes[want] {
			t.Errorf("missing write %q; writes = %v", want, writes)
		}
	}
	for name := range writes {
		if name != "count" {
			t.Errorf("unexpected write %q", name)
		}
	}
}

// TestAnalyzeRejectsListType: csh cannot carry the NUL-separated list
// stream, so a list-typed contract variable used in a csh block is a
// blocking diagnostic with the offending span (and therefore an exact
// .mac line, which is the only line-accurate csh signal there is).
func TestAnalyzeRejectsListType(t *testing.T) {
	st := &ir.Stage{Index: 1, Lang: "csh", StartLine: 4, EndLine: 7, Body: []string{
		"echo start",
		"echo $values[1]",
	}}
	a := (Engine{}).AnalyzeDetailed(st, testContract)
	if len(a.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want exactly one", a.Diagnostics)
	}
	d := a.Diagnostics[0]
	if d.Var != "values" {
		t.Errorf("var = %q, want values", d.Var)
	}
	if !strings.Contains(d.Msg, "int[]") || !strings.Contains(d.Msg, "csh:") {
		t.Errorf("msg = %q, want a csh-prefixed message naming the type", d.Msg)
	}
	if d.Span == nil || d.Span.StartLine != 2 {
		t.Errorf("span = %+v, want body line 2", d.Span)
	}
	if _, _, err := (Engine{}).Analyze(st, testContract); err == nil {
		t.Error("Analyze() = nil error, want the list rejection")
	}
}

// TestMetaVarFormIsARead covers $#name: the array-length form needs the
// value injected, so it counts as a read and, for a list-typed
// variable, triggers the same rejection as any other reference.
func TestMetaVarFormIsARead(t *testing.T) {
	a := (Engine{}).AnalyzeDetailed(testStage("echo $#values"), testContract)
	if !a.Reads["values"] {
		t.Errorf("$#values not inferred as a read; reads = %v", a.Reads)
	}
	if len(a.Diagnostics) != 1 || a.Diagnostics[0].Var != "values" {
		t.Errorf("diagnostics = %+v, want the values rejection", a.Diagnostics)
	}
}

// TestNoListDiagnosticWhenUntouched proves the rejection is lazy.
func TestNoListDiagnosticWhenUntouched(t *testing.T) {
	a := (Engine{}).AnalyzeDetailed(testStage("set count = 1", "echo $count"), testContract)
	if len(a.Diagnostics) != 0 {
		t.Errorf("diagnostics = %+v, want none", a.Diagnostics)
	}
}

// TestEmitScalarPlumbing asserts the generated script shape. The state
// path and type are single-quoted (csh treats `[]` as a glob and skips
// the whole command on an unmatched one), and the codec call is wrapped
// in double quotes so spaces survive command substitution.
func TestEmitScalarPlumbing(t *testing.T) {
	stageDir := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sm := ir.SourceMap{}
	st := &ir.Stage{Index: 1, Lang: "csh", StartLine: 5, EndLine: 7, Body: []string{
		"echo $count",
		"@ count = $count + 1",
		`set msg = "hi there"`,
	}}
	if err := (Engine{}).Emit(st, testContract, stageDir, stateDir, &sm); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(stageDir, genFile))
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)

	for _, want := range []string{
		"#!/usr/bin/env tcsh\n",
		fmt.Sprintf("set count = \"`macaronic codec read '%s/count.macint' 'int'`\"", stateDir),
		fmt.Sprintf("macaronic codec write '%s/count.macint' 'int' \"$count\"", stateDir),
		fmt.Sprintf("macaronic codec write '%s/msg.macstr' 'str' \"$msg\"", stateDir),
		"@ count = $count + 1\n",
		"set msg = \"hi there\"\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated run.csh missing %q:\n%s", want, out)
		}
	}
	// The state path must never appear unquoted, or the `[]` of a list
	// type would be read as a glob.
	if strings.Contains(out, stateDir+"/count.macint ") {
		t.Errorf("state path looks unquoted:\n%s", out)
	}
	// Line 1 is the shebang, 2 is the blank synthetic line, 3 the
	// scalar prologue, so the first body line lands on generated line 4
	// and maps back to StartLine+1 = 6.
	if e, ok := sm["run.csh:4"]; !ok || e.Kind != ir.OrigSource || e.SourceLine != 6 {
		t.Errorf("body entry = %+v, want source line 6", sm["run.csh:4"])
	}
}

// TestEmitRejectsListDefensively: Emit must never emit code that would
// silently split list elements.
func TestEmitRejectsListDefensively(t *testing.T) {
	stageDir := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	err := (Engine{}).Emit(testStage("set values = (1 2 3)"), testContract,
		stageDir, stateDir, nil)
	if err == nil {
		t.Fatal("Emit() = nil, want a list-type error")
	}
	if !strings.Contains(err.Error(), "list type") {
		t.Errorf("err = %v, want a list-type explanation", err)
	}
}

// TestParseDiagnosticsIsDeliberatelyEmpty documents the csh limitation:
// csh emits no file name and no line number, so there is nothing to
// resolve through the source-map and the runner must fall back to raw
// stderr rather than show a guessed line.
func TestParseDiagnosticsIsDeliberatelyEmpty(t *testing.T) {
	for _, stderr := range []string{
		"nosuchcmd: Command not found.\n",
		"Too many ('s.\n",
		"nope: Undefined variable.\n",
		"Division by 0.\n",
		"arr: Subscript out of range.\n",
	} {
		if got := (Engine{}).ParseDiagnostics([]byte(stderr)); len(got) != 0 {
			t.Errorf("ParseDiagnostics(%q) = %+v, want none", stderr, got)
		}
	}
}

// TestRunCommandAndRuntime pins that the invoked interpreter is tcsh
// with -e, and that the probed runtime matches it.
func TestRunCommandAndRuntime(t *testing.T) {
	eng := Engine{}
	argv := eng.RunCommand("stage1")
	if len(argv) != 3 || argv[0] != "tcsh" || argv[1] != "-e" || argv[2] != genFile {
		t.Errorf("RunCommand = %v, want [tcsh -e %s]", argv, genFile)
	}
	req := eng.RequiredCommands()
	if len(req) != 1 || req[0] != argv[0] {
		t.Errorf("RequiredCommands = %v, want [%s]", req, argv[0])
	}
}

// TestE2EScalars runs the real inject-execute loop for all four scalar
// types through two consecutive csh stages, including a value holding a
// space and a single quote.
func TestE2EScalars(t *testing.T) {
	if _, err := exec.LookPath("tcsh"); err != nil {
		t.Skip("tcsh not available")
	}
	bin, err := buildMacaronic(t)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	stage1 := filepath.Join(root, "stage1")
	stage2 := filepath.Join(root, "stage2")
	for _, d := range []string{stateDir, stage1, stage2} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	contract := ir.Contract{
		"count": ir.Int, "price": ir.Float, "flag": ir.Bool, "msg": ir.Str,
	}
	eng := Engine{}
	sm := ir.SourceMap{}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	st1 := &ir.Stage{Index: 1, Lang: "csh", StartLine: 1, EndLine: 4, Body: []string{
		"set count = 7",
		"set price = 2.5",
		"set flag = true",
		`set msg = "it's spaced out"`,
	}}
	if err := eng.Emit(st1, contract, stage1, stateDir, &sm); err != nil {
		t.Fatal(err)
	}
	run(t, stage1)

	st2 := &ir.Stage{Index: 2, Lang: "csh", StartLine: 10, EndLine: 14, Body: []string{
		"@ count = $count + 1",
		`set msg = "$msg + second"`,
		"set price = $price",
		"set flag = false",
	}}
	if err := eng.Emit(st2, contract, stage2, stateDir, &sm); err != nil {
		t.Fatal(err)
	}
	run(t, stage2)

	for _, tc := range []struct {
		file string
		typ  ir.BasicType
		want any
	}{
		{"count.macint", ir.Int, int64(8)},
		{"price.macfloat", ir.Float, 2.5},
		{"flag.macbool", ir.Bool, false},
		{"msg.macstr", ir.Str, "it's spaced out + second"},
	} {
		got := readState(t, filepath.Join(stateDir, tc.file), tc.typ)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s = %#v, want %#v", tc.file, got, tc.want)
		}
	}
}

// TestE2EFailingCommandFailsStage pins why RunCommand uses `tcsh -e`.
// A failing command must make the stage non-zero; plain `tcsh` silently
// continues and exits 0, which would report a broken stage as success.
func TestE2EFailingCommandFailsStage(t *testing.T) {
	if _, err := exec.LookPath("tcsh"); err != nil {
		t.Skip("tcsh not available")
	}
	bin, err := buildMacaronic(t)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	stage1 := filepath.Join(root, "stage1")
	for _, d := range []string{stateDir, stage1} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	contract := ir.Contract{"count": ir.Int}
	st := &ir.Stage{Index: 1, Lang: "csh", StartLine: 1, EndLine: 2, Body: []string{
		"nosuchcmd_ccc_zzz",
		"set count = 1",
	}}
	if err := (Engine{}).Emit(st, contract, stage1, stateDir, nil); err != nil {
		t.Fatal(err)
	}

	// As emitted by RunCommand: fail-fast, non-zero.
	argv := (Engine{}).RunCommand(stage1)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = stage1
	if err := cmd.Run(); err == nil {
		t.Errorf("tcsh -e exited 0 despite a failing command; argv = %v", argv)
	}

	// Control: without -e csh reports success, which is the hazard the
	// flag exists to close.
	plain := exec.Command("tcsh", genFile)
	plain.Dir = stage1
	if err := plain.Run(); err != nil {
		t.Errorf("plain tcsh unexpectedly failed (%v); the -e rationale changed", err)
	}
}

// readState reads a state file through the shared codec.
func readState(t *testing.T, file string, typ ir.BasicType) any {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	v, err := codec.Read(bytes.NewReader(data), typ)
	if err != nil {
		t.Fatalf("codec read %s: %v", file, err)
	}
	return v
}

// run executes an emitted stage through RunCommand and fails on a
// non-zero exit, surfacing combined output.
func run(t *testing.T, dir string) {
	t.Helper()
	argv := (Engine{}).RunCommand(dir)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run %v: %v\n%s", argv, err, out)
	}
}

// buildMacaronic compiles the real CLI into a temp dir so generated
// run.csh can reach `macaronic codec ...` through PATH.
func buildMacaronic(t *testing.T) (string, error) {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "/dev/null" || gomod == os.DevNull {
		t.Fatal("outside a module")
	}
	b := filepath.Join(t.TempDir(), "macaronic")
	cmd := exec.Command("go", "build", "-o", b, "./cmd/macaronic")
	cmd.Dir = filepath.Dir(gomod)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build: %v: %s", err, out)
	}
	return b, nil
}
