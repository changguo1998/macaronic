// Command macaronic compiles and runs multi-language
// "macaronic" .mac scripts. The CLI only dispatches; all logic lives in
// internal/cli so it stays testable.
package main

import (
	"os"

	"github.com/changguo1998/macaronic/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
