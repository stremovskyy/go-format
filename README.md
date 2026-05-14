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

The blank-line pass separates guard clauses, normalization/setup blocks,
lock/defer groups, loops/switches, and final returns. It does not insert blank
lines between every statement.

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

Skip `golines` when only `gofmt`, `gofumpt`, and logical blank lines are wanted:

```sh
go-format --write --skip-golines ./...
```

## Behavior

`go-format` recursively discovers `.go` files under the provided paths and skips:

- generated files with `Code generated ... DO NOT EDIT`
- `.git`
- `vendor`
- `third_party`
- `node_modules`
- hidden directories unless `--include-hidden` is set

`--check` prints unified diffs and exits with status `1` when files need
formatting. `--write` rewrites files in place.

The first run may download the pinned `golines` module into the local Go module
cache. Use `--skip-golines` for environments that must avoid that subprocess.

## Release

Releases use semver tags:

```sh
git tag v0.1.0
git push origin v0.1.0
```
