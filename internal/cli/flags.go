package cli

import (
	"flag"
	"fmt"
	"io"
)

type options struct {
	raw    bool
	pretty bool
	query  string
	paths  []string
}

const usageText = `otq — a jq-like CLI for OpenTelemetry GenAI traces

Usage:
  otq [flags] QUERY PATH...

  QUERY  a pipeline query, e.g. 'spans | select(.name == "gen_ai.chat") | limit(10)'
  PATH   one or more OTLP JSON/JSONL files, directories, or glob patterns

Flags:
`

func parseFlags(args []string, stderr io.Writer) (*options, error) {
	fs := flag.NewFlagSet("otq", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usageText)
		fs.PrintDefaults()
	}

	raw := fs.Bool("raw", false, "raw string output for scalar string results, matches jq -r")
	fs.BoolVar(raw, "r", false, "shorthand for --raw")
	pretty := fs.Bool("pretty", false, "multi-line indented JSON per value")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	rest := fs.Args()
	if len(rest) < 2 {
		fs.Usage()
		return nil, fmt.Errorf("expected a QUERY and at least one PATH")
	}

	return &options{
		raw:    *raw,
		pretty: *pretty,
		query:  rest[0],
		paths:  rest[1:],
	}, nil
}
