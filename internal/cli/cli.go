package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/stremovskyy/go-format/internal/formatrunner"
)

const Usage = `go-format formats Go source using gofmt, golines, gofumpt, and logical blank-line rules.

Usage:
  go-format [--write|--check] [--max-len N] [path ...]

Install:
  go install github.com/stremovskyy/go-format@latest

Flags:
`

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("go-format", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, Usage)
		fs.PrintDefaults()
	}

	check := fs.Bool("check", false, "check formatting without writing files")
	write := fs.Bool("write", false, "write formatted files")
	maxLen := fs.Int("max-len", 120, "target maximum line length for golines")
	goToolchain := fs.String(
		"go-toolchain",
		envDefault("GO_TOOLCHAIN", "local"),
		"GOTOOLCHAIN policy used when invoking Go-based formatters",
	)
	skipGolines := fs.Bool("skip-golines", false, "skip golines wrapping")
	includeHidden := fs.Bool("include-hidden", false, "include hidden directories other than .git")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		return 2
	}

	if *check && *write {
		fmt.Fprintln(stderr, "--check and --write are mutually exclusive")

		return 2
	}

	mode := formatrunner.Write
	if *check {
		mode = formatrunner.Check
	}

	result, err := formatrunner.Run(formatrunner.Options{
		Mode:          mode,
		Paths:         fs.Args(),
		MaxLen:        *maxLen,
		GoToolchain:   *goToolchain,
		SkipGoLines:   *skipGolines,
		IncludeHidden: *includeHidden,
		DiffMaxBytes:  8 << 20,
	})

	if len(result.Diff) > 0 {
		fmt.Fprint(stdout, result.Diff)
	}

	if err != nil {
		var checkErr formatrunner.CheckFailedError
		if errors.As(err, &checkErr) {
			fmt.Fprintf(
				stderr,
				"go-format: %d file(s) need formatting; run go-format --write\n",
				len(checkErr.ChangedFiles),
			)

			return 1
		}

		fmt.Fprintf(stderr, "go-format: %v\n", err)

		return 1
	}

	if mode == formatrunner.Check {
		fmt.Fprintf(stdout, "go-format: %d Go file(s) checked; no changes needed\n", len(result.CheckedFiles))

		return 0
	}

	fmt.Fprintf(
		stdout,
		"go-format: %d Go file(s) checked; %d file(s) formatted\n",
		len(result.CheckedFiles),
		len(result.ChangedFiles),
	)

	return 0
}

func envDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}
