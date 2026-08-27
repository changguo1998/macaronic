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
            // assert stub message collides? use "not implemented"
            name:      "sub help",
            args:      []string{"run", "--help"},
            wantCode:  exitOK,
            stdoutHas: "未实现",
        },
        {
            name:      "parse stub",
            args:      []string{"parse", "foo.mac"},
            wantCode:  exitFail,
            stdoutHas: "",
            stderrHas: "未实现",
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
