// Package zsh tests the macaronic zsh engine.
package zsh

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
	"values": ir.ListOf(ir.Int),
	"words":  ir.ListOf(ir.Str),
	"unused": ir.Int,
}

func testStage(body ...string) *ir.Stage {
	return &ir.Stage{Index: 1, Lang: "zsh", StartLine: 10, EndLine: 10 + len(body),
		Body: body}
}

// TestAnalyzeScalarsAndLists covers scalar inference plus the two zsh
// array shapes: ${name[@]} reads and `name+=(...)` appends. An append
// must count as both a read and a write, otherwise the modification is
// dropped from the epilogue.
func TestAnalyzeScalarsAndLists(t *testing.T) {
	st := testStage(
		"count=$(($count + 1))",
		`echo ${msg}`,
		"for v in \"${values[@]}\"; do echo $v; done",
		`words+=("extra")`,
		"echo $unused",
	)
	reads, writes, err := (Engine{}).Analyze(st, testContract)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"count", "msg", "values", "words", "unused"} {
		if !reads[want] {
			t.Errorf("missing read %q; reads = %v", want, reads)
		}
	}
	for _, want := range []string{"count", "words"} {
		if !writes[want] {
			t.Errorf("missing write %q; writes = %v", want, writes)
		}
	}
	for name := range writes {
		switch name {
		case "count", "words":
		default:
			t.Errorf("unexpected write %q", name)
		}
	}
}

// TestEmitListPlumbing pins the three zsh-specific decisions measured
// before this engine was written: no mapfile, a read-loop prologue, and
// quoted path/type so nomatch cannot silently empty the array.
func TestEmitListPlumbing(t *testing.T) {
	stageDir := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sm := ir.SourceMap{}
	// Write detection is line-start only (same rule as bash), so the
	// scalar assignment sits on its own line rather than inside the loop.
	st := &ir.Stage{Index: 1, Lang: "zsh", StartLine: 5, EndLine: 7, Body: []string{
		"for v in \"${values[@]}\"; do echo $v; done",
		"count=$(($count + 1))",
		`words=("a b")`,
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
		"#!/usr/bin/env zsh\n",
		"set -eu\n",
		// M20: the list prologue reads through a stage-private scratch
		// file so a read-list failure cannot hide behind process
		// substitution.
		`if ! macaronic codec read-list "` + stateDir + `/values.macint[]" "int[]" > "` + stageDir + `/.prologue-values.tmp"; then`,
		`echo 'macaronic: stage 1: cannot read contract variable "values" (int[])' >&2`,
		"values=()\n",
		`while IFS= read -r -d '' __macaronic_item; do values+=("$__macaronic_item"); done < "` + stageDir + `/.prologue-values.tmp"`,
		`rm -f "` + stageDir + `/.prologue-values.tmp"`,
		// The write-only list (words) never gets a prologue, so the
		// epilogue must define it before expanding "${words[@]}".
		"(( ${+words} )) || words=()\n",
		`macaronic codec write-list "` + stateDir + `/words.macstr[]" "str[]" "${words[@]}"`,
		// M20: a declared write the body never assigned aborts with the
		// shared message.
		`(( ${+count} )) || { echo 'macaronic: stage 1: contract variable "count" is unset at epilogue' >&2; exit 1; }`,
		`macaronic codec write "` + stateDir + `/count.macint" "int" "$count"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated run.sh missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "mapfile") {
		t.Errorf("generated run.sh uses bash-only mapfile:\n%s", out)
	}
}

// TestParseDiagnostics covers the zsh "file:N: msg" layout.
func TestParseDiagnostics(t *testing.T) {
	got := (Engine{}).ParseDiagnostics([]byte(
		"run.sh:7: command not found: nosuchcmd\n./run.sh:9: division by zero\n"))
	if len(got) != 2 {
		t.Fatalf("diagnostics = %+v, want two", got)
	}
	if got[0].Msg != "run.sh:7:command not found: nosuchcmd" {
		t.Errorf("msg[0] = %q", got[0].Msg)
	}
	if got[1].Msg != "run.sh:9:division by zero" {
		t.Errorf("msg[1] = %q", got[1].Msg)
	}
}

// TestRunCommandAndRuntime pins the argv contract and the probed runtime.
func TestRunCommandAndRuntime(t *testing.T) {
	eng := Engine{}
	argv := eng.RunCommand("stage1")
	if len(argv) != 2 || argv[0] != "zsh" || argv[1] != genFile {
		t.Errorf("RunCommand = %v, want [zsh %s]", argv, genFile)
	}
	if req := eng.RequiredCommands(); len(req) != 1 || req[0] != argv[0] {
		t.Errorf("RequiredCommands = %v, want [%s]", req, argv[0])
	}
}

// TestE2EScalarsAndLists runs the real inject-execute loop for scalars
// and for int[]/str[] (including an element with a space, which is what
// makes quoting and the array plumbing observable).
func TestE2EScalarsAndLists(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not available")
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
		"count":  ir.Int,
		"msg":    ir.Str,
		"values": ir.ListOf(ir.Int),
		"words":  ir.ListOf(ir.Str),
	}
	eng := Engine{}
	sm := ir.SourceMap{}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Stage1: seed a scalar and both list types, one element holding a
	// space.
	st1 := &ir.Stage{Index: 1, Lang: "zsh", StartLine: 1, EndLine: 4, Body: []string{
		"count=7",
		`msg="from zsh"`,
		"values=(1 2 3)",
		`words=("alpha" "b b")`,
	}}
	if err := eng.Emit(st1, contract, stage1, stateDir, &sm); err != nil {
		t.Fatal(err)
	}
	run(t, stage1)

	// Stage2: read them back through the prologue, mutate, write back.
	// zsh arrays are 1-based, so ${values[1]} is the first element.
	st2 := &ir.Stage{Index: 2, Lang: "zsh", StartLine: 10, EndLine: 14, Body: []string{
		"count=$(($count + ${values[1]}))",
		"values+=(4)",
		`msg="$msg/${#words[@]}"`,
		`words+=("c c")`,
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
		{"msg.macstr", ir.Str, "from zsh/2"},
		{"values.macint[]", ir.ListOf(ir.Int), []int64{1, 2, 3, 4}},
		{"words.macstr[]", ir.ListOf(ir.Str), []string{"alpha", "b b", "c c"}},
	} {
		got := readState(t, filepath.Join(stateDir, tc.file), tc.typ)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s = %#v, want %#v", tc.file, got, tc.want)
		}
	}
}

// TestE2EUnsetListStaysConsistent pins the epilogue guard. A stage that
// unsets a list it is contracted to write used to hit zsh's `parameter
// not set` under set -u (bash expands to zero arguments instead). Both
// dialects must now record an empty list rather than one hard-failing.
func TestE2EUnsetListStaysConsistent(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not available")
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
	contract := ir.Contract{"values": ir.ListOf(ir.Int)}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	st := &ir.Stage{Index: 1, Lang: "zsh", StartLine: 1, EndLine: 2, Body: []string{
		"values=(1 2 3)",
		"unset values",
	}}
	if err := (Engine{}).Emit(st, contract, stage1, stateDir, nil); err != nil {
		t.Fatal(err)
	}
	run(t, stage1)

	got := readState(t, filepath.Join(stateDir, "values.macint[]"), ir.ListOf(ir.Int))
	if !reflect.DeepEqual(got, []int64{}) {
		t.Errorf("values = %#v, want an empty list", got)
	}
}

// readState reads a state file through the shared codec, so the test
// compares real typed values (including str[] elements with spaces)
// rather than parsed text.
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

// run executes an emitted stage script and fails on non-zero exit.
func run(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("zsh", genFile)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run zsh %s: %v\n%s", genFile, err, out)
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
