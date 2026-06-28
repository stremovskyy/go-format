package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadFileMergesYAMLWithDefaults(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".go-format.yml")

	if err := os.WriteFile(path, []byte(`max_len: 100
skip_golines: true
skip_readability: true
advice: true
advice_fail: true
mutate: true
include_hidden: true
go_toolchain: auto
exclude:
  - ignored/**
  - '*.pb.go'
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFile(path, Defaults())
	if err != nil {
		t.Fatalf("LoadFile error = %v", err)
	}

	if cfg.MaxLen != 100 {
		t.Fatalf("MaxLen = %d, want 100", cfg.MaxLen)
	}

	if !cfg.SkipGoLines || !cfg.SkipReadability || !cfg.Advice || !cfg.AdviceFail || !cfg.Mutate || !cfg.IncludeHidden {
		t.Fatalf("bool config not applied: %#v", cfg)
	}

	if cfg.GoToolchain != "auto" {
		t.Fatalf("GoToolchain = %q, want auto", cfg.GoToolchain)
	}

	if !reflect.DeepEqual(cfg.Exclude, []string{"ignored/**", "*.pb.go"}) {
		t.Fatalf("Exclude = %#v, want configured patterns", cfg.Exclude)
	}
}

func TestDiscoverFindsNearestParentConfig(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "pkg", "feature")

	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	rootConfig := filepath.Join(root, ".go-format.yml")
	nestedConfig := filepath.Join(root, "pkg", ".go-format.yml")

	for _, path := range []string{rootConfig, nestedConfig} {
		if err := os.WriteFile(path, []byte("max_len: 100\n"), 0o644); err != nil {
			t.Fatalf("write config %s: %v", path, err)
		}
	}

	found, ok, err := Discover(nested)
	if err != nil {
		t.Fatalf("Discover error = %v", err)
	}

	if !ok {
		t.Fatal("Discover ok = false, want true")
	}

	if found != nestedConfig {
		t.Fatalf("Discover = %q, want nearest parent %q", found, nestedConfig)
	}
}

func TestLoadFileRejectsUnknownFields(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".go-format.yml")

	if err := os.WriteFile(path, []byte("unknown: true\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadFile(path, Defaults())
	if err == nil {
		t.Fatal("LoadFile error = nil, want unknown field error")
	}

	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("LoadFile error = %v, want unknown field detail", err)
	}
}
