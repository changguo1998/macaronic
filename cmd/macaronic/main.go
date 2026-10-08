// Command macaronic compiles and runs multi-language
// "macaronic" .mac scripts. The CLI only dispatches; all logic lives in
// internal/cli so it stays testable.
package main

import (
	"os"

	"github.com/changguo1998/macaronic/internal/cli"
	"github.com/changguo1998/macaronic/internal/engine"
	"github.com/changguo1998/macaronic/internal/engine/bash"
	"github.com/changguo1998/macaronic/internal/engine/csh"
	"github.com/changguo1998/macaronic/internal/engine/golang"
	"github.com/changguo1998/macaronic/internal/engine/node"
	"github.com/changguo1998/macaronic/internal/engine/python"
	"github.com/changguo1998/macaronic/internal/engine/sh"
	"github.com/changguo1998/macaronic/internal/engine/zsh"
)

func main() {
	engine.Register(bash.Engine{})
	engine.Register(sh.Engine{})
	engine.Register(zsh.Engine{})
	engine.Register(csh.Engine{})
	engine.Register(python.Engine{})
	engine.Register(golang.Engine{})
	engine.Register(node.Engine{})
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
