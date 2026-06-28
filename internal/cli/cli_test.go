package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stremovskyy/go-format/internal/formatrunner"
)

func TestRunVersionPrintsFormatterVersions(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithIO([]string{"--version"}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("RunWithIO(--version) code = %d, want 0\nstderr:\n%s", code, stderr.String())
	}

	output := stdout.String()

	for _, expected := range []string{
		"go-format",
		"golines: v0.13.0",
		"gofumpt:",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("version output missing %q:\n%s", expected, output)
		}
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunFormatsStdin(t *testing.T) {
	input := `package sample

func active(enabled bool) bool {
	if !enabled {
		return false
	}
	return true
}
`

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithIO(
		[]string{"--stdin", "--stdin-path", "input.go", "--skip-golines"},
		strings.NewReader(input),
		&stdout,
		&stderr,
	)

	if code != 0 {
		t.Fatalf("RunWithIO(--stdin) code = %d, want 0\nstderr:\n%s", code, stderr.String())
	}

	if !strings.Contains(stdout.String(), "return false\n\t}\n\n\treturn true") {
		t.Fatalf("stdin output missing readability blank line:\n%s", stdout.String())
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunRejectsStdinWithPathArguments(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithIO([]string{"--stdin", "."}, strings.NewReader(""), &stdout, &stderr)

	if code != 2 {
		t.Fatalf("RunWithIO(--stdin .) code = %d, want 2", code)
	}

	if !strings.Contains(stderr.String(), "--stdin does not accept path arguments") {
		t.Fatalf("stderr missing stdin/path error:\n%s", stderr.String())
	}

	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunRejectsNegativeJobs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithIO([]string{"--check", "--jobs", "-1", "."}, strings.NewReader(""), &stdout, &stderr)

	if code != 2 {
		t.Fatalf("RunWithIO(--jobs -1) code = %d, want 2", code)
	}

	if !strings.Contains(stderr.String(), "--jobs must be non-negative") {
		t.Fatalf("stderr missing jobs validation error:\n%s", stderr.String())
	}

	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunListChangedFilesWithoutDiff(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")

	if err := os.WriteFile(file, []byte(`package sample

func active(enabled bool) bool {
	if !enabled {
		return false
	}
	return true
}
`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithIO(
		[]string{"--check", "--skip-golines", "--diff=false", "--list", root},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)

	if code != 1 {
		t.Fatalf("RunWithIO(--check --list) code = %d, want 1\nstderr:\n%s", code, stderr.String())
	}

	if strings.Contains(stdout.String(), "--- ") || strings.Contains(stdout.String(), "+++ ") {
		t.Fatalf("stdout contains unified diff despite --diff=false:\n%s", stdout.String())
	}

	if !strings.Contains(stdout.String(), file) {
		t.Fatalf("stdout missing changed file %q:\n%s", file, stdout.String())
	}
}

func TestRunCanDisableProgressOutput(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")

	if err := os.WriteFile(file, []byte(`package sample

func active() bool {
	return true
}
`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithIO(
		[]string{"--check", "--skip-golines", "--progress=false", root},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)

	if code != 0 {
		t.Fatalf("RunWithIO(--progress=false) code = %d, want 0\nstderr:\n%s", code, stderr.String())
	}

	if !strings.Contains(stdout.String(), "go-format: 1 Go file(s) checked; no changes needed") {
		t.Fatalf("stdout missing success summary:\n%s", stdout.String())
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunLoadsConfigFromCurrentDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")

	if err := os.WriteFile(filepath.Join(root, ".go-format.yml"), []byte("skip_readability: true\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := os.WriteFile(file, []byte(`package sample

func active(enabled bool) bool {
	if !enabled {
		return false
	}
	return true
}
`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir %s: %v", root, err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithIO(
		[]string{"--check", "--skip-golines", "--progress=false", "."},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)

	if code != 0 {
		t.Fatalf(
			"RunWithIO with config code = %d, want 0\nstdout:\n%s\nstderr:\n%s",
			code,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestRunCLIFlagsOverrideConfig(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, ".go-format.yml"), []byte("skip_readability: true\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(`package sample

func active(enabled bool) bool {
	if !enabled {
		return false
	}
	return true
}
`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir %s: %v", root, err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithIO(
		[]string{"--check", "--skip-golines", "--skip-readability=false", "--progress=false", "."},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)

	if code != 1 {
		t.Fatalf(
			"RunWithIO override code = %d, want check failure\nstdout:\n%s\nstderr:\n%s",
			code,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestRunCanIgnoreDiscoveredConfig(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, ".go-format.yml"), []byte("skip_readability: true\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(`package sample

func active(enabled bool) bool {
	if !enabled {
		return false
	}
	return true
}
`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir %s: %v", root, err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithIO(
		[]string{"--check", "--skip-golines", "--no-config", "--progress=false", "."},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)

	if code != 1 {
		t.Fatalf(
			"RunWithIO --no-config code = %d, want check failure\nstdout:\n%s\nstderr:\n%s",
			code,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestRunPrintsEffectiveConfig(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, ".go-format.yml"), []byte(`max_len: 100
skip_readability: true
exclude:
  - ignored/**
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir %s: %v", root, err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithIO(
		[]string{"--print-config", "--skip-golines"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)

	if code != 0 {
		t.Fatalf("RunWithIO --print-config code = %d, want 0\nstderr:\n%s", code, stderr.String())
	}

	for _, expected := range []string{
		"max_len: 100",
		"skip_golines: true",
		"skip_readability: true",
		"exclude:",
		"  - ignored/**",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("print config output missing %q:\n%s", expected, stdout.String())
		}
	}
}

func TestRunInitWritesDefaultConfig(t *testing.T) {
	root := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir %s: %v", root, err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithIO([]string{"--init"}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("RunWithIO --init code = %d, want 0\nstderr:\n%s", code, stderr.String())
	}

	body, err := os.ReadFile(filepath.Join(root, ".go-format.yml"))
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}

	for _, expected := range []string{
		"max_len: 120",
		"skip_golines: false",
		"skip_readability: false",
		"include_hidden: false",
		"go_toolchain: local",
		"exclude: []",
	} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("generated config missing %q:\n%s", expected, body)
		}
	}

	if !strings.Contains(stdout.String(), "created .go-format.yml") {
		t.Fatalf("stdout missing init message:\n%s", stdout.String())
	}
}

func TestProgressReporterWritesProgressBarUpdates(t *testing.T) {
	var stderr bytes.Buffer

	reporter := newProgressReporter(&stderr)
	reporter.update(formatrunner.ProgressEvent{Current: 1, Total: 2, File: "a.go"})
	reporter.update(formatrunner.ProgressEvent{Current: 2, Total: 2, File: "b.go"})
	reporter.update(formatrunner.ProgressEvent{Current: 2, Total: 2, Done: true})

	output := stderr.String()

	for _, expected := range []string{
		"\rgo-format: [",
		"1/2 a.go",
		"2/2 b.go",
		"2/2 done",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("progress output missing %q:\n%s", expected, output)
		}
	}

	if !strings.HasSuffix(output, "\n") {
		t.Fatalf("progress output = %q, want trailing newline after done", output)
	}
}
