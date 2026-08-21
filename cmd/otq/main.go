// Command otq is a jq-like CLI for querying OpenTelemetry GenAI traces.
package main

import (
	"os"

	"otq/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
