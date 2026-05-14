package formatrunner

import (
	"bytes"
	"errors"
	"fmt"
	goformat "go/format"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stremovskyy/go-format/internal/readability"
	gofumpt "mvdan.cc/gofumpt/format"
)

const GolinesVersion = "v0.13.0"

type Mode string

const (
	Check Mode = "check"
	Write Mode = "write"
)

type Options struct {
	Mode            Mode
	Paths           []string
	MaxLen          int
	GoToolchain     string
	SkipGoLines     bool
	SkipReadability bool
	DiffMaxBytes    int
	IncludeHidden   bool
}

type Result struct {
	CheckedFiles []string
	ChangedFiles []string
	Diff         string
	Issues       []readability.Issue
}

type CheckFailedError struct {
	ChangedFiles []string
}

func (err CheckFailedError) Error() string {
	return fmt.Sprintf("go-format check failed: %d file(s) need formatting", len(err.ChangedFiles))
}

func Run(opts Options) (Result, error) {
	opts = normalizeOptions(opts)

	files, err := readability.CollectFiles(opts.Paths, opts.IncludeHidden)
	if err != nil {
		return Result{}, err
	}

	result := Result{CheckedFiles: files}

	for _, file := range files {
		original, err := os.ReadFile(file)
		if err != nil {
			return result, fmt.Errorf("read %s: %w", file, err)
		}

		formatted, issues, err := FormatSource(file, original, opts)
		if err != nil {
			return result, err
		}

		result.Issues = append(result.Issues, issues...)

		if bytes.Equal(original, formatted) {
			continue
		}

		result.ChangedFiles = append(result.ChangedFiles, file)
		if opts.Mode == Write {
			if err := os.WriteFile(file, formatted, 0o644); err != nil {
				return result, fmt.Errorf("write %s: %w", file, err)
			}

			continue
		}

		diff, err := unifiedDiff(file, original, formatted)
		if err != nil {
			return result, err
		}

		result.Diff += diff
	}

	sort.Strings(result.ChangedFiles)

	if opts.Mode == Check && len(result.ChangedFiles) > 0 {
		if opts.DiffMaxBytes > 0 && len(result.Diff) > opts.DiffMaxBytes {
			result.Diff = result.Diff[:opts.DiffMaxBytes] + "\n... diff truncated ...\n"
		}

		return result, CheckFailedError{ChangedFiles: result.ChangedFiles}
	}

	return result, nil
}

func FormatSource(path string, src []byte, opts Options) ([]byte, []readability.Issue, error) {
	formatted, err := goformat.Source(src)
	if err != nil {
		return nil, nil, fmt.Errorf("gofmt %s: %w", path, err)
	}

	if !opts.SkipGoLines {
		formatted, err = runGolines(path, formatted, opts)
		if err != nil {
			return nil, nil, err
		}
	}

	formatted, err = gofumpt.Source(formatted, gofumpt.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("gofumpt %s: %w", path, err)
	}

	var issues []readability.Issue

	if !opts.SkipReadability {
		formatted, issues, err = readability.Rewrite(path, formatted)
		if err != nil {
			return nil, nil, err
		}

		formatted, err = gofumpt.Source(formatted, gofumpt.Options{})
		if err != nil {
			return nil, nil, fmt.Errorf("gofumpt after readability %s: %w", path, err)
		}
	}

	return formatted, issues, nil
}

func normalizeOptions(opts Options) Options {
	if opts.Mode == "" {
		opts.Mode = Write
	}

	if len(opts.Paths) == 0 {
		opts.Paths = []string{"."}
	}

	if opts.MaxLen <= 0 {
		opts.MaxLen = 120
	}

	if opts.GoToolchain == "" {
		opts.GoToolchain = "local"
	}

	if opts.DiffMaxBytes <= 0 {
		opts.DiffMaxBytes = 4 << 20
	}

	return opts
}

func runGolines(path string, src []byte, opts Options) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "go-format-golines-*")
	if err != nil {
		return nil, fmt.Errorf("create golines temp dir: %w", err)
	}

	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, filepath.Base(path))
	if err := os.WriteFile(tmpFile, src, 0o644); err != nil {
		return nil, fmt.Errorf("write golines temp file: %w", err)
	}

	args := []string{
		"run",
		"github.com/segmentio/golines@" + GolinesVersion,
		"--max-len", fmt.Sprintf("%d", opts.MaxLen),
		"--base-formatter", "gofmt",
		"--write-output",
		tmpFile,
	}
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN="+opts.GoToolchain)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("golines %s: %w\n%s", path, err, output)
	}

	formatted, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("read golines temp file: %w", err)
	}

	return formatted, nil
}

func unifiedDiff(path string, before []byte, after []byte) (string, error) {
	tmpDir, err := os.MkdirTemp("", "go-format-diff-*")
	if err != nil {
		return "", fmt.Errorf("create diff temp dir: %w", err)
	}

	defer os.RemoveAll(tmpDir)

	beforePath := filepath.Join(tmpDir, "before.go")
	afterPath := filepath.Join(tmpDir, "after.go")
	if err := os.WriteFile(beforePath, before, 0o644); err != nil {
		return "", fmt.Errorf("write diff before: %w", err)
	}

	if err := os.WriteFile(afterPath, after, 0o644); err != nil {
		return "", fmt.Errorf("write diff after: %w", err)
	}

	cmd := exec.Command("diff", "-u", beforePath, afterPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return relabelDiff(string(output), beforePath, afterPath, path), nil
		}

		return "", fmt.Errorf("diff %s: %w\n%s", path, err, output)
	}

	return string(output), nil
}

func relabelDiff(diff string, beforePath string, afterPath string, displayPath string) string {
	diff = strings.Replace(diff, "--- "+beforePath, "--- "+displayPath, 1)
	diff = strings.Replace(diff, "+++ "+afterPath, "+++ "+displayPath+" (formatted)", 1)

	return diff
}
