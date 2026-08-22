# Contributing to otq

Thanks for considering a contribution.

## Prerequisites

- Go 1.22+

## Getting set up

```sh
git clone git@github.com:ChatIntel/otq.git
cd otq
go build ./...
go test ./...
```

## Before opening a PR

```sh
gofmt -l .        # must print nothing
go vet ./...      # must be clean
go test ./...     # must pass
```

Every new behavior should have a test — unit tests live next to the code they cover (`*_test.go`), and `test/integration_test.go` drives the CLI end-to-end against `testdata/sample_trace.{json,jsonl}`. If you're touching ingestion, the query grammar, or output formatting, add or update a fixture-driven case there too.

## Branching and commits

- Branch off `main`: `feat/<short-slug>` for new functionality, `chore/<short-slug>` for docs/cleanup, `fix/<short-slug>` for bug fixes.
- Commit messages: a short imperative summary line, a blank line, then the *why* (not a restatement of the diff) if it isn't obvious from the summary alone.
- Open a PR against `main`. CI (`go build`/`go vet`/`go test`) must pass before merge.

## Code style

- No comments unless they explain something non-obvious — a hidden constraint, an intentional deviation from a "natural" implementation, a judgment call the spec left open. Don't comment what the code already says.
- Don't add abstractions, config knobs, or error handling for cases that can't happen. Match the existing packages' shape (see `internal/`) rather than introducing a new pattern for one file.

## Extending the query grammar

otq's query language (see `internal/parser`) is deliberately a fixed, small subset of jq-like syntax — not an open-ended reimplementation of jq. If a use case needs a construct the grammar doesn't support, that's a signal to propose a deliberate grammar extension (open an issue first), not to work around it with a one-off special case.

## License

By contributing, you agree your contributions are licensed under this project's [Apache-2.0 license](LICENSE).
