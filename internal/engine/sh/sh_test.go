// Package sh tests the macaronic POSIX sh engine.
package sh

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/changguo1998/macaronic/internal/ir"
)

var testContract = ir.Contract{
	"count":  ir.Int,
	"price":  ir.Float,
	"flag":   ir.Bool,
	"msg":    ir.Str,
	"values": ir.ListOf(ir.Int),
	"unused": ir.Int,
}

func testStage(body ...string) *ir.Stage {
	return &ir.Stage{Index: 1, Lang: "sh", StartLine: 10, EndLine: 10 + len(body),
		Body: body}
}

// TestAnalyzeReadsWrites covers scalar inference.
func TestAnalyzeReadsWrites(t *testing.T) {
	st := testStage(
		"count=$(expr $count + 1)",
		"echo ${msg}",
		"price=$(( $price * 2 ))",
		"flag=true",
		"total=3",      // not in contract; neither read nor write
		"echo $unused", // contract var read
		"echo $flagx",  // partial-name guard
	)
	reads, writes, err := (Engine{}).Analyze(st, testContract)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"count", "msg", "price", "unused"} {
		if !reads[want] {
			t.Errorf("missing read %q; reads = %v", want, reads)
		}
	}
	for name := range reads {
		switch name {
		case "count", "msg", "price", "unused":
		default:
			t.Errorf("unexpected read %q", name)
		}
	}
	for _, want := range []string{"count", "price", "flag"} {
		if !writes[want] {
			t.Errorf("missing write %q; writes = %v", want, writes)
		}
	}
	for name := range writes {
		switch name {
		case "count", "price", "flag":
		default:
			t.Errorf("unexpected write %q", name)
		}
	}
}

// TestAnalyzeRejectsListType pins T17.5: a list-typed contract variable
// used in a sh block is a blocking diagnostic carrying the variable and
// a body-relative span, so the framework can report the original .mac
// line and suppress the M12 warning for this stage.
func TestAnalyzeRejectsListType(t *testing.T) {
	st := &ir.Stage{Index: 1, Lang: "sh", StartLine: 4, EndLine: 7, Body: []string{
		"echo start",
		"echo ${values[0]}",
		"values=x",
	}}
	a := (Engine{}).AnalyzeDetailed(st, testContract)
	if len(a.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want exactly one", a.Diagnostics)
	}
	d := a.Diagnostics[0]
	if d.Var != "values" {
		t.Errorf("var = %q, want values", d.Var)
	}
	if !strings.Contains(d.Msg, "int[]") {
		t.Errorf("msg = %q, want the offending list type", d.Msg)
	}
	if !strings.Contains(d.Msg, "sh:") {
		t.Errorf("msg = %q, want an engine-prefixed message", d.Msg)
	}
	if d.Span == nil {
		t.Fatal("span = nil, want the offending body line")
	}
	// "${values[0]}" is on body line 2, so the span points there.
	if d.Span.StartLine != 2 {
		t.Errorf("span.StartLine = %d, want 2", d.Span.StartLine)
	}
	// The legacy Analyze path surfaces the same finding as an error.
	if _, _, err := (Engine{}).Analyze(st, testContract); err == nil {
		t.Error("Analyze() = nil error, want the list rejection")
	}
}

// TestNoListDiagnosticWhenUntouched proves the rejection is lazy: a
// list in the contract that this block never references is not this
// block's problem.
func TestNoListDiagnosticWhenUntouched(t *testing.T) {
	st := testStage("count=1", "echo $count")
	a := (Engine{}).AnalyzeDetailed(st, testContract)
	if len(a.Diagnostics) != 0 {
		t.Errorf("diagnostics = %+v, want none", a.Diagnostics)
	}
}

// TestEmitScalarPlumbing asserts the generated script shape.
func TestEmitScalarPlumbing(t *testing.T) {
	stageDir := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sm := ir.SourceMap{}
	st := &ir.Stage{Index: 1, Lang: "sh", StartLine: 5, EndLine: 6, Body: []string{
		"count=$(( $count + 1 ))",
		"msg=hello",
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
		"#!/bin/sh\n",
		"set -eu\n",
		`count=$(macaronic codec read "` + stateDir + `/count.macint" int)`,
		// M20: a declared write the body never assigned aborts with the
		// shared message instead of silently writing an empty value.
		`[ -n "${count+x}" ] || { echo 'macaronic: stage 1: contract variable "count" is unset at epilogue' >&2; exit 1; }`,
		`macaronic codec write "` + stateDir + `/count.macint" int "$count"`,
		`[ -n "${msg+x}" ] || { echo 'macaronic: stage 1: contract variable "msg" is unset at epilogue' >&2; exit 1; }`,
		`macaronic codec write "` + stateDir + `/msg.macstr" str "$msg"`,
		"count=$(( $count + 1 ))\n",
		"msg=hello\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated run.sh missing %q:\n%s", want, out)
		}
	}
	// No bash-only array plumbing may leak into a sh script.
	for _, forbidden := range []string{"mapfile", "< <(", "<("} {
		if strings.Contains(out, forbidden) {
			t.Errorf("generated run.sh contains bash-only %q:\n%s", forbidden, out)
		}
	}
	// Injected lines are synthetic; body lines map back to source.
	// StartLine is the marker line, so body line i is .mac line
	// StartLine+1+i: here the marker is line 5, so body lines are 6,7.
	if e, ok := sm["run.sh:3"]; !ok || e.Kind != ir.OrigSynthetic {
		t.Errorf("prologue entry = %+v, want synthetic", sm["run.sh:3"])
	}
	if e, ok := sm["run.sh:5"]; !ok || e.Kind != ir.OrigSource || e.SourceLine != 6 {
		t.Errorf("body entry = %+v, want source line 6", sm["run.sh:5"])
	}
	if e, ok := sm["run.sh:6"]; !ok || e.SourceLine != 7 {
		t.Errorf("body entry = %+v, want source line 7", sm["run.sh:6"])
	}
}

// TestEmitRejectsListDefensively: check blocks this earlier, but Emit
// must never emit code that would silently drop a list value.
func TestEmitRejectsListDefensively(t *testing.T) {
	stageDir := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	st := testStage("values=1")
	err := (Engine{}).Emit(st, testContract, stageDir, stateDir, nil)
	if err == nil {
		t.Fatal("Emit() = nil, want a list-type error")
	}
	if !strings.Contains(err.Error(), "list type") {
		t.Errorf("err = %v, want a list-type explanation", err)
	}
	if _, statErr := os.Stat(filepath.Join(stageDir, genFile)); !os.IsNotExist(statErr) {
		t.Errorf("Emit wrote a script despite the error (stat err = %v)", statErr)
	}
}

// TestParseDiagnostics accepts both sh flavours: dash prints
// "file: N: msg" and a bash-as-sh prints "file: line N: msg".
func TestParseDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stderr string
		want   string
	}{
		{"dash", "run.sh: 7: nosuchcmd: not found\n", "run.sh:7:nosuchcmd: not found"},
		{"bash as sh", "run.sh: line 7: nosuchcmd: command not found\n",
			"run.sh:7:nosuchcmd: command not found"},
		{"relative", "./run.sh: 3: bad: not found\n", "run.sh:3:bad: not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (Engine{}).ParseDiagnostics([]byte(tc.stderr))
			if len(got) != 1 {
				t.Fatalf("diagnostics = %+v, want one", got)
			}
			if got[0].Msg != tc.want {
				t.Errorf("msg = %q, want %q", got[0].Msg, tc.want)
			}
		})
	}
}

// TestRunCommandAndRuntime pins the argv contract and that the probed
// runtime is the interpreter actually invoked.
func TestRunCommandAndRuntime(t *testing.T) {
	eng := Engine{}
	argv := eng.RunCommand("stage1")
	if len(argv) != 2 || argv[0] != "sh" || argv[1] != genFile {
		t.Errorf("RunCommand = %v, want [sh %s]", argv, genFile)
	}
	req := eng.RequiredCommands()
	if len(req) != 1 || req[0] != argv[0] {
		t.Errorf("RequiredCommands = %v, want [%s]", req, argv[0])
	}
}

// TestE2EShToSh runs the real inject-execute loop for the four scalar
// types through two consecutive sh stages.
func TestE2EShToSh(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
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
	seed := func(name, typ, val string) {
		t.Helper()
		f := filepath.Join(stateDir, name+".mac"+typ)
		cmd := exec.Command(bin, "codec", "write", f, typ, val)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("seed %s: %v %s", name, err, out)
		}
	}
	seed("count", "int", "7")
	seed("price", "float", "2.5")
	seed("flag", "bool", "true")
	seed("msg", "str", "hello")

	contract := ir.Contract{
		"count": ir.Int, "price": ir.Float, "flag": ir.Bool, "msg": ir.Str,
		"total": ir.Int, "out": ir.Str,
	}
	eng := Engine{}
	sm := ir.SourceMap{}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Stage1 mutates each scalar (float is passed through unchanged).
	body1 := []string{
		"count=$(( $count + 1 ))",
		"price=$price",
		"flag=false",
		`msg="$msg world"`,
	}
	st1 := &ir.Stage{Index: 1, Lang: "sh", StartLine: 1, EndLine: 4, Body: body1}
	if err := eng.Emit(st1, contract, stage1, stateDir, &sm); err != nil {
		t.Fatal(err)
	}
	run(t, stage1, []string{"sh", genFile})

	// Stage2 reads stage1's values and derives two new ones.
	body2 := []string{
		"total=$(( count * 2 ))",
		`out="$msg/$count"`,
	}
	st2 := &ir.Stage{Index: 2, Lang: "sh", StartLine: 10, EndLine: 11, Body: body2}
	if err := eng.Emit(st2, contract, stage2, stateDir, &sm); err != nil {
		t.Fatal(err)
	}
	run(t, stage2, []string{"sh", genFile})

	for _, tc := range []struct{ name, typ, want string }{
		{"count", "int", "8"},
		{"price", "float", "2.5"},
		{"flag", "bool", "false"},
		{"msg", "str", "hello world"},
		{"total", "int", "16"},
		{"out", "str", "hello world/8"},
	} {
		f := filepath.Join(stateDir, tc.name+".mac"+tc.typ)
		out, err := exec.Command(bin, "codec", "read", f, tc.typ).Output()
		if err != nil {
			t.Fatalf("read %s: %v", tc.name, err)
		}
		if got := strings.TrimSpace(string(out)); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// run executes an emitted stage script and fails the test on non-zero
// exit, surfacing combined output.
func run(t *testing.T, dir string, argv []string) {
	t.Helper()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run %v: %v\n%s", argv, err, out)
	}
}

// buildMacaronic compiles the real CLI into a temp dir so generated
// run.sh can reach `macaronic codec ...` through PATH.
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
