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
- optional non-mutating optimization advice

The blank-line pass separates guard clauses, validation and normalization,
setup/defer cleanup blocks, decision branches, lock groups, loops/switches, and
final returns. It keeps coupled `value, err := call()` / `if err != nil` pairs
together and keeps leading comments attached to the statement they describe.
It also separates top-level declaration groups, receiver groups, field groups,
large switch cases, and test flow blocks. It does not insert blank lines between
every statement.

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

Print non-mutating optimization advice:

```sh
go-format --advice ./...
```

Fail CI when advice issues are found:

```sh
go-format --check --advice-fail ./...
```

Create a project config:

```sh
go-format --init
```

Print the effective config after discovery and CLI overrides:

```sh
go-format --print-config
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

`--advice` prints non-mutating optimization findings to standard error. Without
an explicit `--write`, advice runs in check mode and does not rewrite files.
Advice includes struct padding opportunities, inconsistent receiver names,
misplaced `context.Context`, missed `%w` error wrapping, `defer` inside loops,
function-local `regexp.MustCompile`, append-in-loop preallocation candidates,
`strings.Builder` literals without `Grow`, missing GoDoc on exported symbols,
and TODO format checks. Use `--advice-fail` to exit with status `1` when advice
issues are found.

## Configuration

`go-format` discovers `.go-format.yml` from the current directory upward. CLI
flags override discovered config values, and `--no-config` disables discovery
for one run.

```yaml
max_len: 120
skip_golines: false
skip_readability: false
advice: false
advice_fail: false
include_hidden: false
go_toolchain: local
exclude: []
```

Use `--config path/to/.go-format.yml` for an explicit config path. `exclude`
accepts simple filepath-style patterns relative to each formatted root, for
example `ignored/**` or `*.pb.go`.

## CI and Editors

GitHub Actions can run the formatter directly from the module:

```yaml
- name: Format check
  run: go run github.com/stremovskyy/go-format@v0.1.0 --check ./...
```

For this repository, CI uses the checked-out command:

```yaml
- name: Format check
  run: go run . --check --progress=false ./...
```

A minimal pre-commit hook can format staged Go changes before commit:

```sh
#!/bin/sh
go-format --write ./...
git add $(git ls-files '*.go')
```

Editors can format buffers through stdin without touching files directly:

```sh
go-format --stdin --stdin-path "$FILE" < "$FILE"
```

## Release

Releases use semver tags:

```sh
git tag v0.1.0
git push origin v0.1.0
```
