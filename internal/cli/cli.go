// Package cli implements the macaronic command-line interface.
// Run parses args, dispatches to a subcommand handler and returns the
// process exit code. Keeping handlers here (not in package main) makes
// them unit-testable.
package cli

import (
	"fmt"
	"io"
)

// subcommand names registered in M1.
const (
	cmdParse = "parse"
	cmdCheck = "check"
	cmdBuild = "build"
	cmdRun   = "run"
)

// exit codes.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

// proposalHelp marks placeholder subcommands until M2..M9 wire
// real implementations in.
const notImplementedMsg = "未实现：后续里程碑接入（M2/M3/M4/M9）"

const topUsage = `macaronic：混合多语言脚本编译运行工具

用法：
  macaronic <脚本.mac>           编译并运行（等价 run）
  macaronic <子命令> [参数] ...

子命令：
  parse    解析并输出 IR
  check    静态检查并输出检查报告
  build    生成产物目录
  run      编译、构建并执行

全局参数：
  -h, --help  打印帮助
`

const stubUsage = `%s：功能未实现

用法：macaronic %s <脚本.mac>
`

// Run executes the CLI and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0:
		fmt.Fprint(stderr, topUsage)
		return exitUsage
	case args[0] == "-h" || args[0] == "--help":
		fmt.Fprint(stdout, topUsage)
		return exitOK
	}

	name := args[0]
	switch name {
	case cmdParse, cmdCheck, cmdBuild, cmdRun:
		return runSub(name, args[1:], stdout, stderr)
	}

	if !looksLikeScript(name) {
		fmt.Fprintf(stderr, "macaronic：未知子命令 %q\n\n%s", name, topUsage)
		return exitUsage
	}
	// Shorthand: macaronic <script> === macaronic run <script>.
	return runSub(cmdRun, args, stdout, stderr)
}

// runSub validates positional args, handles -h/--help and forwards to
// the (currently stubbed) subcommand driver.
func runSub(name string, rest []string, stdout, stderr io.Writer) int {
	for _, r := range rest {
		if r == "-h" || r == "--help" {
			fmt.Fprintf(stdout, stubUsage, name, name)
			return exitOK
		}
	}
	if len(rest) != 1 {
		fmt.Fprintf(stderr, "macaronic：%s 恰需 1 个脚本参数，得到 %d 个\n", name, len(rest))
		return exitUsage
	}
	fmt.Fprintf(stderr, "%s：%s\n", name, notImplementedMsg)
	return exitFail
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
