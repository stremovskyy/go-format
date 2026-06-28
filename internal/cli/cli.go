package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/stremovskyy/go-format/internal/advice"
	appconfig "github.com/stremovskyy/go-format/internal/config"
	"github.com/stremovskyy/go-format/internal/formatrunner"
	"github.com/stremovskyy/go-format/internal/readability"
)

var Version = "dev"

const Usage = `go-format formats Go source using gofmt, golines, gofumpt, and logical blank-line rules.

Usage:
  go-format --write [--config path] [--no-config] [--list] [--jobs N] [--max-len N] [path ...]
  go-format --check [--config path] [--no-config] [--list] [--diff=false] [--jobs N] [--max-len N] [path ...]
  go-format --stdin [--config path] [--no-config] [--stdin-path file.go]
  go-format --advice [--advice-fail] [path ...]
  go-format --init [--config path]
  go-format --print-config [--config path]
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
	defaults := appconfig.Defaults()
	defaults.GoToolchain = envDefault("GO_TOOLCHAIN", defaults.GoToolchain)
	maxLen := fs.Int("max-len", defaults.MaxLen, "target maximum line length for golines")
	jobs := fs.Int("jobs", 0, "number of files to format concurrently; 0 uses GOMAXPROCS")
	goToolchain := fs.String(
		"go-toolchain",
		defaults.GoToolchain,
		"GOTOOLCHAIN policy used when invoking Go-based formatters",
	)
	skipGolines := fs.Bool("skip-golines", false, "skip golines wrapping")
	skipReadability := fs.Bool("skip-readability", false, "skip logical blank-line formatting")
	adviceFlag := fs.Bool("advice", false, "print non-mutating optimization advice")
	adviceFailFlag := fs.Bool("advice-fail", false, "exit with status 1 when advice issues are found")
	includeHidden := fs.Bool("include-hidden", false, "include hidden directories other than .git")
	progress := fs.Bool("progress", true, "print file progress to stderr")
	stdinMode := fs.Bool("stdin", false, "format Go source from stdin and write to stdout")
	stdinPath := fs.String("stdin-path", "stdin.go", "display path used for stdin parsing and diagnostics")
	configPath := fs.String("config", "", "path to .go-format.yml")
	noConfig := fs.Bool("no-config", false, "disable .go-format.yml discovery")
	initConfig := fs.Bool("init", false, "create a default .go-format.yml and exit")
	printConfig := fs.Bool("print-config", false, "print the effective configuration and exit")
	version := fs.Bool("version", false, "print formatter versions")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		return 2
	}

	setFlags := visitedFlags(fs)

	if *jobs < 0 {
		fmt.Fprintln(stderr, "--jobs must be non-negative")

		return 2
	}

	if *version {
		fmt.Fprint(stdout, versionOutput())

		return 0
	}

	if *configPath != "" && *noConfig {
		fmt.Fprintln(stderr, "--config and --no-config are mutually exclusive")

		return 2
	}

	if *check && *write {
		fmt.Fprintln(stderr, "--check and --write are mutually exclusive")

		return 2
	}

	initTarget := *configPath

	if initTarget == "" {
		initTarget = appconfig.DefaultFileName
	}

	if *initConfig {
		if len(fs.Args()) > 0 {
			fmt.Fprintln(stderr, "--init does not accept path arguments")

			return 2
		}

		cfg := defaults
		applyConfigOverrides(
			&cfg,
			setFlags,
			*maxLen,
			*goToolchain,
			*skipGolines,
			*skipReadability,
			*adviceFlag,
			*adviceFailFlag,
			*includeHidden,
		)
		normalizeConfig(&cfg)

		if err := appconfig.WriteFile(initTarget, cfg); err != nil {
			fmt.Fprintf(stderr, "go-format: %v\n", err)

			return 1
		}

		fmt.Fprintf(stdout, "go-format: created %s\n", initTarget)

		return 0
	}

	cfg, err := loadConfig(*configPath, *noConfig, defaults)
	if err != nil {
		fmt.Fprintf(stderr, "go-format: %v\n", err)

		return 1
	}

	applyConfigOverrides(
		&cfg,
		setFlags,
		*maxLen,
		*goToolchain,
		*skipGolines,
		*skipReadability,
		*adviceFlag,
		*adviceFailFlag,
		*includeHidden,
	)
	normalizeConfig(&cfg)

	if *printConfig {
		body, err := appconfig.Marshal(cfg)
		if err != nil {
			fmt.Fprintf(stderr, "go-format: marshal config: %v\n", err)

			return 1
		}

		if _, err := stdout.Write(body); err != nil {
			fmt.Fprintf(stderr, "go-format: write stdout: %v\n", err)

			return 1
		}

		return 0
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
			MaxLen:          cfg.MaxLen,
			GoToolchain:     cfg.GoToolchain,
			SkipGoLines:     cfg.SkipGoLines,
			SkipReadability: cfg.SkipReadability,
		})
		if err != nil {
			fmt.Fprintf(stderr, "go-format: %v\n", err)

			return 1
		}

		if _, err := stdout.Write(formatted); err != nil {
			fmt.Fprintf(stderr, "go-format: write stdout: %v\n", err)

			return 1
		}

		if cfg.Advice {
			issues, err := advice.Analyze(*stdinPath, formatted)
			if err != nil {
				fmt.Fprintf(stderr, "go-format: %v\n", err)

				return 1
			}

			printIssues(stderr, adviceOnly(issues))

			if cfg.AdviceFail && len(adviceOnly(issues)) > 0 {
				fmt.Fprintf(stderr, "go-format: %d advice issue(s) found\n", len(adviceOnly(issues)))

				return 1
			}
		}

		return 0
	}

	mode := formatrunner.Write

	if *check || (cfg.Advice && !*write) {
		mode = formatrunner.Check
	}

	var progressFunc func(formatrunner.ProgressEvent)
	var reporter *progressReporter

	if *progress {
		reporter = newProgressReporter(stderr)
		progressFunc = reporter.update
		defer reporter.close()
	}

	result, err := formatrunner.Run(formatrunner.Options{
		Mode:            mode,
		Paths:           fs.Args(),
		MaxLen:          cfg.MaxLen,
		GoToolchain:     cfg.GoToolchain,
		Jobs:            *jobs,
		SkipGoLines:     cfg.SkipGoLines,
		SkipReadability: cfg.SkipReadability,
		Advice:          cfg.Advice,
		AdviceFail:      cfg.AdviceFail,
		IncludeHidden:   cfg.IncludeHidden,
		Exclude:         cfg.Exclude,
		Diff:            *printDiff,
		DiffMaxBytes:    8 << 20,
		Progress:        progressFunc,
	})

	if *printDiff && len(result.Diff) > 0 {
		fmt.Fprint(stdout, result.Diff)
	}

	if *list {
		printFiles(stdout, result.ChangedFiles)
	}

	printIssues(stderr, adviceOnly(result.Issues))

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

		var adviceErr formatrunner.AdviceFailedError

		if errors.As(err, &adviceErr) {
			fmt.Fprintf(stderr, "go-format: %d advice issue(s) found\n", len(adviceErr.Issues))

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

type progressReporter struct {
	writer  io.Writer
	lastLen int
}

func newProgressReporter(writer io.Writer) *progressReporter {
	return &progressReporter{writer: writer}
}

func (reporter *progressReporter) update(event formatrunner.ProgressEvent) {
	if reporter == nil || reporter.writer == nil || event.Total == 0 {
		return
	}

	completed := event.Current - 1
	label := event.File

	if event.Done {
		completed = event.Current
		label = "done"
	}

	line := fmt.Sprintf(
		"go-format: [%s] %d/%d %s",
		progressBar(completed, event.Total, 24),
		event.Current,
		event.Total,
		label,
	)

	padding := ""

	if reporter.lastLen > len(line) {
		padding = strings.Repeat(" ", reporter.lastLen-len(line))
	}

	fmt.Fprintf(reporter.writer, "\r%s%s", line, padding)
	reporter.lastLen = len(line)

	if event.Done {
		fmt.Fprintln(reporter.writer)
		reporter.lastLen = 0
	}
}

func (reporter *progressReporter) close() {
	if reporter == nil || reporter.writer == nil || reporter.lastLen == 0 {
		return
	}

	fmt.Fprintln(reporter.writer)
	reporter.lastLen = 0
}

func progressBar(completed int, total int, width int) string {
	if total <= 0 || width <= 0 {
		return ""
	}

	completed = min(max(completed, 0), total)
	filled := completed * width / total

	return strings.Repeat("=", filled) + strings.Repeat("-", width-filled)
}

func printFiles(stdout io.Writer, files []string) {
	for _, file := range files {
		fmt.Fprintln(stdout, file)
	}
}

func printIssues(stderr io.Writer, issues []readability.Issue) {
	for _, issue := range issues {
		fmt.Fprintf(stderr, "%s:%d: %s: %s\n", issue.File, issue.Line, issue.Rule, issue.Message)
	}
}

func adviceOnly(issues []readability.Issue) []readability.Issue {
	filtered := make([]readability.Issue, 0, len(issues))

	for _, issue := range issues {
		if !issue.Fixable {
			filtered = append(filtered, issue)
		}
	}

	return filtered
}

func visitedFlags(fs *flag.FlagSet) map[string]bool {
	flags := map[string]bool{}
	fs.Visit(func(flag *flag.Flag) {
		flags[flag.Name] = true
	})

	return flags
}

func loadConfig(path string, noConfig bool, defaults appconfig.Config) (appconfig.Config, error) {
	if noConfig {
		return defaults, nil
	}

	if path != "" {
		return appconfig.LoadFile(path, defaults)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return appconfig.Config{}, fmt.Errorf("find current directory: %w", err)
	}

	discovered, ok, err := appconfig.Discover(cwd)
	if err != nil {
		return appconfig.Config{}, err
	}

	if !ok {
		return defaults, nil
	}

	return appconfig.LoadFile(discovered, defaults)
}

func applyConfigOverrides(
	cfg *appconfig.Config,
	setFlags map[string]bool,
	maxLen int,
	goToolchain string,
	skipGolines bool,
	skipReadability bool,
	advice bool,
	adviceFail bool,
	includeHidden bool,
) {
	if setFlags["max-len"] {
		cfg.MaxLen = maxLen
	}

	if setFlags["go-toolchain"] {
		cfg.GoToolchain = goToolchain
	}

	if setFlags["skip-golines"] {
		cfg.SkipGoLines = skipGolines
	}

	if setFlags["skip-readability"] {
		cfg.SkipReadability = skipReadability
	}

	if setFlags["advice"] {
		cfg.Advice = advice
	}

	if setFlags["advice-fail"] {
		cfg.AdviceFail = adviceFail
	}

	if setFlags["include-hidden"] {
		cfg.IncludeHidden = includeHidden
	}
}

func normalizeConfig(cfg *appconfig.Config) {
	if cfg.AdviceFail {
		cfg.Advice = true
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
