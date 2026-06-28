package formatrunner

import (
	"bytes"
	"context"
	"fmt"
	goformat "go/format"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"github.com/pmezard/go-difflib/difflib"
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
	Jobs            int
	Diff            bool
	DiffMaxBytes    int
	IncludeHidden   bool
	Progress        func(ProgressEvent)

	golinesBinary   string
	golinesCacheDir string
}

type ProgressEvent struct {
	Current int
	Total   int
	File    string
	Done    bool
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
	if opts.Jobs < 0 {
		return Result{}, fmt.Errorf("jobs must be non-negative")
	}

	opts = normalizeOptions(opts)

	files, err := readability.CollectFiles(opts.Paths, opts.IncludeHidden)
	if err != nil {
		return Result{}, err
	}

	result := Result{CheckedFiles: files}

	if len(files) == 0 {
		return result, nil
	}

	if !opts.SkipGoLines && opts.golinesBinary == "" {
		golinesBinary, err := resolveGolinesBinary(opts)
		if err != nil {
			return result, err
		}

		opts.golinesBinary = golinesBinary
	}

	fileResults := runFileJobs(files, opts)

	for idx, fileResult := range fileResults {
		if !fileResult.processed {
			continue
		}

		file := files[idx]
		reportProgress(opts.Progress, ProgressEvent{
			Current: idx + 1,
			Total:   len(files),
			File:    file,
		})

		if fileResult.err != nil {
			return result, fileResult.err
		}

		result.Issues = append(result.Issues, fileResult.issues...)

		if fileResult.changed {
			result.ChangedFiles = append(result.ChangedFiles, file)
			result.Diff += fileResult.diff
		}
	}

	reportProgress(opts.Progress, ProgressEvent{
		Current: len(files),
		Total:   len(files),
		Done:    true,
	})

	sort.Strings(result.ChangedFiles)

	if opts.Mode == Check && len(result.ChangedFiles) > 0 {
		if opts.DiffMaxBytes > 0 && len(result.Diff) > opts.DiffMaxBytes {
			result.Diff = result.Diff[:opts.DiffMaxBytes] + "\n... diff truncated ...\n"
		}

		return result, CheckFailedError{ChangedFiles: result.ChangedFiles}
	}

	return result, nil
}

type fileRunResult struct {
	index     int
	processed bool
	changed   bool
	diff      string
	issues    []readability.Issue
	err       error
}

func runFileJobs(files []string, opts Options) []fileRunResult {
	if opts.Jobs <= 1 || len(files) <= 1 {
		results := make([]fileRunResult, len(files))

		for idx, file := range files {
			results[idx] = processFile(idx, file, opts)

			if results[idx].err != nil {
				break
			}
		}

		return results
	}

	workers := min(opts.Jobs, len(files))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobs := make(chan int)
	results := make(chan fileRunResult, len(files))

	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for idx := range jobs {
				result := processFile(idx, files[idx], opts)
				results <- result

				if result.err != nil {
					cancel()
				}
			}
		}()
	}

	go func() {
		defer close(jobs)

		for idx := range files {
			select {
			case <-ctx.Done():
				return
			case jobs <- idx:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	fileResults := make([]fileRunResult, len(files))

	for result := range results {
		fileResults[result.index] = result

		if result.err != nil {
			cancel()
		}
	}

	return fileResults
}

func processFile(index int, file string, opts Options) fileRunResult {
	result := fileRunResult{index: index, processed: true}

	original, err := os.ReadFile(file)
	if err != nil {
		result.err = fmt.Errorf("read %s: %w", file, err)

		return result
	}

	formatted, issues, err := FormatSource(file, original, opts)
	if err != nil {
		result.err = err

		return result
	}

	result.issues = issues

	if bytes.Equal(original, formatted) {
		return result
	}

	result.changed = true

	if opts.Mode == Write {
		if err := os.WriteFile(file, formatted, 0o644); err != nil {
			result.err = fmt.Errorf("write %s: %w", file, err)
		}

		return result
	}

	if opts.Diff {
		diff, err := unifiedDiff(file, original, formatted)
		if err != nil {
			result.err = err

			return result
		}

		result.diff = diff
	}

	return result
}

func reportProgress(progress func(ProgressEvent), event ProgressEvent) {
	if progress == nil || event.Total == 0 {
		return
	}

	progress(event)
}

func FormatSource(path string, src []byte, opts Options) ([]byte, []readability.Issue, error) {
	opts = normalizeOptions(opts)

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

	if opts.Jobs == 0 {
		opts.Jobs = runtime.GOMAXPROCS(0)
	}

	return opts
}

func runGolines(path string, src []byte, opts Options) ([]byte, error) {
	golinesBinary := opts.golinesBinary

	if golinesBinary == "" {
		var err error
		golinesBinary, err = resolveGolinesBinary(opts)
		if err != nil {
			return nil, err
		}
	}

	args := []string{
		"--max-len", fmt.Sprintf("%d", opts.MaxLen),
		"--base-formatter", "gofmt",
	}
	cmd := exec.Command(golinesBinary, args...)
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN="+opts.GoToolchain)
	cmd.Stdin = bytes.NewReader(src)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("golines %s: %w\n%s", path, err, stderr.String())
	}

	return stdout.Bytes(), nil
}

func resolveGolinesBinary(opts Options) (string, error) {
	if opts.golinesBinary != "" {
		return opts.golinesBinary, nil
	}

	cacheRoot := opts.golinesCacheDir

	if cacheRoot == "" {
		userCacheDir, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("find user cache dir: %w", err)
		}

		cacheRoot = filepath.Join(userCacheDir, "go-format")
	}

	binDir := filepath.Join(cacheRoot, "golines", GolinesVersion)
	binPath := filepath.Join(binDir, golinesBinaryName())

	if isExecutable(binPath) {
		return binPath, nil
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create golines cache dir: %w", err)
	}

	tmpDir, err := os.MkdirTemp(binDir, "build-*")
	if err != nil {
		return "", fmt.Errorf("create golines build dir: %w", err)
	}

	defer os.RemoveAll(tmpDir)

	args := []string{"install", "github.com/segmentio/golines@" + GolinesVersion}
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOBIN="+tmpDir, "GOTOOLCHAIN="+opts.GoToolchain)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("install golines %s: %w\n%s", GolinesVersion, err, output)
	}

	tmpBin := filepath.Join(tmpDir, golinesBinaryName())

	if err := os.Rename(tmpBin, binPath); err != nil {
		if isExecutable(binPath) {
			return binPath, nil
		}

		return "", fmt.Errorf("cache golines binary: %w", err)
	}

	return binPath, nil
}

func golinesBinaryName() string {
	if runtime.GOOS == "windows" {
		return "golines.exe"
	}

	return "golines"
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return !info.IsDir() && info.Mode()&0o111 != 0
}

func unifiedDiff(path string, before []byte, after []byte) (string, error) {
	return difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(before)),
		B:        difflib.SplitLines(string(after)),
		FromFile: path,
		ToFile:   path + " (formatted)",
		Context:  3,
	})
}
