package golang

import (
    "bytes"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "strings"
    "testing"

    "github.com/changguo1998/macaronic/internal/codec"
    "github.com/changguo1998/macaronic/internal/ir"
)

// runStage executes a built stage binary via RunCommand.
func runStage(t *testing.T, root, stageDir string) (string, error) {
    t.Helper()
    cmd := exec.Command((Engine{}).RunCommand(stageDir)[0],
        (Engine{}).RunCommand(stageDir)[1:]...)
    cmd.Dir = root
    out, err := cmd.CombinedOutput()
    return string(out), err
}

func TestCodecAlignGoldenBytes(t *testing.T) {
    root := t.TempDir()
    c := ir.Contract{"count": ir.Int, "total": ir.Float, "flag": ir.Bool, "msg": ir.Str}
    st := &ir.Stage{Index: 1, Lang: "go", StartLine: 5, Body: []string{
        `count = 7`,
        `total = 3.25`,
        `flag = true`,
        `msg = "héllo"`,
    }}
    stageDir := filepath.Join(root, "stage1")
    stateDir := filepath.Join(root, "state")
    if err := os.MkdirAll(stateDir, 0o755); err != nil {
        t.Fatal(err)
    }
    sm := ir.SourceMap{}
    if err := (Engine{}).Emit(st, c, stageDir, stateDir, &sm); err != nil {
        t.Fatalf("Emit: %v", err)
    }
    if out, err := runStage(t, root, stageDir); err != nil {
        t.Fatalf("run: %v\n%s", err, out)
    }

    readBinary := func(name string) []byte {
        b, err := os.ReadFile(filepath.Join(stateDir, name))
        if err != nil {
            t.Fatalf("state %s: %v", name, err)
        }
        return b
    }
    if got, want := readBinary("count.macint"), goldenInt(7); !bytes.Equal(got, want) {
        t.Errorf("count bytes = %v, want %v", got, want)
    }
    if got, want := readBinary("total.macfloat"), goldenFloat(3.25); !bytes.Equal(got, want) {
        t.Errorf("total bytes = %v, want %v", got, want)
    }
    if got, want := readBinary("flag.macbool"), []byte{1}; !bytes.Equal(got, want) {
        t.Errorf("flag bytes = %v, want %v", got, want)
    }
    if got, want := readBinary("msg.macstr"), goldenStr("héllo"); !bytes.Equal(got, want) {
        t.Errorf("msg bytes = %v, want %v", got, want)
    }

    // internal/codec can read back what the stage wrote (interop).
    v, err := codec.Read(bytes.NewReader(readBinary("count.macint")), ir.Int)
    if err != nil || v.(int64) != 7 {
        t.Errorf("internal/codec read count = %v (%v)", v, err)
    }
    v, _ = codec.Read(bytes.NewReader(readBinary("msg.macstr")), ir.Str)
    if v.(string) != "héllo" {
        t.Errorf("internal/codec read msg = %v", v)
    }
}

func goldenInt(i int64) []byte {
    var b bytes.Buffer
    if err := codec.Write(&b, ir.Int, i); err != nil {
        panic(err)
    }
    return b.Bytes()
}

func goldenFloat(f float64) []byte {
    var b bytes.Buffer
    if err := codec.Write(&b, ir.Float, f); err != nil {
        panic(err)
    }
    return b.Bytes()
}

func goldenStr(s string) []byte {
    var b bytes.Buffer
    if err := codec.Write(&b, ir.Str, s); err != nil {
        panic(err)
    }
    return b.Bytes()
}

// TestStageToStage verifies read-after-write across two stages.
func TestStageToStage(t *testing.T) {
    root := t.TempDir()
    c := ir.Contract{"count": ir.Int, "msg": ir.Str}
    // stage1 writes count and msg
    st1 := &ir.Stage{Index: 1, Lang: "go", StartLine: 5, Body: []string{
        `count = 41`,
        `msg = "init"`,
    }}
    stage1 := filepath.Join(root, "stage1")
    stateDir := filepath.Join(root, "state")
    if err := os.MkdirAll(stateDir, 0o755); err != nil {
        t.Fatal(err)
    }
    sm1 := ir.SourceMap{}
    if err := (Engine{}).Emit(st1, c, stage1, stateDir, &sm1); err != nil {
        t.Fatalf("stage1 Emit: %v", err)
    }
    if out, err := runStage(t, root, stage1); err != nil {
        t.Fatalf("stage1 run: %v\n%s", err, out)
    }

    // stage2 reads both, modifies count, writes back
    st2 := &ir.Stage{Index: 2, Lang: "go", StartLine: 12, Body: []string{
        `count = count + 1`,
        `msg = msg + "!"`,
    }}
    stage2 := filepath.Join(root, "stage2")
    sm2 := ir.SourceMap{}
    if err := (Engine{}).Emit(st2, c, stage2, stateDir, &sm2); err != nil {
        t.Fatalf("stage2 Emit: %v", err)
    }
    if out, err := runStage(t, root, stage2); err != nil {
        t.Fatalf("stage2 run: %v\n%s", err, out)
    }

    cnt, err := os.ReadFile(filepath.Join(stateDir, "count.macint"))
    if err != nil {
        t.Fatal(err)
    }
    msg, _ := os.ReadFile(filepath.Join(stateDir, "msg.macstr"))
    v, _ := codec.Read(bytes.NewReader(cnt), ir.Int)
    if v.(int64) != 42 {
        t.Errorf("final count = %v, want 42", v)
    }
    m, _ := codec.Read(bytes.NewReader(msg), ir.Str)
    if m.(string) != "init!" {
        t.Errorf("final msg = %q, want init!", m)
    }
}

// TestEmitBuildFailCapture checks build errors are persisted and
// ParseDiagnostics maps to generated line (T8.4/T8.5).
func TestEmitBuildFailCapture(t *testing.T) {
    root := t.TempDir()
    c := ir.Contract{"count": ir.Int}
    st := &ir.Stage{Index: 1, Lang: "go", StartLine: 5, Body: []string{
        `count = undefinedName + 1`,
    }}
    stageDir := filepath.Join(root, "stage1")
    stateDir := filepath.Join(root, "state")
    os.MkdirAll(stateDir, 0o755)
    sm := ir.SourceMap{}
    err := (Engine{}).Emit(st, c, stageDir, stateDir, &sm)
    if err == nil {
        t.Fatal("Emit should fail on build error")
    }
    berr, rerr := os.ReadFile(filepath.Join(stageDir, buildErrorsName))
    if rerr != nil {
        t.Fatalf("build-errors.txt missing: %v", rerr)
    }
    if diags := (Engine{}).ParseDiagnostics(berr); len(diags) == 0 {
        t.Fatalf("no diagnostics from build stderr: %s", berr)
    }
    if e, ok := sm[goFile+":1"]; !ok || e.Kind != ir.OrigSynthetic {
        t.Errorf("missing synthetic entry: %+v", e)
    }
}

// TestRuntimePanicMapping exercises runtime-stack diagnostics and
// source-map back-mapping (T8.5/T8.7).
func TestRuntimePanicMapping(t *testing.T) {
    root := t.TempDir()
    c := ir.Contract{"count": ir.Int}
    st := &ir.Stage{Index: 1, Lang: "go", StartLine: 5, Body: []string{
        `count = 1`,
        `panic("boom")`,
    }}
    stageDir := filepath.Join(root, "stage1")
    stateDir := filepath.Join(root, "state")
    os.MkdirAll(stateDir, 0o755)
    sm := ir.SourceMap{}
    if err := (Engine{}).Emit(st, c, stageDir, stateDir, &sm); err != nil {
        t.Fatalf("Emit: %v", err)
    }
    out, err := runStage(t, root, stageDir)
    if err == nil {
        t.Fatal("expected non-zero exit on panic")
    }
    diags := (Engine{}).ParseDiagnostics([]byte(out))
    if len(diags) == 0 {
        t.Fatalf("no runtime diagnostics, stderr=%s", out)
    }
    if !strings.Contains(diags[0].Msg, "boom") {
        t.Errorf("diag msg = %q, want panic message", diags[0].Msg)
    }
    if !strings.Contains(diags[0].Msg, "main.go") {
        t.Errorf("diag msg = %q, want gen file", diags[0].Msg)
    }
    // back-map the diagnosed generated line to a .mac source line
    for _, d := range diags {
        if d.Span.StartLine == 0 {
            continue
        }
        if e, ok := sm[goFile+":"+strconv.Itoa(d.Span.StartLine)]; ok && e.SourceLine != 0 {
            t.Logf("gen line %d -> .mac line %d", d.Span.StartLine, e.SourceLine)
            return
        }
    }
    t.Errorf("could not back-map any diagnosed main.go line; diags=%+v sm=%v", diags, sm)
}
