// Package cli wires flag parsing, input loading, query parsing, evaluation,
// and output together. Run never calls os.Exit itself — it returns an exit
// code — so it stays directly testable in-process (see test/integration_test.go).
package cli

import (
	"flag"
	"fmt"
	"io"

	"otq/internal/eval"
	"otq/internal/flatten"
	"otq/internal/otlp"
	"otq/internal/parser"
	"otq/output"
)

// Run is the single, centralized panic-recovery point: any unexpected panic
// anywhere in the pipeline is caught here and reported as an internal error
// rather than a raw stack trace, per the spec's error-handling requirement.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) (exitCode int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "otq: internal error (please report): %v\n", r)
			exitCode = 1
		}
	}()
	return run(args, stdin, stdout, stderr)
}

func run(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		fmt.Fprintf(stderr, "otq: %v\n", err)
		return 1
	}

	q, err := parser.Parse(opts.query)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	rawSpans, err := otlp.LoadInputs(opts.paths)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	values := make([]eval.Value, 0, len(rawSpans))
	for _, raw := range rawSpans {
		sp, ferr := flatten.Flatten(raw)
		if ferr != nil {
			fmt.Fprintln(stderr, ferr)
			return 1
		}
		values = append(values, sp.ToValue())
	}

	results, err := eval.Evaluate(q, values)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	w := output.Writer{W: stdout, Raw: opts.raw, Pretty: opts.pretty}
	if err := w.WriteAll(results); err != nil {
		fmt.Fprintf(stderr, "otq: %v\n", err)
		return 1
	}
	return 0
}
