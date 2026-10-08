// Package engine_test holds cross-engine integration tests that run
// real emitted bash/python/go stages back to back, verifying the
// shared state-file naming contract (<name>.mac<type>) and the
// sequential data flow.
package engine_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/changguo1998/macaronic/internal/codec"
	"github.com/changguo1998/macaronic/internal/engine"
	"github.com/changguo1998/macaronic/internal/engine/bash"
	"github.com/changguo1998/macaronic/internal/engine/csh"
	"github.com/changguo1998/macaronic/internal/engine/golang"
	"github.com/changguo1998/macaronic/internal/engine/node"
	"github.com/changguo1998/macaronic/internal/engine/python"
	"github.com/changguo1998/macaronic/internal/engine/sh"
	"github.com/changguo1998/macaronic/internal/engine/zsh"
	"github.com/changguo1998/macaronic/internal/ir"
)

func TestCrossEngineFlow(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contract := ir.Contract{
		"count": ir.Int, "total": ir.Float, "ok": ir.Bool, "msg": ir.Str,
	}
	// macaronic binary on PATH for the bash engine's `codec` helper
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "macaronic")
	rootDir := repoRoot(t)
	bb, err := exec.Command("go", "build", "-o", bin,
		rootDir+"/cmd/macaronic").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, bb)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// stage 1: bash writes count=40 total=2.5 ok msg
	st1 := &ir.Stage{Index: 1, Lang: "bash", StartLine: 1, Body: []string{
		`count=40`,
		`total=2.5`,
		`ok=true`,
		`msg="hello from bash"`,
	}}
	runStage(t, bash.Engine{}, st1, root, contract, "1")

	// stage 2: python reads count and msg (self-referencing
	// annotations), writes them back; total/ok read too.
	st2 := &ir.Stage{Index: 2, Lang: "python", StartLine: 6, Body: []string{
		"count: int = count + 1",
		"msg: str = msg + \" & python\"",
	}}
	runStage(t, python.Engine{}, st2, root, contract, "2")

	// stage 3: go reads everything (no writes) and prints
	st3 := &ir.Stage{Index: 3, Lang: "go", StartLine: 12, Body: []string{
		`fmt.Printf("final values: count=%d total=%g ok=%t msg=%s\n",
            count, total, ok, msg)`,
	}}
	out := runStage(t, golang.Engine{}, st3, root, contract, "3")

	// asserted exact final output (matches examples/pipeline.mac)
	want := "final values: count=41 total=2.5 ok=true msg=hello from bash & python\n"
	if out != want {
		t.Errorf("final output = %q, want %q", out, want)
	}

	// state files follow the shared contract <name>.mac<type>;
	// legacy bare names must not exist.
	stored := map[string]struct{}{
		"count.macint": {}, "total.macfloat": {}, "ok.macbool": {}, "msg.macstr": {},
	}
	for f := range stored {
		fi, err := os.Stat(filepath.Join(stateDir, f))
		if err != nil || fi.Size() == 0 {
			t.Errorf("missing/empty state file %s: %v", f, err)
		}
	}
	for _, bare := range []string{"count", "total", "ok", "msg"} {
		if _, err := os.Stat(filepath.Join(stateDir, bare)); err == nil {
			t.Errorf("legacy bare state file exists: state/%s", bare)
		}
	}
	cnt, err := os.ReadFile(filepath.Join(stateDir, "count.macint"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := codec.Read(bytes.NewReader(cnt), ir.Int)
	if err != nil || v.(int64) != 41 {
		t.Errorf("count state = %v (%v), want 41", v, err)
	}
	msg, _ := os.ReadFile(filepath.Join(stateDir, "msg.macstr"))
	m, err := codec.Read(bytes.NewReader(msg), ir.Str)
	if err != nil || m.(string) != "hello from bash & python" {
		t.Errorf("msg state = %v (%v)", m, err)
	}
}

func TestCrossEngineListFlow(t *testing.T) {
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "macaronic")
	rootDir := repoRoot(t)
	if out, err := exec.Command("go", "build", "-o", bin, rootDir+"/cmd/macaronic").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cases := []struct {
		name     string
		typ      ir.BasicType
		bashBody string
		pyType   string
		pyBody   string
		goBody   string
		want     any
	}{
		{"int", ir.ListOf(ir.Int), `values=(1 2 3)`, "int", "values[0] += 10", "values[1] += 20", []int64{11, 22, 3}},
		{"float", ir.ListOf(ir.Float), `values=(1.5 2.5 3.5)`, "float", "values[0] += 1.5", "values[1] += 2.5", []float64{3, 5, 3.5}},
		{"bool", ir.ListOf(ir.Bool), `values=(true false true)`, "bool", "values[0] = not values[0]", "values[1] = !values[1]", []bool{false, true, true}},
		{"str", ir.ListOf(ir.Str), `values=("a" "b" "c")`, "str", `values[0] += "-py"`, `values[1] += "-go"`, []string{"a-py", "b-go", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			stateDir := filepath.Join(root, "state")
			if err := os.MkdirAll(stateDir, 0o755); err != nil {
				t.Fatal(err)
			}
			contract := ir.Contract{"values": tc.typ}
			st1 := &ir.Stage{Index: 1, Lang: "bash", StartLine: 1, Body: []string{tc.bashBody}}
			runStage(t, bash.Engine{}, st1, root, contract, "1")
			st2 := &ir.Stage{Index: 2, Lang: "python", StartLine: 4, Body: []string{
				"values: list[" + tc.pyType + "]", tc.pyBody,
			}}
			runStage(t, python.Engine{}, st2, root, contract, "2")
			st3 := &ir.Stage{Index: 3, Lang: "go", StartLine: 8, Body: []string{tc.goBody}}
			runStage(t, golang.Engine{}, st3, root, contract, "3")

			data, err := os.ReadFile(filepath.Join(stateDir, "values.mac"+string(tc.typ)))
			if err != nil {
				t.Fatal(err)
			}
			got, err := codec.Read(bytes.NewReader(data), tc.typ)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("values = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

func runStage(t *testing.T, e interface {
	Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string, sm *ir.SourceMap) error
	RunCommand(stageDir string) []string
}, st *ir.Stage, root string, c ir.Contract, n string) string {
	t.Helper()
	stageDir := filepath.Join(root, "stage"+n)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sm := ir.SourceMap{}
	if err := e.Emit(st, c, stageDir, filepath.Join(root, "state"), &sm); err != nil {
		t.Fatalf("stage%s Emit: %v", n, err)
	}
	argv := e.RunCommand(stageDir)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = stageDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("stage%s run: %v\n%s", n, err, out.String())
	}
	return out.String()
}

// TestCrossEngineBashNodePython runs a real pipeline through three
// engines that use three different codec implementations: bash through
// the CLI helper, node through its embedded Buffer codec, python
// through struct. The final state proves the layouts agree, including
// a str[] element with a space.
func TestCrossEngineBashNodePython(t *testing.T) {
	for _, cmd := range []string{"bash", "node", "python3", "go"} {
		if _, err := exec.LookPath(cmd); err != nil {
			t.Skipf("%s not available", cmd)
		}
	}
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "macaronic")
	if out, err := exec.Command("go", "build", "-o", bin,
		repoRoot(t)+"/cmd/macaronic").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contract := ir.Contract{
		"count": ir.Int, "msg": ir.Str, "values": ir.BasicType("int[]"),
	}

	runStage(t, bash.Engine{}, &ir.Stage{Index: 1, Lang: "bash", StartLine: 1, Body: []string{
		"count=7",
		`msg="from bash"`,
		"values=(1 2)",
	}}, root, contract, "1")

	// node reads bash's state, mutates and writes it back.
	runStage(t, node.Engine{}, &ir.Stage{Index: 2, Lang: "node", StartLine: 6, Body: []string{
		`msg = msg + " + node"`,
		"count++",
		"values.push(3)",
	}}, root, contract, "2")

	stored := map[string]any{
		"count.macint":    int64(8),
		"msg.macstr":      "from bash + node",
		"values.macint[]": []int64{1, 2, 3},
	}
	for file, want := range stored {
		raw, err := os.ReadFile(filepath.Join(stateDir, file))
		if err != nil {
			t.Fatalf("state %s: %v", file, err)
		}
		got, err := codec.Read(bytes.NewReader(raw), typeOfStateFile(t, file, contract))
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
		default:
			if got != want {
				t.Fatalf("%s = %#v, want %#v", file, got, want)
			}
		}
	}

	// python prints the final values, proving it reads node's output.
	out := runStage(t, python.Engine{}, &ir.Stage{Index: 3, Lang: "python", StartLine: 12, Body: []string{
		"count: int",
		"msg: str",
		"values: list[int]",
		`print("count=%d msg=%s values=%s" % (count, msg, ",".join(str(v) for v in values)))`,
	}}, root, contract, "3")
	want := "count=8 msg=from bash + node values=1,2,3\n"
	if out != want {
		t.Errorf("python stage output = %q, want %q", out, want)
	}
}

// typeOfStateFile maps a state file name back to its contract type.
func typeOfStateFile(t *testing.T, file string, c ir.Contract) ir.BasicType {
	t.Helper()
	for name, typ := range c {
		if file == name+".mac"+string(typ) {
			return typ
		}
	}
	t.Fatalf("no contract type for state file %s", file)
	return ""
}

// TestRuntimeCommandsMatchRunCommand pins T17.2: for every engine that
// implements RuntimeChecker, RequiredCommands[0] must be the executable
// RunCommand actually invokes, so the M17 preflight cannot drift from
// what run.sh really executes.
//
// Exception (M20): a compiled language runs the artifact it built, not
// the compiler, so its declared command is the toolchain used during
// Emit. TestRuntimeCommandsDeclared pins those requirements instead.
func TestRuntimeCommandsMatchRunCommand(t *testing.T) {
	for _, eng := range []engine.Engine{bash.Engine{}, sh.Engine{}, zsh.Engine{}, csh.Engine{}, golang.Engine{}, python.Engine{}, node.Engine{}} {
		rc, ok := eng.(engine.RuntimeChecker)
		if !ok {
			continue
		}
		argv := eng.RunCommand("stage1")
		if len(argv) == 0 {
			t.Errorf("%s: RunCommand returned no argv", eng.Name())
			continue
		}
		req := rc.RequiredCommands()
		if len(req) == 0 {
			t.Errorf("%s: RuntimeChecker declared no commands", eng.Name())
			continue
		}
		if req[0] == argv[0] {
			continue
		}
		if !strings.Contains(argv[0], string(filepath.Separator)) {
			t.Errorf("%s: RequiredCommands[0] = %q, RunCommand argv[0] = %q",
				eng.Name(), req[0], argv[0])
		}
	}
}

// TestRuntimeCommandsDeclared pins the M20 preflight table: every
// supported block language names the commands check/build/run must find
// before the program can run at all (README states this for all six).
func TestRuntimeCommandsDeclared(t *testing.T) {
	want := map[string][]string{
		"bash":   {"bash"},
		"sh":     {"sh"},
		"zsh":    {"zsh"},
		"csh":    {"tcsh"},
		"python": {"python3"},
		"go":     {"go"},
		"node":   {"node"},
	}
	for _, eng := range []engine.Engine{bash.Engine{}, sh.Engine{}, zsh.Engine{}, csh.Engine{}, golang.Engine{}, python.Engine{}, node.Engine{}} {
		rc, ok := eng.(engine.RuntimeChecker)
		if !ok {
			t.Errorf("%s: does not implement engine.RuntimeChecker", eng.Name())
			continue
		}
		if got := rc.RequiredCommands(); !reflect.DeepEqual(got, want[eng.Name()]) {
			t.Errorf("%s: RequiredCommands() = %v, want %v", eng.Name(), got, want[eng.Name()])
		}
	}
}

// TestUnsetScalarIsLoudEverywhere pins the M20 cross-dialect contract:
// a write the body declares but never assigns at runtime aborts with
// the same message in all four shell dialects (before M20 csh said
// `Undefined variable.` and bash silently wrote an empty str). The
// stub `macaronic` on PATH never gets called: the guard fires first.
func TestUnsetScalarIsLoudEverywhere(t *testing.T) {
	const wantMsg = `macaronic: stage 1: contract variable "count" is unset at epilogue`
	cases := []struct {
		eng  engine.Engine
		body []string
	}{
		{bash.Engine{}, []string{"count=1", "unset count"}},
		{sh.Engine{}, []string{"count=1", "unset count"}},
		{zsh.Engine{}, []string{"count=1", "unset count"}},
		{csh.Engine{}, []string{"set count = 1", "unset count"}},
	}
	for _, tc := range cases {
		name := tc.eng.Name()
		t.Run(name, func(t *testing.T) {
			argv := tc.eng.RunCommand("unused") // just for the interpreter name
			if _, err := exec.LookPath(argv[0]); err != nil {
				t.Skipf("%s not available", argv[0])
			}
			root := t.TempDir()
			stateDir := filepath.Join(root, "state")
			stageDir := filepath.Join(root, "stage1")
			binDir := filepath.Join(root, "bin")
			for _, d := range []string{stateDir, stageDir, binDir} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			stubMacaronic(t, binDir)

			st := &ir.Stage{Index: 1, Lang: name, StartLine: 4, Body: tc.body}
			if err := tc.eng.Emit(st, ir.Contract{"count": ir.Int}, stageDir, stateDir, nil); err != nil {
				t.Fatalf("Emit: %v", err)
			}
			out, err := runEmitted(t, tc.eng, stageDir)
			if err == nil {
				t.Fatalf("stage succeeded despite an unset write target:\n%s", out)
			}
			if !strings.Contains(out, wantMsg) {
				t.Errorf("output = %q, want the shared message %q", out, wantMsg)
			}
		})
	}
}

// TestListReadFailureIsLoud covers the M20 fix for the silent-empty
// array: a read-list failure must abort the stage on every dialect that
// supports lists, instead of yielding an empty array and exit 0.
func TestListReadFailureIsLoud(t *testing.T) {
	const wantMsg = `macaronic: stage 2: cannot read contract variable "values" (int[])`
	for _, eng := range []engine.Engine{bash.Engine{}, zsh.Engine{}} {
		name := eng.Name()
		t.Run(name, func(t *testing.T) {
			if _, err := exec.LookPath(name); err != nil {
				t.Skipf("%s not available", name)
			}
			root := t.TempDir()
			stateDir := filepath.Join(root, "state")
			stageDir := filepath.Join(root, "stage2")
			binDir := filepath.Join(root, "bin")
			for _, d := range []string{stateDir, stageDir, binDir} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			stubMacaronic(t, binDir)

			st := &ir.Stage{Index: 2, Lang: name, StartLine: 4, Body: []string{
				`for v in "${values[@]}"; do echo "$v"; done`,
			}}
			if err := eng.Emit(st, ir.Contract{"values": ir.BasicType("int[]")}, stageDir, stateDir, nil); err != nil {
				t.Fatalf("Emit: %v", err)
			}
			out, err := runEmitted(t, eng, stageDir)
			if err == nil {
				t.Fatalf("stage succeeded despite a failing read-list:\n%s", out)
			}
			if !strings.Contains(out, wantMsg) {
				t.Errorf("output = %q, want the shared message %q", out, wantMsg)
			}
		})
	}
}

// stubMacaronic puts a `macaronic` that always fails on PATH, standing
// in for a missing or corrupt state file. State directories are cleaned
// between stages, so a real codec failure is hard to stage hermetically;
// a stub makes the failure deterministic.
func stubMacaronic(t *testing.T, binDir string) {
	t.Helper()
	const stub = "#!/bin/sh\necho 'codec: no such file' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "macaronic"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// runEmitted executes an emitted stage through its own RunCommand and
// returns its combined stdout+stderr with the execution error. It is the
// error-tolerant sibling of runStage's helper.
func runEmitted(t *testing.T, e engine.Engine, stageDir string) (string, error) {
	t.Helper()
	argv := e.RunCommand(stageDir)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = stageDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// TestCrossDialectBashShFlow runs the four scalar types through
// bash → sh → bash and verifies the final values through the shared
// codec ABI, proving the two dialects interoperate despite different
// interpreters (and that sh carries no bash-only syntax into run.sh).
func TestCrossDialectBashShFlow(t *testing.T) {
	for _, cmd := range []string{"bash", "sh"} {
		if _, err := exec.LookPath(cmd); err != nil {
			t.Skipf("%s not available", cmd)
		}
	}
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "macaronic")
	if out, err := exec.Command("go", "build", "-o", bin,
		repoRoot(t)+"/cmd/macaronic").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := t.TempDir()
	// The codec helper writes into state/; the pipeline creates it in
	// emit.WS.Create, so the test must provide it too.
	if err := os.MkdirAll(filepath.Join(root, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	contract := ir.Contract{
		"count": ir.Int, "price": ir.Float, "flag": ir.Bool, "msg": ir.Str,
	}

	st1 := &ir.Stage{Index: 1, Lang: "bash", StartLine: 5, EndLine: 8, Body: []string{
		"count=40",
		"price=1.25",
		"flag=true",
		`msg="from bash"`,
	}}
	runStage(t, bash.Engine{}, st1, root, contract, "1")

	st2 := &ir.Stage{Index: 2, Lang: "sh", StartLine: 12, EndLine: 15, Body: []string{
		"count=$(( count + 2 ))",
		"price=$price",
		"flag=false",
		`msg="$msg + sh"`,
	}}
	runStage(t, sh.Engine{}, st2, root, contract, "2")

	st3 := &ir.Stage{Index: 3, Lang: "bash", StartLine: 20, EndLine: 21, Body: []string{
		`msg="$msg + bash"`,
	}}
	runStage(t, bash.Engine{}, st3, root, contract, "3")

	stateDir := filepath.Join(root, "state")
	for _, tc := range []struct {
		file string
		typ  ir.BasicType
		want any
	}{
		{"count.macint", ir.Int, int64(42)},
		{"price.macfloat", ir.Float, 1.25},
		{"flag.macbool", ir.Bool, false},
		{"msg.macstr", ir.Str, "from bash + sh + bash"},
	} {
		data, err := os.ReadFile(filepath.Join(stateDir, tc.file))
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		got, err := codec.Read(bytes.NewReader(data), tc.typ)
		if err != nil {
			t.Fatalf("codec read %s: %v", tc.file, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s = %#v, want %#v", tc.file, got, tc.want)
		}
	}
}

// TestCrossDialectBashZshListFlow runs one-dimensional lists through
// bash → zsh and verifies the final values through the shared codec
// ABI. This is the cross-dialect proof that the two array
// implementations interchange even though zsh has no mapfile and
// indexes arrays from 1 (the test body uses an append, which is
// index-agnostic).
func TestCrossDialectBashZshListFlow(t *testing.T) {
	for _, cmd := range []string{"bash", "zsh"} {
		if _, err := exec.LookPath(cmd); err != nil {
			t.Skipf("%s not available", cmd)
		}
	}
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "macaronic")
	if out, err := exec.Command("go", "build", "-o", bin,
		repoRoot(t)+"/cmd/macaronic").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	contract := ir.Contract{
		"values": ir.ListOf(ir.Int),
		"words":  ir.ListOf(ir.Str),
	}

	// bash creates both lists; one str element contains a space, which
	// is what a naive NUL/whitespace bridge would silently split.
	st1 := &ir.Stage{Index: 1, Lang: "bash", StartLine: 5, EndLine: 6, Body: []string{
		"values=(1 2 3)",
		`words=("alpha" "b b")`,
	}}
	runStage(t, bash.Engine{}, st1, root, contract, "1")

	// zsh reads them back and appends.
	st2 := &ir.Stage{Index: 2, Lang: "zsh", StartLine: 12, EndLine: 14, Body: []string{
		"values+=(4)",
		`words+=("c c")`,
	}}
	runStage(t, zsh.Engine{}, st2, root, contract, "2")

	stateDir := filepath.Join(root, "state")
	for _, tc := range []struct {
		file string
		typ  ir.BasicType
		want any
	}{
		{"values.macint[]", ir.ListOf(ir.Int), []int64{1, 2, 3, 4}},
		{"words.macstr[]", ir.ListOf(ir.Str), []string{"alpha", "b b", "c c"}},
	} {
		data, err := os.ReadFile(filepath.Join(stateDir, tc.file))
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		got, err := codec.Read(bytes.NewReader(data), tc.typ)
		if err != nil {
			t.Fatalf("codec read %s: %v", tc.file, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s = %#v, want %#v", tc.file, got, tc.want)
		}
	}
}

// TestCrossDialectBashCshFlow runs scalars through bash → csh → bash.
// csh is the most divergent dialect (backticks instead of $(...),
// `set name = value` instead of `name=value`, `@` for arithmetic), so
// this is the proof that the shared state ABI still interchanges.
func TestCrossDialectBashCshFlow(t *testing.T) {
	for _, cmd := range []string{"bash", "tcsh"} {
		if _, err := exec.LookPath(cmd); err != nil {
			t.Skipf("%s not available", cmd)
		}
	}
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "macaronic")
	if out, err := exec.Command("go", "build", "-o", bin,
		repoRoot(t)+"/cmd/macaronic").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	contract := ir.Contract{
		"count": ir.Int, "price": ir.Float, "flag": ir.Bool, "msg": ir.Str,
	}

	st1 := &ir.Stage{Index: 1, Lang: "bash", StartLine: 5, EndLine: 8, Body: []string{
		"count=40",
		"price=1.25",
		"flag=true",
		`msg="from bash"`,
	}}
	runStage(t, bash.Engine{}, st1, root, contract, "1")

	// csh reads through its own prologue and writes back with `set`/`@`.
	st2 := &ir.Stage{Index: 2, Lang: "csh", StartLine: 12, EndLine: 15, Body: []string{
		"@ count = $count + 2",
		`set msg = "$msg + csh"`,
		"set flag = false",
		"set price = $price",
	}}
	runStage(t, csh.Engine{}, st2, root, contract, "2")

	st3 := &ir.Stage{Index: 3, Lang: "bash", StartLine: 20, EndLine: 21, Body: []string{
		`msg="$msg + bash"`,
	}}
	runStage(t, bash.Engine{}, st3, root, contract, "3")

	stateDir := filepath.Join(root, "state")
	for _, tc := range []struct {
		file string
		typ  ir.BasicType
		want any
	}{
		{"count.macint", ir.Int, int64(42)},
		{"price.macfloat", ir.Float, 1.25},
		{"flag.macbool", ir.Bool, false},
		{"msg.macstr", ir.Str, "from bash + csh + bash"},
	} {
		data, err := os.ReadFile(filepath.Join(stateDir, tc.file))
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		got, err := codec.Read(bytes.NewReader(data), tc.typ)
		if err != nil {
			t.Fatalf("codec read %s: %v", tc.file, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s = %#v, want %#v", tc.file, got, tc.want)
		}
	}
}
