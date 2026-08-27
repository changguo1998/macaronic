package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/changguo1998/macaronic/internal/engine"
	golangengine "github.com/changguo1998/macaronic/internal/engine/golang"
	pythonengine "github.com/changguo1998/macaronic/internal/engine/python"
	"github.com/changguo1998/macaronic/internal/engine/shell"
)

// M11 CLI behavior: warnings are visible but never block; errors still
// block. The fixtures live in testdata/ and use the real shell engine
// (registered here, as main.go does in production) so the CLI path
// exercises actual inference, not mocks.

func TestCheckWarningOnlyExitsOK(t *testing.T) {
	engine.Register(shell.Engine{})
	path := filepath.Join("testdata", "warn_only.mac")
	var out, err strings.Builder
	if code := Run([]string{"check", path}, &out, &err); code != exitOK {
		t.Fatalf("check code = %d, want 0 (warnings do not block)\nstdout=%q stderr=%q",
			code, out.String(), err.String())
	}
	if !strings.Contains(out.String(), "warning:") {
		t.Errorf("stdout = %q, want a warning line", out.String())
	}
	if !strings.Contains(out.String(), `contract variable "total" declared but never`) {
		t.Errorf("stdout = %q, want unused-contract warning for total", out.String())
	}
}

func TestCheckErrorStillBlocks(t *testing.T) {
	engine.Register(shell.Engine{})
	path := filepath.Join("testdata", "read_before_write.mac")
	var out, err strings.Builder
	if code := Run([]string{"check", path}, &out, &err); code != exitFail {
		t.Fatalf("check code = %d, want 1 (errors still block)\nstdout=%q",
			code, out.String())
	}
	if !strings.Contains(out.String(), `read of "count" before any write`) {
		t.Errorf("stdout = %q, want read-before-write error", out.String())
	}
}

// TestBuildWarningOnlyExitsOK proves `build` is not blocked by warnings
// either (objective: build 仅被 error 阻断). The fixture is copied to a
// temp dir so emitted artifacts do not pollute testdata/.
func TestBuildWarningOnlyExitsOK(t *testing.T) {
	engine.Register(shell.Engine{})
	data, err := os.ReadFile(filepath.Join("testdata", "warn_only.mac"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "warn_only.mac")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut strings.Builder
	if code := Run([]string{"build", path}, &out, &errOut); code != exitOK {
		t.Fatalf("build code = %d, want 0 (warnings do not block)\nstdout=%q stderr=%q",
			code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "warning:") {
		t.Errorf("stdout = %q, want a warning line", out.String())
	}
}

// TestCheckObservedNotInferredWarns covers the M12 safety net through
// the real CLI: the shell body mentions count inside a string (no $),
// so nothing is inferred; the framework warns that the value may not
// be injected, but the check still exits 0.
func TestCheckObservedNotInferredWarns(t *testing.T) {
	engine.Register(shell.Engine{})
	path := filepath.Join("testdata", "observed_not_inferred.mac")
	var out, err strings.Builder
	if code := Run([]string{"check", path}, &out, &err); code != exitOK {
		t.Fatalf("check code = %d, want 0 (M12 warnings do not block)\nstdout=%q stderr=%q",
			code, out.String(), err.String())
	}
	if !strings.Contains(out.String(), "warning:") {
		t.Errorf("stdout = %q, want a warning line", out.String())
	}
	if !strings.Contains(out.String(), "was not inferred as read or write") {
		t.Errorf("stdout = %q, want M12 warning wording", out.String())
	}
}

func TestCheckArithmeticDependency(t *testing.T) {
	engine.Register(shell.Engine{})
	cases := []struct {
		name    string
		file    string
		want    int
		message string
	}{
		{"without prior writer", "arithmetic_without_prior.mac", exitFail, "read of \"count\" before any write"},
		{"with prior writer", "arithmetic_with_prior.mac", exitOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut strings.Builder
			code := Run([]string{"check", filepath.Join("testdata", tc.file)}, &out, &errOut)
			if code != tc.want {
				t.Fatalf("code = %d, want %d; stdout=%q stderr=%q", code, tc.want, out.String(), errOut.String())
			}
			if tc.message != "" && !strings.Contains(out.String(), tc.message) {
				t.Errorf("stdout = %q, want %q", out.String(), tc.message)
			}
		})
	}
}

func TestCheckStaticDiagnosticsUseOriginalLines(t *testing.T) {
	engine.Register(shell.Engine{})
	engine.Register(pythonengine.Engine{})
	engine.Register(golangengine.Engine{})
	cases := []struct {
		name string
		file string
		want string
	}{
		{"shell", "shell_nonfirst_diagnostic.mac", `error: stage 1 line 7 var "count":`},
		{"python", "python_nonfirst_diagnostic.mac", `error: stage 1 line 7 var "count":`},
		{"go", "go_nonfirst_diagnostic.mac", `error: stage 1 line 7 var "count":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut strings.Builder
			code := Run([]string{"check", filepath.Join("testdata", tc.file)}, &out, &errOut)
			if code != exitFail {
				t.Fatalf("code = %d, want failure; stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("stdout = %q, want %q", out.String(), tc.want)
			}
		})
	}
}
