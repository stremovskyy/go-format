package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIHelpMentionsInstallableCommand(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . --help error = %v\n%s", err, output)
	}

	for _, expected := range []string{
		"go-format",
		"--check",
		"--write",
		"--max-len",
		"--stdin",
		"--list",
		"--diff",
		"--version",
	} {
		if !strings.Contains(string(output), expected) {
			t.Fatalf("help output missing %q:\n%s", expected, output)
		}
	}
}

func TestCLIFormatsFixture(t *testing.T) {
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

	checkCmd := exec.Command("go", "run", ".", "--check", "--skip-golines", root)
	checkOutput, err := checkCmd.CombinedOutput()
	if err == nil {
		t.Fatalf("go-format --check unexpectedly passed:\n%s", checkOutput)
	}

	writeCmd := exec.Command("go", "run", ".", "--write", "--skip-golines", root)
	writeOutput, err := writeCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go-format --write error = %v\n%s", err, writeOutput)
	}

	verifyCmd := exec.Command("go", "run", ".", "--check", "--skip-golines", root)
	verifyOutput, err := verifyCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go-format --check after write error = %v\n%s", err, verifyOutput)
	}
}
