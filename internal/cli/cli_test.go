package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
