# Examples

`go-format` is meant for Go projects that already trust `gofmt`, but want a
single command that also handles long lines, stricter formatting, and a small
readability pass for dense code.

The output is still ordinary Go source. The tool does not rewrite architecture,
rename symbols, reorder logic, or insert blank lines between every statement.

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
