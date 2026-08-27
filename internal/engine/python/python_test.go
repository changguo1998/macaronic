package python

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/changguo1998/macaronic/internal/codec"
	"github.com/changguo1998/macaronic/internal/ir"
)

// requirePython skips the test when python3 is unavailable.
func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}
}

func stage(lang string, line int, body ...string) *ir.Stage {
	return &ir.Stage{Index: 1, Lang: lang, StartLine: line, EndLine: line + len(body) - 1, Body: body}
}

// writeStateFile creates and returns an io.Writer for path.
func writeStateFile(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// runPython runs stageDir/run.py and returns combined output.
func runPython(_ *testing.T, stageDir string) (string, error) {
	cmd := exec.Command("python3", "run.py")
	cmd.Dir = stageDir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestAnalyzeMissingAnnotation(t *testing.T) {
	cases := []struct {
		name  string
		body  []string
		start int
		want  string
	}{
		{"bare use", []string{"print(count)"}, 3, "used without type annotation"},
		{"plain assignment", []string{"count = 5"}, 3, "used without type annotation"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := stage("python", c.start, c.body...)
			_, _, err := (Engine{}).Analyze(st, ir.Contract{"count": ir.Int})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %q, want substring %q", err.Error(), c.want)
			}
		})
	}
}

func TestAnalyzeOK(t *testing.T) {
	st := stage("python", 4,
		"count: int",
		"total: float = count * 1.5",
		"print(total)")
	reads, writes, err := (Engine{}).Analyze(st, ir.Contract{"count": ir.Int, "total": ir.Float})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !reads["count"] || reads["total"] {
		t.Errorf("reads = %v (count read, total not)", reads)
	}
	if !writes["total"] || writes["count"] {
		t.Errorf("writes = %v (total write, count not)", writes)
	}
}

func TestAnalyzeReadWriteSemantics(t *testing.T) {
	// Sequential-evaluation semantics (architecture §6).
	cases := []struct {
		name string
		body []string
		want string // "r", "w", "rw", "-"
	}{
		{"produce then augment", []string{"x: int = 0", "x += 1"}, "w"},
		{"declare then augment", []string{"x: int", "x += 1"}, "rw"},
		{"self-ref annotation", []string{"x: int = x + 1"}, "rw"},
		{"declare then plain assign", []string{"x: int", "x = 5"}, "rw"},
		{"augment before annotation", []string{"x += 1", "x: int"}, "rw"},
		{"annotated then print", []string{"x: int = 0", "print(x)"}, "w"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := stage("python", 3, c.body...)
			reads, writes, err := (Engine{}).Analyze(st, ir.Contract{"x": ir.Int})
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			_, read := reads["x"]
			_, write := writes["x"]
			got := "-"
			if read && write {
				got = "rw"
			} else if read {
				got = "r"
			} else if write {
				got = "w"
			}
			if got != c.want {
				t.Errorf("body %v: got %q, want %q (reads=%v writes=%v)",
					c.body, got, c.want, reads, writes)
			}
		})
	}
}

func TestAnalyzeErrorLine(t *testing.T) {
	st := stage("python", 10, "print(count)")
	_, _, err := (Engine{}).Analyze(st, ir.Contract{"count": ir.Int})
	if err == nil {
		t.Fatal("expected error")
	}
	// first body line maps to StartLine+1 (line 11)
	if !strings.Contains(err.Error(), "line 11") {
		t.Errorf("err = %q", err)
	}
}

func TestEmitReadWrite(t *testing.T) {
	requirePython(t)
	dir := t.TempDir()
	stageDir := filepath.Join(dir, "stage1")
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// pre-populate state with an int and a str
	if err := codec.WriteInt(writeStateFile(t, filepath.Join(stateDir, "count.macint")), 5); err != nil {
		t.Fatal(err)
	}
	if err := codec.WriteStr(writeStateFile(t, filepath.Join(stateDir, "msg.macstr")), "hello"); err != nil {
		t.Fatal(err)
	}

	sm := ir.SourceMap{}
	st := stage("python", 3,
		"count: int",
		"msg: str",
		"print(count, msg)",
	)
	c := ir.Contract{"count": ir.Int, "msg": ir.Str}
	if err := (Engine{}).Emit(st, c, stageDir, stateDir, &sm); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	out, err := runPython(t, stageDir)
	if err != nil {
		t.Fatalf("run: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "5 hello") {
		t.Errorf("output = %q, want %q", out, "5 hello")
	}
}

func TestEmitTracebackDiagnostic(t *testing.T) {
	requirePython(t)
	dir := t.TempDir()
	stageDir := filepath.Join(dir, "stage")
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := codec.WriteInt(writeStateFile(t, filepath.Join(stateDir, "count.macint")), 5); err != nil {
		t.Fatal(err)
	}
	// user code divides by zero -> ZeroDivisionError at the print line
	st := stage("python", 3, "count: int", "print(1 // (count - 5))")
	c := ir.Contract{"count": ir.Int}
	var sm ir.SourceMap
	if err := (Engine{}).Emit(st, c, stageDir, stateDir, &sm); err != nil {
		t.Fatal(err)
	}
	bb, runErr := runPython(t, stageDir)
	if runErr == nil {
		t.Fatal("expected python to fail")
	}
	diags := (Engine{}).ParseDiagnostics([]byte(bb + "\n"))
	if len(diags) == 0 {
		t.Fatalf("expected diagnostics")
	}
	if !strings.Contains(diags[0].Msg, "run.py:") {
		t.Errorf("diag msg = %q, want genFile prefix", diags[0].Msg)
	}
	if diags[0].Span == nil || diags[0].Span.StartLine == 0 {
		t.Errorf("diag span = %+v", diags[0].Span)
	}
}

func TestParseDiagnosticsTraceback(t *testing.T) {
	stderr := `Traceback (most recent call last):
  File "run.py", line 3, in <module>
    print(1 // (count - 5))
ZeroDivisionError: integer division or modulo by zero
`
	diags := (Engine{}).ParseDiagnostics([]byte(stderr))
	if len(diags) != 1 {
		t.Fatalf("diags = %+v", diags)
	}
	d := diags[0]
	if !strings.Contains(d.Msg, "run.py:3") ||
		!strings.Contains(d.Msg, "ZeroDivisionError") {
		t.Errorf("msg = %q", d.Msg)
	}
	if d.Span == nil || d.Span.StartLine != 3 {
		t.Errorf("span = %+v", d.Span)
	}
}

func TestEmitSourceMap(t *testing.T) {
	st := stage("python", 4, "count: int", "x: str = str(count)")
	c := ir.Contract{"count": ir.Int, "x": ir.Str}
	dir := t.TempDir()
	var sm ir.SourceMap
	if err := (Engine{}).Emit(st, c, filepath.Join(dir, "s"), filepath.Join(dir, "st"), &sm); err != nil {
		t.Fatal(err)
	}
	// generated lines exist for synthetic header + prologue + body
	if len(sm) == 0 {
		t.Fatal("expected sourcemap entries")
	}
	// find orig source entries (body mapped 1:1)
	orig := 0
	for _, e := range sm {
		if e.Kind == ir.OrigSource {
			orig++
		}
	}
	if orig != 2 {
		t.Errorf("orig entries = %d, want 2", orig)
	}
}

func TestAnalyzePythonParenContinuation(t *testing.T) {
	st := stage("python", 3,
		"x: int = make_value(",
		"    x",
		")",
	)
	reads, writes, err := (Engine{}).Analyze(st, ir.Contract{"x": ir.Int})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !reads["x"] || !writes["x"] {
		t.Fatalf("reads=%v writes=%v, want continuation RHS read+write", reads, writes)
	}
}

func TestAnalyzePythonSubscriptWrite(t *testing.T) {
	st := stage("python", 3, "x: int", "x[0] = 1")
	reads, writes, err := (Engine{}).Analyze(st, ir.Contract{"x": ir.Int})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !reads["x"] || !writes["x"] {
		t.Fatalf("reads=%v writes=%v, want subscript read+write", reads, writes)
	}
}

func TestAnalyzePythonDefParamShadow(t *testing.T) {
	st := stage("python", 3, "def f(x):", "    return x")
	_, _, err := (Engine{}).Analyze(st, ir.Contract{"x": ir.Int})
	if err == nil || !strings.Contains(err.Error(), "shadows the contract binding") {
		t.Fatalf("err=%v, want function-parameter shadow error", err)
	}
}
