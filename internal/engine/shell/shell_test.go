// Package shell tests the macaronic shell engine.
package shell

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
	"unused": ir.Int,
}

func testStage(body ...string) *ir.Stage {
	return &ir.Stage{Index: 1, Lang: "shell", StartLine: 10, EndLine: 10 + len(body),
		Body: body}
}

func TestAnalyzeReadsWrites(t *testing.T) {
	st := testStage(
		"count=$(expr $count + 1)",
		"echo ${msg}",
		"price=$(( $price * 2 ))",
		"flag=true",
		"total=3",      // not in contract; not a read or write
		"echo $unused", // contract var read
		"echo $flagx",  // partial-name guard
	)
	reads, writes, err := (Engine{}).Analyze(st, testContract)
	if err != nil {
		t.Fatal(err)
	}
	for name := range reads {
		switch name {
		case "count", "msg", "price", "unused":
		default:
			t.Errorf("unexpected read %q", name)
		}
	}
	for _, want := range []string{"count", "msg", "price", "unused"} {
		if !reads[want] {
			t.Errorf("missing read %q", want)
		}
	}
	for name := range writes {
		switch name {
		case "count", "price", "flag":
		default:
			t.Errorf("unexpected write %q", name)
		}
	}
	for _, want := range []string{"count", "price", "flag"} {
		if !writes[want] {
			t.Errorf("missing write %q", want)
		}
	}
}

func TestEmitSourceMap(t *testing.T) {
	st := testStage("echo $price", "count=$(( $count + 1 ))", "flag=true")
	st.StartLine = 10
	stageDir := filepath.Join(t.TempDir(), "s1")
	stateDir := filepath.Join(t.TempDir(), "state")
	sm := ir.SourceMap{}
	err := (Engine{}).Emit(st, testContract, stageDir, stateDir, &sm)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(stageDir, genFile))
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	// prologue injects reads; epilogue writes
	if !strings.Contains(out, "macaronic codec read") ||
		!strings.Contains(out, "macaronic codec write") {
		t.Errorf("generated script missing codec calls:\n%s", out)
	}
	// verbatim body preserved
	if !strings.Contains(out, "echo $price") {
		t.Errorf("body missing:\n%s", out)
	}
	// sourcemap: body lines map to source lines (OrigSource),
	// prologue/epilogue lines are synthetic.
	var foundSource, foundSynthetic bool
	for k, e := range sm {
		if !strings.HasPrefix(k, "run.sh:") {
			t.Errorf("bad map key %q", k)
		}
		switch e.Kind {
		case ir.OrigSource:
			foundSource = true
		case ir.OrigSynthetic:
			foundSynthetic = true
			if e.SourceLine != 0 {
				t.Errorf("synthetic entry has SourceLine %d", e.SourceLine)
			}
		}
	}
	if !foundSource || !foundSynthetic {
		t.Errorf("want both source and synthetic entries, got %d entries", len(sm))
	}
}

func TestParseDiagnostics(t *testing.T) {
	stderr := []byte("run.sh: line 7: foo: command not found\n" +
		"some other line\n" +
		"run.sh: line 9: bar: command not found\n")
	ds := (Engine{}).ParseDiagnostics(stderr)
	if len(ds) != 2 {
		t.Fatalf("diags = %d, want 2: %+v", len(ds), ds)
	}
	if ds[0].Msg != "run.sh:7:foo: command not found" {
		t.Errorf("first diag = %q", ds[0].Msg)
	}
	if ds[1].Msg != "run.sh:9:bar: command not found" {
		t.Errorf("second diag = %q", ds[1].Msg)
	}
	if ds[0].Span != nil {
		t.Errorf("Span should be nil, got %+v", ds[0].Span)
	}
}

func TestWrongTypeValueRejectedAtCodecLevel(t *testing.T) {
	b, err := buildMacaronic(t)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "x.macint")
	cmd := exec.Command(b, "codec", "write", f, "int", "notanint")
	if err := cmd.Run(); err == nil {
		t.Errorf("expected codec write to fail for bad int")
	}
}

// repoRoot walks up from the test's working directory (the package
// dir) to find go.mod and returns the repository root.
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").CombinedOutput()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "/dev/null" || gomod == os.DevNull {
		t.Fatal("outside a module")
	}
	return filepath.Dir(gomod)
}

// buildMacaronic compiles the real CLI binary into a temp dir so
// generated run.sh can call `macaronic codec ...` through PATH.
func buildMacaronic(t *testing.T) (string, error) {
	t.Helper()
	root := repoRoot(t)
	b := filepath.Join(t.TempDir(), "macaronic")
	cmd := exec.Command("go", "build", "-o", b, "./cmd/macaronic")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%v: %s", err, out)
	}
	return b, nil
}

// TestE2EShellToShell runs the full inject-then-execute loop for
// str/int/float/bool variables through two consecutive shell stages,
// using a locally built macaronic binary and a .mac.run-like layout.
func TestE2EShellToShell(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
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

	// Seed input states directly with the codec helper.
	seed := func(name string, typ string, val string) {
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

	contract := ir.Contract{"count": ir.Int, "price": ir.Float,
		"flag": ir.Bool, "msg": ir.Str, "result": ir.Int,
		"out": ir.Str}
	sm := ir.SourceMap{}
	eng := Engine{}

	// Stage1: alter each primitive, set result=99. All writes use
	// leading assignment (if/loop bodies are not line-start writes).
	body1 := []string{
		"count=$(( $count + 1 ))",
		"price=$(awk \"BEGIN{print $price*2}\")",
		"flag=false", // input was true; only line-start writes are picked up
		"msg=\"$msg world\"",
		"result=99",
	}
	st1 := &ir.Stage{Index: 1, Lang: "shell", StartLine: 1, EndLine: 5, Body: body1}
	if err := eng.Emit(st1, contract, stage1, stateDir, &sm); err != nil {
		t.Fatal(err)
	}

	// Stage2: read everything and emit a combined string into the
	// contract variable "out" (only contract vars are transferred).
	comb := []string{"out=\"count=$count;price=$price;flag=$flag;msg=$msg\""}
	st2 := &ir.Stage{Index: 2, Lang: "shell", StartLine: 20, EndLine: 20, Body: comb}
	if err := eng.Emit(st2, contract, stage2, stateDir, &sm); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(), "PATH="+filepath.Dir(bin)+":"+os.Getenv("PATH"))
	run := func(dir string) {
		t.Helper()
		cmd := exec.Command("bash", "run.sh")
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("run %s: %v\n%s", dir, err, out)
		}
	}
	run(stage1)
	run(stage2)

	read := func(name, typ string) string {
		t.Helper()
		f := filepath.Join(stateDir, name+".mac"+typ)
		out, err := exec.Command(bin, "codec", "read", f, typ).CombinedOutput()
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return strings.TrimSpace(string(out))
	}
	if got := read("count", "int"); got != "8" {
		t.Errorf("count = %q, want 8", got)
	}
	if got := read("price", "float"); got != "5" {
		t.Errorf("price = %q, want 5", got)
	}
	if got := read("flag", "bool"); got != "false" {
		t.Errorf("flag = %q, want false", got)
	}
	if got := read("msg", "str"); got != "hello world" {
		t.Errorf("msg = %q, want hello world", got)
	}
	if got := read("result", "int"); got != "99" {
		t.Errorf("result = %q, want 99", got)
	}
	if got := read("out", "str"); !strings.Contains(got, "count=8") {
		t.Errorf("out = %q", got)
	}
}

func TestAnalyzeShellReadBuiltin(t *testing.T) {
	for _, body := range []string{"read count", "read -r count"} {
		t.Run(body, func(t *testing.T) {
			reads, writes, err := (Engine{}).Analyze(testStage(body), ir.Contract{"count": ir.Int})
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if reads["count"] || !writes["count"] {
				t.Fatalf("reads=%v writes=%v, want write-only count", reads, writes)
			}
		})
	}
}

func TestAnalyzeShellArithmeticReference(t *testing.T) {
	reads, writes, err := (Engine{}).Analyze(
		testStage("count=$((count + 1))"), ir.Contract{"count": ir.Int})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !reads["count"] || !writes["count"] {
		t.Fatalf("reads=%v writes=%v, want arithmetic read+write", reads, writes)
	}
}
