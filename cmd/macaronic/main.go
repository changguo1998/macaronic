// Command macaronic compiles and runs multi-language
// "macaronic" .mac scripts. The CLI only dispatches; all logic lives in
// internal/cli so it stays testable.
package main

import (
    "os"

    "github.com/changguo1998/macaronic/internal/cli"
    "github.com/changguo1998/macaronic/internal/engine"
    "github.com/changguo1998/macaronic/internal/engine/golang"
    "github.com/changguo1998/macaronic/internal/engine/python"
    "github.com/changguo1998/macaronic/internal/engine/shell"
)

func main() {
    engine.Register(shell.Engine{})
    engine.Register(python.Engine{})
    engine.Register(golang.Engine{})
    os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
