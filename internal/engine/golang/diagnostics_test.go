package golang

import (
	"strings"
	"testing"
)

func TestParseDiagnosticsCompile(t *testing.T) {
	stderr := `# macaronicstage
./main.go:12:5: undefined: x
./main.go:9:2: something else
`
	diags := (Engine{}).ParseDiagnostics([]byte(stderr))
	if len(diags) != 2 {
		t.Fatalf("diags = %+v, want 2", diags)
	}
	d0 := diags[0]
	if d0.Span.StartLine != 12 || d0.Span.StartCol != 5 {
		t.Errorf("d0 span = %+v", *d0.Span)
	}
	if !strings.Contains(d0.Msg, "undefined") {
		t.Errorf("d0 msg = %q", d0.Msg)
	}
	if !strings.Contains(d0.Msg, "main.go") {
		t.Errorf("d0 should carry gen file, msg=%q", d0.Msg)
	}
}

func TestParseDiagnosticsRuntime(t *testing.T) {
	stderr := `panic: boom

goroutine 1 [running]:
main.main()
	/tmp/x/foo/bar/main.go:34 +0x4b
exit status 2
`
	diags := (Engine{}).ParseDiagnostics([]byte(stderr))
	if len(diags) == 0 {
		t.Fatal("no diagnostics")
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Msg, "boom") && d.Span.StartLine == 34 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("runtime panic diags = %+v, want boom@34", diags)
	}
}

func TestParseDiagnosticsEmpty(t *testing.T) {
	if diags := (Engine{}).ParseDiagnostics(nil); len(diags) != 0 {
		t.Errorf("empty stderr produced diags %+v", diags)
	}
}
