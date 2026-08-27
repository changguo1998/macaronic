package python

import (
    "os"
    "os/exec"
    "path/filepath"
    "testing"

    "github.com/changguo1998/macaronic/internal/codec"
    "github.com/changguo1998/macaronic/internal/ir"
)

// buildMacaronic compiles the real CLI into dir (used only for the
// `codec` helper subcommand, standing in for the shell engine's
// writes).
func buildMacaronic(t *testing.T, dir string) string {
    t.Helper()
    out := filepath.Join(dir, "macaronic")
    cmd := exec.Command("go", "build", "-o", out, "./cmd/macaronic")
    cmd.Dir = repoRoot(t)
    if bb, err := cmd.CombinedOutput(); err != nil {
        t.Fatalf("go build: %v\n%s", err, bb)
    }
    return out
}

func repoRoot(t *testing.T) string {
    t.Helper()
    dir, err := os.Getwd()
    if err != nil {
        t.Fatal(err)
    }
    return filepath.Dir(filepath.Dir(filepath.Dir(dir))) // internal/engine/python -> root
}

// runStage emits and runs one python stage; returns its combined output.
func runStage(t *testing.T, eng Engine, stageNo int, body []string, contract ir.Contract, root string, sm *ir.SourceMap) string {
    t.Helper()
    st := &ir.Stage{Index: stageNo, Lang: "python", StartLine: 10 + stageNo*10, EndLine: 10 + stageNo*10 + len(body) - 1, Body: body}
    stageDir := filepath.Join(root, "stage"+string(rune('0'+stageNo))) // "stage1","stage2"
    if err := os.MkdirAll(stageDir, 0o755); err != nil {
        t.Fatal(err)
    }
    stateDir := filepath.Join(root, "state")
    if err := os.MkdirAll(stateDir, 0o755); err != nil {
        t.Fatal(err)
    }
    if err := eng.Emit(st, contract, stageDir, stateDir, sm); err != nil {
        t.Fatalf("emit stage%s: %v", string(rune('0'+stageNo)), err)
    }
    out, err := runPython(t, stageDir)
    if err != nil {
        t.Fatalf("run stage%s: %v\n%v", string(rune('0'+stageNo)), out, err)
    }
    return out
}

func TestE2EPyToPy(t *testing.T) {
    requirePython(t)
    root := t.TempDir()
    stateDir := filepath.Join(root, "state")
    contract := ir.Contract{"x": ir.Int, "y": ir.Str}

    // stage1 writes x
    out1 := runStage(t, Engine{}, 1, []string{"x: int = 41"}, contract, root, &ir.SourceMap{})
    if out1 != "" {
        t.Errorf("stage1 output = %q, want empty", out1) // epilogue only
    }
    // verify state/x bytes via internal codec
    f, err := os.Open(filepath.Join(stateDir, "x.macint"))
    if err != nil {
        t.Fatal(err)
    }
    defer f.Close()
    got, err := codec.ReadInt(f)
    if err != nil || got != 41 {
        t.Errorf("state/x = %d, %v; want 41", got, err)
    }
}

func TestE2EShellToPy(t *testing.T) {
    requirePython(t)
    binDir := t.TempDir()
    mac := buildMacaronic(t, binDir)
    root := t.TempDir()
    stateDir := filepath.Join(root, "state")
    if err := os.MkdirAll(stateDir, 0o755); err != nil {
        t.Fatal(err)
    }

    // shell side (via real codec helper) writes count:int and msg:str
    for _, kv := range []struct {
        file, typ, val string
    }{
        {"count.macint", "int", "7"},
        {"msg.macstr", "str", "shell-ok"},
    } {
        bb := exec.Command(mac, "codec", "write",
            filepath.Join(stateDir, kv.file), kv.typ, kv.val)
        if err := bb.Run(); err != nil {
            t.Fatalf("codec write: %v", err)
        }
    }

    // python side reads them
    st := &ir.Stage{Index: 1, Lang: "python", StartLine: 3, Body: []string{
        "count: int",
        "msg: str",
        "print(count, msg)",
    }}
    stageDir := filepath.Join(root, "stage1")
    if err := os.MkdirAll(stageDir, 0o755); err != nil {
        t.Fatal(err)
    }
    var sm ir.SourceMap
    if err := (Engine{}).Emit(st, ir.Contract{"count": ir.Int, "msg": ir.Str}, stageDir, stateDir, &sm); err != nil {
        t.Fatal(err)
    }
    out, err := runPython(t, stageDir)
    if err != nil {
        t.Fatalf("run: %v\n%s", err, out)
    }
    if out != "7 shell-ok" {
        t.Errorf("output = %q, want %q", out, "7 shell-ok")
    }
}

func TestE2EPyWritesForNext(t *testing.T) {
    requirePython(t)
    root := t.TempDir()
    stateDir := filepath.Join(root, "state")
    contract := ir.Contract{"acc": ir.Int}

    // stage1 reads nothing, writes acc (from literal)
    accFrom := -1
    _ = accFrom
    st1 := &ir.Stage{Index: 1, Lang: "python", StartLine: 3, Body: []string{"acc: int = 0", "acc = acc + 1"}}
    _ = st1
    // NOTE: `acc: int = 0` with later assignment; treat `acc: int = ...` as write.
    // But `acc = acc + 1` references acc w/o annotation on that line -> is read; has annotation -> ok.
    // Emit writes acc only if isWrite.
    s1 := filepath.Join(root, "stage1")
    sm1 := ir.SourceMap{}
    if err := os.MkdirAll(s1, 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.MkdirAll(stateDir, 0o755); err != nil {
        t.Fatal(err)
    }
    if err := (Engine{}).Emit(st1, contract, s1, stateDir, &sm1); err != nil {
        t.Fatal(err)
    }
    _, err := runPython(t, s1)
    if err != nil {
        t.Fatalf("stage1 run: %v", err)
    }
    // verify state/acc readable as int
    f, err := os.Open(filepath.Join(stateDir, "acc.macint"))
    if err != nil {
        t.Fatal(err)
    }
    n, err := codec.ReadInt(f)
    f.Close()
    if err != nil {
        t.Fatal(err)
    }
    if n != 1 {
        t.Errorf("state/acc = %d, want 1", n)
    }
}
