package cli

import (
	"strings"
	"testing"
)

// smokeCase holds one CLI invocation expectations.
type smokeCase struct {
	name      string
	args      []string
	wantCode  int
	stdoutHas string
	stderrHas string
}

func TestRunSmoke(t *testing.T) {
	cases := []smokeCase{
		{
			name:      "no args",
			args:      nil,
			wantCode:  exitUsage, // 2
			stdoutHas: "",
			stderrHas: "",
		},
		{
			name:      "top help",
			args:      []string{"--help"},
			wantCode:  exitOK,
			stdoutHas: "macaronic：混合多语言脚本编译运行工具",
		},
		{
			name:      "sub help",
			args:      []string{"run", "--help"},
			wantCode:  exitOK,
			stdoutHas: "run：编译、构建并执行\n\n用法：macaronic run <脚本.mac>",
		},
		{
			name:      "parse missing file",
			args:      []string{"parse", "no-such-file.mac"},
			wantCode:  exitFail,
			stdoutHas: "",
			stderrHas: "macaronic parse: ",
		},
		{
			name:      "shorthand equals run",
			args:      []string{"foo.mac"},
			wantCode:  exitFail,
			stderrHas: "no such file", // run now executes; missing file -> fail
		},
		{
			name:      "unknown subcommand",
			args:      []string{"frobnicate"},
			wantCode:  exitUsage,
			stderrHas: "未知子命令",
		},
		{
			name:      "extra positional arg",
			args:      []string{"run", "foo.mac", "bar"},
			wantCode:  exitUsage,
			stderrHas: "恰需 1 个",
		},
	}
	for _, c := range cases {
		var out, err strings.Builder
		got := Run(c.args, &out, &err)
		if !strings.Contains(out.String(), c.stdoutHas) {
			t.Errorf("%s: stdout = %q, want substring %q", c.name, out.String(), c.stdoutHas)
		}
		if !strings.Contains(err.String(), c.stderrHas) {
			t.Errorf("%s: stderr = %q, want substring %q", c.name, err.String(), c.stderrHas)
		}
		if got != c.wantCode {
			t.Errorf("%s: code = %d, want %d", c.name, got, c.wantCode)
		}
	}
}

// TestParseSummary pins the parse command's IR summary: block list plus
// the contract in name order. No engine needs to be registered, since
// parse never analyzes.
func TestParseSummary(t *testing.T) {
	path := writeFile(t, "two.mac", "#!mac\n[contract]\nmsg = \"str\"\ncount = \"int\"\n\n#!bash\ncount=1\n\n#!python\nprint(msg)\n")

	var out, err strings.Builder
	if code := Run([]string{"parse", path}, &out, &err); code != exitOK {
		t.Fatalf("parse code = %d, want %d\nstdout=%q\nstderr=%q",
			code, exitOK, out.String(), err.String())
	}
	want := "path: " + path + "\n" +
		"stages: 2\n" +
		"contract:\n" +
		"  count int\n" +
		"  msg str\n" +
		"stage 1: bash (line 6)\n" +
		"stage 2: python (line 9)\n"
	if out.String() != want {
		t.Errorf("parse stdout:\ngot  %q\nwant %q", out.String(), want)
	}
	if err.Len() != 0 {
		t.Errorf("parse stderr = %q, want empty", err.String())
	}
}

// TestParseBadHeadBlock checks that a head-block error fails the command
// with the usual "macaronic <cmd>: " prefix instead of a partial summary.
func TestParseBadHeadBlock(t *testing.T) {
	path := writeFile(t, "bad.mac", "#!bash\necho hi\n")

	var out, err strings.Builder
	if code := Run([]string{"parse", path}, &out, &err); code != exitFail {
		t.Errorf("parse code = %d, want %d", code, exitFail)
	}
	if out.Len() != 0 {
		t.Errorf("parse stdout = %q, want empty on error", out.String())
	}
	if !strings.HasPrefix(err.String(), "macaronic parse: ") {
		t.Errorf("stderr = %q, want macaronic parse: prefix", err.String())
	}
}
