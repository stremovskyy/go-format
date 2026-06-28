# Examples

`go-format` is meant for Go projects that already trust `gofmt`, but want a
single command that also handles long lines, stricter formatting, a readability
pass for dense code, optional safe source mutations, and non-mutating
optimization advice.

The output is still ordinary Go source. The tool does not rewrite architecture,
rename symbols, reorder logic, or insert blank lines between every statement.
Advice mode follows the same safety rule: it reports opportunities but does not
move symbols, split files, reorder fields, or change source bytes. `--mutate`
is the explicit opt-in path for conservative source rewrites.

## Common Commands

Format a repository in place:

```sh
go-format --write ./...
```

Check a repository in CI:

```sh
go-format --check ./...
```

Check changed files without printing full diffs:

```sh
go-format --check --list --diff=false ./...
```

Format a buffer from an editor:

```sh
go-format --stdin --stdin-path internal/cli/cli.go < internal/cli/cli.go
```

Run without the `golines` subprocess:

```sh
go-format --write --skip-golines ./...
```

Run without the logical blank-line pass:

```sh
go-format --write --skip-readability ./...
```

Print advice without failing:

```sh
go-format --advice ./...
```

Preview safe mutations without writing files:

```sh
go-format --check --mutate ./...
```

Apply safe mutations:

```sh
go-format --write --mutate ./...
```

Fail CI on advice issues:

```sh
go-format --check --advice-fail ./...
```

Create and inspect project defaults:

```sh
go-format --init
go-format --print-config
```

Show the formatter versions used by the binary:

```sh
go-format --version
```

## What Improves

### Dense Function Bodies

Plain `gofmt` keeps this function compact because the syntax is already valid:

```go
func active(enabled bool, count int) bool {
	if !enabled {
		return false
	}
	if count == 0 {
		count = 1
	}
	count++
	mu.RLock()
	defer mu.RUnlock()
	for i := 0; i < count; i++ {
		if i == count-1 {
			return true
		}
	}
	return false
}
```

`go-format` keeps the same logic, but separates the major blocks:

```go
func active(enabled bool, count int) bool {
	if !enabled {
		return false
	}

	if count == 0 {
		count = 1
	}

	count++

	mu.RLock()
	defer mu.RUnlock()

	for i := 0; i < count; i++ {
		if i == count-1 {
			return true
		}
	}

	return false
}
```

This is better when a function mixes guard clauses, normalization, setup,
locking, loops, and a final return. Each section becomes easier to scan.

### Safe Mutations

`--mutate` applies narrowly scoped source rewrites. It can add placeholder GoDoc
comments for undocumented exported top-level symbols, wrap simple
`fmt.Errorf("... %v", err)` calls with `%w`, and simplify direct bool returns:

```go
type Config struct{}

func load() error {
	if err := read(); err != nil {
		return fmt.Errorf("read: %v", err)
	}
	return nil
}

func enabled(ok bool) bool {
	if ok {
		return true
	}
	return false
}
```

With `--mutate`, that becomes:

```go
// Config ...
type Config struct{}

func load() error {
	if err := read(); err != nil {
		return fmt.Errorf("read: %w", err)
	}
	return nil
}

func enabled(ok bool) bool {
	return ok
}
```

Use `--check --mutate` first when adopting this in an existing repository.

### readability Logical Groups

The readability pass also keeps related statements together. A call that returns
an error stays next to its immediate error check, cleanup defers stay with their
setup, and comments remain attached to the statement they explain:

```go
func build(ctx context.Context, raw string) (Result, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Result{}, errors.New("empty")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	values, err := fetch(ctx, raw)
	if err != nil {
		return Result{}, err
	}
	// Persist only validated results.
	result := Result{Name: raw, Values: values}
	if len(result.Values) == 0 {
		result.Reason = "empty"
	}
	return result, nil
}
```

`go-format` turns that into:

```go
func build(ctx context.Context, raw string) (Result, error) {
	raw = strings.TrimSpace(raw)

	if raw == "" {
		return Result{}, errors.New("empty")
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	values, err := fetch(ctx, raw)
	if err != nil {
		return Result{}, err
	}

	// Persist only validated results.
	result := Result{Name: raw, Values: values}

	if len(result.Values) == 0 {
		result.Reason = "empty"
	}

	return result, nil
}
```

This is intentionally semantic rather than purely vertical: the formatter adds
space around changes in intent without splitting the error check from the call
that produced the error.

### Long Lines

Plain `gofmt` does not wrap long composite literals just because they cross a
line-length target:

```go
values := map[string]string{"first_key": "first_value", "second_key": "second_value", "third_key": "third_value", "fourth_key": "fourth_value"}
```

With `golines` enabled, `go-format` wraps those lines around the configured
target length:

```go
values := map[string]string{
	"first_key":  "first_value",
	"second_key": "second_value",
	"third_key":  "third_value",
	"fourth_key": "fourth_value",
}
```

This is better for reviews because diffs stop forcing horizontal scrolling.

### Non-Mutating Advice

Advice mode reports optimization and maintenance opportunities to standard
error. Without an explicit `--write`, it runs in check mode and does not change
files:

```sh
go-format --advice ./...
```

Example output is written to standard error:

```text
internal/service/config.go:12: struct-padding: struct Config may waste memory: field Count follows smaller field Flag
internal/service/config.go:24: receiver-name: receiver name for Config is inconsistent: saw c and cfg
internal/service/fetch.go:18: context-first: context.Context should be the first parameter
internal/service/fetch.go:20: error-wrap: fmt.Errorf should wrap err with %w instead of %v
internal/service/cache.go:31: defer-in-loop: defer inside loop may delay cleanup until function return
internal/service/cache.go:44: regexp-mustcompile: regexp.MustCompile inside function should be hoisted to package scope
internal/service/list.go:55: append-prealloc: append in loop to values may need preallocation
internal/service/render.go:67: builder-grow: strings.Builder b writes string literals without Grow
internal/service/public.go:3: todo-format: TODO should use format TODO(owner): text
internal/service/public.go:10: exported-doc: exported type Config should have a doc comment
```

Use `--advice-fail` in CI when these findings should fail the run:

```sh
go-format --check --advice-fail ./...
```

### CI Output

For local development, diffs are useful:

```sh
go-format --check ./...
```

For CI summaries, a file list is often enough:

```sh
go-format --check --list --diff=false ./...
```

Example output:

```text
internal/cli/cli.go
internal/formatrunner/formatrunner.go
go-format: 2 file(s) need formatting; run go-format --write
```

This is better for CI systems that already expose artifacts or annotations and
do not need large unified diffs in the main log.

## When To Disable Parts

Use `--skip-golines` when the environment cannot download or execute the pinned
`golines` module. The tool still runs `gofmt`, `gofumpt`, and readability rules.

Use `--skip-readability` when a repository wants only mechanical formatting and
line wrapping. This keeps blank-line decisions closer to plain `gofmt`.

Use plain `gofmt` when a project intentionally avoids opinionated wrapping or
vertical-spacing rules. `go-format` is best when a team wants that opinion
captured in one repeatable command.

## Repository Adoption

### Config

Commit `.go-format.yml` at the repository root when CI, editor integrations, and
local scripts should share the same defaults:

```yaml
max_len: 120
skip_golines: false
skip_readability: false
advice: false
advice_fail: false
include_hidden: false
go_toolchain: local
exclude:
  - ignored/**
  - '*.pb.go'
```

CLI flags override config values. For example, this uses the repository config
but disables the readability pass for one run:

```sh
go-format --check --skip-readability ./...
```

Use `--no-config` when validating behavior with only built-in defaults:

```sh
go-format --check --no-config ./...
```

### GitHub Actions

For a project consuming a tagged release:

```yaml
- name: Format check
  run: go run github.com/stremovskyy/go-format@v0.1.0 --check ./...
```

For this repository, use the checked-out command:

```yaml
- name: Format check
  run: go run . --check --progress=false ./...
```

### Pre-Commit

```sh
#!/bin/sh
set -eu

go-format --write ./...
git add $(git ls-files '*.go')
```

### Editors

Use stdin mode for editor format-on-save integrations so the editor owns the
buffer write:

```sh
go-format --stdin --stdin-path "$FILE" < "$FILE"
```
