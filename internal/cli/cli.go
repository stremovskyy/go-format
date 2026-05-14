package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/stremovskyy/go-format/internal/formatrunner"
)

var Version = "dev"

const Usage = `go-format formats Go source using gofmt, golines, gofumpt, and logical blank-line rules.

Usage:
  go-format --write [--list] [--max-len N] [path ...]
  go-format --check [--list] [--diff=false] [--max-len N] [path ...]
  go-format --stdin [--stdin-path file.go]
  go-format --version

Install:
  go install github.com/stremovskyy/go-format@latest

Flags:
`

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	return RunWithIO(args, os.Stdin, stdout, stderr)
}

func RunWithIO(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("go-format", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, Usage)
		fs.PrintDefaults()
	}

	check := fs.Bool("check", false, "check formatting without writing files")
	write := fs.Bool("write", false, "write formatted files")
	printDiff := fs.Bool("diff", true, "print unified diffs in check mode")
	list := fs.Bool("list", false, "print changed file paths")
	maxLen := fs.Int("max-len", 120, "target maximum line length for golines")
	goToolchain := fs.String(
		"go-toolchain",
		envDefault("GO_TOOLCHAIN", "local"),
		"GOTOOLCHAIN policy used when invoking Go-based formatters",
	)
	skipGolines := fs.Bool("skip-golines", false, "skip golines wrapping")
	skipReadability := fs.Bool("skip-readability", false, "skip logical blank-line formatting")
	includeHidden := fs.Bool("include-hidden", false, "include hidden directories other than .git")
	stdinMode := fs.Bool("stdin", false, "format Go source from stdin and write to stdout")
	stdinPath := fs.String("stdin-path", "stdin.go", "display path used for stdin parsing and diagnostics")
	version := fs.Bool("version", false, "print formatter versions")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		return 2
	}

	if *version {
		fmt.Fprint(stdout, versionOutput())

		return 0
	}

	if *check && *write {
		fmt.Fprintln(stderr, "--check and --write are mutually exclusive")

		return 2
	}

	if *stdinMode {
		if len(fs.Args()) > 0 {
			fmt.Fprintln(stderr, "--stdin does not accept path arguments")

			return 2
		}

		if *check || *write || *list {
			fmt.Fprintln(stderr, "--stdin cannot be combined with --check, --write, or --list")

			return 2
		}

		if *stdinPath == "" {
			fmt.Fprintln(stderr, "--stdin-path must not be empty")

			return 2
		}

		src, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "go-format: read stdin: %v\n", err)

			return 1
		}

		formatted, _, err := formatrunner.FormatSource(*stdinPath, src, formatrunner.Options{
			MaxLen:          *maxLen,
			GoToolchain:     *goToolchain,
			SkipGoLines:     *skipGolines,
			SkipReadability: *skipReadability,
		})
		if err != nil {
			fmt.Fprintf(stderr, "go-format: %v\n", err)

			return 1
		}

		if _, err := stdout.Write(formatted); err != nil {
			fmt.Fprintf(stderr, "go-format: write stdout: %v\n", err)

			return 1
		}

		return 0
	}

	mode := formatrunner.Write
	if *check {
		mode = formatrunner.Check
	}

	result, err := formatrunner.Run(formatrunner.Options{
		Mode:            mode,
		Paths:           fs.Args(),
		MaxLen:          *maxLen,
		GoToolchain:     *goToolchain,
		SkipGoLines:     *skipGolines,
		SkipReadability: *skipReadability,
		IncludeHidden:   *includeHidden,
		DiffMaxBytes:    8 << 20,
	})

	if *printDiff && len(result.Diff) > 0 {
		fmt.Fprint(stdout, result.Diff)
	}

	if *list {
		printFiles(stdout, result.ChangedFiles)
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

func printFiles(stdout io.Writer, files []string) {
	for _, file := range files {
		fmt.Fprintln(stdout, file)
	}
}

func envDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}

func versionOutput() string {
	version := Version
	if info, ok := debug.ReadBuildInfo(); ok && version == "dev" {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}

	return fmt.Sprintf(
		"go-format %s\ngolines: %s\ngofumpt: %s\n",
		version,
		formatrunner.GolinesVersion,
		moduleVersion("mvdan.cc/gofumpt"),
	)
}

func moduleVersion(path string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	for _, dep := range info.Deps {
		if dep.Path != path {
			continue
		}

		if dep.Replace != nil && dep.Replace.Version != "" {
			return dep.Replace.Version
		}

		if dep.Version != "" {
			return dep.Version
		}

		return "unknown"
	}

	return "unknown"
}
