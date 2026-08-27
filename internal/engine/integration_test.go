// Package engine_test holds cross-engine integration tests that run
// real emitted shell/python/go stages back to back, verifying the
// shared state-file naming contract (<name>.mac<type>) and the
// sequential data flow.
package engine_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/changguo1998/macaronic/internal/codec"
	"github.com/changguo1998/macaronic/internal/engine/golang"
	"github.com/changguo1998/macaronic/internal/engine/python"
	"github.com/changguo1998/macaronic/internal/engine/shell"
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
	// macaronic binary on PATH for the shell engine's `codec` helper
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "macaronic")
	rootDir := repoRoot(t)
	bb, err := exec.Command("go", "build", "-o", bin,
		rootDir+"/cmd/macaronic").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, bb)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// stage 1: shell writes count=40 total=2.5 ok msg
	st1 := &ir.Stage{Index: 1, Lang: "shell", StartLine: 1, Body: []string{
		`count=40`,
		`total=2.5`,
		`ok=true`,
		`msg="hello from shell"`,
	}}
	runStage(t, shell.Engine{}, st1, root, contract, "1")

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
	want := "final values: count=41 total=2.5 ok=true msg=hello from shell & python\n"
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
	if err != nil || m.(string) != "hello from shell & python" {
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
		name      string
		typ       ir.BasicType
		shellBody string
		pyType    string
		pyBody    string
		goBody    string
		want      any
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
			st1 := &ir.Stage{Index: 1, Lang: "shell", StartLine: 1, Body: []string{tc.shellBody}}
			runStage(t, shell.Engine{}, st1, root, contract, "1")
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
