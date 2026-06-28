# go-format

[![Go Reference](https://pkg.go.dev/badge/github.com/stremovskyy/go-format.svg)](https://pkg.go.dev/github.com/stremovskyy/go-format)
[![Go Report Card](https://goreportcard.com/badge/github.com/stremovskyy/go-format)](https://goreportcard.com/report/github.com/stremovskyy/go-format)
[![License](https://img.shields.io/github/license/stremovskyy/go-format)](LICENSE)

`go-format` is an opinionated Go formatter for codebases that already use
`gofmt`, but still want more readable line wrapping and vertical spacing.

It runs:

- `gofmt`
- pinned `golines` for long-line wrapping
- `gofumpt`
- a logical blank-line pass for dense functions

The blank-line pass separates guard clauses, validation and normalization,
setup/defer cleanup blocks, decision branches, lock groups, loops/switches, and
final returns. It keeps coupled `value, err := call()` / `if err != nil` pairs
together and keeps leading comments attached to the statement they describe.
It does not insert blank lines between every statement.

## Installation

```sh
go install github.com/stremovskyy/go-format@latest
```

For reproducible CI, install or run a tagged version:

```sh
go run github.com/stremovskyy/go-format@v0.1.0 --check ./...
```

## Usage

Format the current directory:

```sh
go-format --write ./...
```

Check formatting in CI:

```sh
go-format --check ./...
```

Use a different target line length:

```sh
go-format --write --max-len 100 ./...
```

Limit parallel file formatting:

```sh
go-format --write --jobs 4 ./...
```

Skip `golines` when only `gofmt`, `gofumpt`, and logical blank lines are wanted:

```sh
go-format --write --skip-golines ./...
```

Format editor input from stdin:

```sh
go-format --stdin --stdin-path internal/cli/cli.go < internal/cli/cli.go
```

List files that would change without printing diffs:

```sh
go-format --check --list --diff=false ./...
```

Disable progress output for quiet scripts:

```sh
go-format --check --progress=false ./...
```

Print the bundled formatter versions:

```sh
go-format --version
```

More examples and before/after comparisons are in [docs/examples.md](docs/examples.md).

## Behavior

`go-format` recursively discovers `.go` files under the provided paths and skips:

- generated files with `Code generated ... DO NOT EDIT`
- `.git`
- `vendor`
- `third_party`
- `node_modules`
- hidden directories unless `--include-hidden` is set

`--check` prints unified diffs and exits with status `1` when files need
formatting. Use `--diff=false` to suppress diffs and `--list` to print changed
file paths. `--write` rewrites files in place and can also use `--list`.
Path-based runs format files concurrently by default using `GOMAXPROCS`. Use
`--jobs 1` for sequential processing or `--jobs N` to set a fixed worker count.
Path-based runs print file progress to standard error; use `--progress=false` to
keep script logs quiet.

`--stdin` formats source from standard input and writes the formatted source to
standard output. It accepts `--stdin-path` so parse errors and formatter
subprocesses can use a meaningful file name.

The first run may download and build the pinned `golines` module into the local
Go module and user cache. Later runs reuse the cached `golines` binary. Use
`--skip-golines` for environments that must avoid that subprocess. Use
`--skip-readability` to disable only the logical blank-line pass.

## Release

Releases use semver tags:

```sh
git tag v0.1.0
git push origin v0.1.0
```
