// Command otq is a jq-like CLI for querying OpenTelemetry GenAI traces.
package main

import (
	"fmt"
	"os"

	"otq/internal/cli"
)

// version/commit/date are set via -ldflags at release build time (see
// .goreleaser.yml); "dev" et al. are what a plain `go build`/`go run` gets.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Printf("otq %s (%s, built %s)\n", version, commit, date)
		return
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
