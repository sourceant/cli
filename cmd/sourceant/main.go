// Command sourceant is the SourceAnt command line client.
package main

import (
	"os"

	"github.com/sourceant/cli/internal/command"
)

func main() {
	os.Exit(command.Run(os.Args[1:], os.Stdout, os.Stderr))
}
