// Package cli implements the macaronic command-line interface.
// Run parses args, dispatches to a subcommand handler and returns the
// process exit code. Keeping handlers here (not in package main) makes
// them unit-testable.
package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/changguo1998/macaronic/internal/ir"
)

// subcommand names registered in M1.
const (
	cmdParse = "parse"
	cmdCheck = "check"
	cmdBuild = "build"
	cmdRun   = "run"
	cmdCodec = "codec"
)

// exit codes.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

// subcommands lists the script-taking subcommands in pipeline order
// (architecture §3): each runs stageParse plus the stages up to and
// including its own. The table is the single source of truth for
// dispatch, the top-level help and per-subcommand help.
var subcommands = []struct {
	name string
	desc string
	run  func(path string, stdout, stderr io.Writer) int
}{
	{cmdParse, "解析并输出 IR", runParse},
	{cmdCheck, "静态检查并输出检查报告", runCheck},
	{cmdBuild, "生成产物目录", runBuildCmd},
	{cmdRun, "编译、构建并执行", runCmd},
}

// subcommand looks name up in the pipeline table.
func subcommand(name string) (func(string, io.Writer, io.Writer) int, string, bool) {
	for _, s := range subcommands {
		if s.name == name {
			return s.run, s.desc, true
		}
	}
	return nil, "", false
}

// topUsage renders the top-level help from the subcommand table, so the
// listed commands cannot drift from the ones actually wired up.
func topUsage() string {
	var b strings.Builder
	fmt.Fprint(&b, "macaronic：混合多语言脚本编译运行工具\n\n用法：\n"+
		"  macaronic <脚本.mac>           编译并运行（等价 run）\n"+
		"  macaronic <子命令> [参数] ...\n\n子命令：\n")
	for _, s := range subcommands {
		fmt.Fprintf(&b, "  %-8s %s\n", s.name, s.desc)
	}
	fmt.Fprint(&b, "\n全局参数：\n  -h, --help  打印帮助\n")
	return b.String()
}

// Run executes the CLI and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0:
		fmt.Fprint(stderr, topUsage())
		return exitUsage
	case args[0] == "-h" || args[0] == "--help":
		fmt.Fprint(stdout, topUsage())
		return exitOK
	}

	name := args[0]
	if name == cmdCodec {
		return runCodec(args[1:], stdout, stderr)
	}
	if _, _, ok := subcommand(name); ok {
		return runSub(name, args[1:], stdout, stderr)
	}

	if !looksLikeScript(name) {
		fmt.Fprintf(stderr, "macaronic：未知子命令 %q\n\n%s", name, topUsage())
		return exitUsage
	}
	// Shorthand: macaronic <script> === macaronic run <script>.
	return runSub(cmdRun, args, stdout, stderr)
}

// runSub validates positional args, handles -h/--help and forwards to
// the handler of one pipeline prefix.
func runSub(name string, rest []string, stdout, stderr io.Writer) int {
	run, desc, _ := subcommand(name)
	for _, r := range rest {
		if r == "-h" || r == "--help" {
			fmt.Fprintf(stdout, "%s：%s\n\n用法：macaronic %s <脚本.mac>\n", name, desc, name)
			return exitOK
		}
	}
	if len(rest) != 1 {
		fmt.Fprintf(stderr, "macaronic：%s 恰需 1 个脚本参数，得到 %d 个\n", name, len(rest))
		return exitUsage
	}
	return run(rest[0], stdout, stderr)
}

// runParse implements `macaronic parse <script>`: pipeline stage 1
// only (architecture §3), printing the IR summary.
func runParse(path string, stdout, stderr io.Writer) int {
	p, err := stageParse(path)
	if err != nil {
		fmt.Fprintf(stderr, "macaronic parse: %v\n", err)
		return exitFail
	}
	printParse(stdout, p)
	return exitOK
}

// runCheck implements `macaronic check <script>`: pipeline prefix
// parse + check. It writes no artifacts.
func runCheck(path string, stdout, stderr io.Writer) int {
	p, err := stageParse(path)
	if err != nil {
		fmt.Fprintf(stderr, "macaronic check: %v\n", err)
		return exitFail
	}
	if !stageCheck(p, stdout) {
		return exitFail
	}
	return exitOK
}

// printParse renders the parse result: script path, block list (index,
// language, line of the block marker) and the contract in deterministic
// name order.
func printParse(w io.Writer, p *ir.Program) {
	fmt.Fprintf(w, "path: %s\n", p.Path)
	fmt.Fprintf(w, "stages: %d\n", len(p.Stages))
	names := make([]string, 0, len(p.Contract))
	for name := range p.Contract {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		fmt.Fprint(w, "contract: (none)\n")
	} else {
		fmt.Fprint(w, "contract:\n")
		for _, name := range names {
			fmt.Fprintf(w, "  %s %s\n", name, p.Contract[name])
		}
	}
	for i := range p.Stages {
		st := &p.Stages[i]
		fmt.Fprintf(w, "stage %d: %s (line %d)\n", st.Index, st.Lang, st.StartLine)
	}
}

// looksLikeScript guesses whether an unknown first arg is meant as the
// shorthand "macaronic <script>" form instead of an unknown subcommand.
// Heuristic: an explicit path, an extension, or presence of '/' or '.'.
// Anything else is reported as an unknown command.
func looksLikeScript(arg string) bool {
	for i := 0; i < len(arg); i++ {
		switch arg[i] {
		case '/', '\\', '.':
			return true
		}
	}
	return false
}
