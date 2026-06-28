package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const DefaultFileName = ".go-format.yml"

type Config struct {
	MaxLen          int      `yaml:"max_len"`
	SkipGoLines     bool     `yaml:"skip_golines"`
	SkipReadability bool     `yaml:"skip_readability"`
	Advice          bool     `yaml:"advice"`
	AdviceFail      bool     `yaml:"advice_fail"`
	Mutate          bool     `yaml:"mutate"`
	IncludeHidden   bool     `yaml:"include_hidden"`
	GoToolchain     string   `yaml:"go_toolchain"`
	Exclude         []string `yaml:"exclude"`
}

type fileConfig struct {
	MaxLen          *int     `yaml:"max_len"`
	SkipGoLines     *bool    `yaml:"skip_golines"`
	SkipReadability *bool    `yaml:"skip_readability"`
	Advice          *bool    `yaml:"advice"`
	AdviceFail      *bool    `yaml:"advice_fail"`
	Mutate          *bool    `yaml:"mutate"`
	IncludeHidden   *bool    `yaml:"include_hidden"`
	GoToolchain     *string  `yaml:"go_toolchain"`
	Exclude         []string `yaml:"exclude"`
}

func Defaults() Config {
	return Config{
		MaxLen:      120,
		GoToolchain: "local",
		Exclude:     []string{},
	}
}

func LoadFile(path string, base Config) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %s: %w", path, err)
	}

	defer file.Close()

	cfg, err := Decode(file, base)
	if err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}

	return cfg, nil
}

func Decode(reader io.Reader, base Config) (Config, error) {
	var raw fileConfig

	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)

	if err := decoder.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return base, nil
		}

		return Config{}, err
	}

	cfg := base

	if raw.MaxLen != nil {
		if *raw.MaxLen <= 0 {
			return Config{}, fmt.Errorf("max_len must be positive")
		}

		cfg.MaxLen = *raw.MaxLen
	}

	if raw.SkipGoLines != nil {
		cfg.SkipGoLines = *raw.SkipGoLines
	}

	if raw.SkipReadability != nil {
		cfg.SkipReadability = *raw.SkipReadability
	}

	if raw.Advice != nil {
		cfg.Advice = *raw.Advice
	}

	if raw.AdviceFail != nil {
		cfg.AdviceFail = *raw.AdviceFail
	}

	if raw.Mutate != nil {
		cfg.Mutate = *raw.Mutate
	}

	if raw.IncludeHidden != nil {
		cfg.IncludeHidden = *raw.IncludeHidden
	}

	if raw.GoToolchain != nil {
		if *raw.GoToolchain == "" {
			return Config{}, fmt.Errorf("go_toolchain must not be empty")
		}

		cfg.GoToolchain = *raw.GoToolchain
	}

	if raw.Exclude != nil {
		cfg.Exclude = append([]string(nil), raw.Exclude...)
	}

	return cfg, nil
}

func Discover(startDir string) (string, bool, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", false, fmt.Errorf("resolve config search dir: %w", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return "", false, fmt.Errorf("stat config search dir %s: %w", dir, err)
	}

	if !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	for {
		candidate := filepath.Join(dir, DefaultFileName)
		info, err := os.Stat(candidate)

		if err == nil && !info.IsDir() {
			return candidate, true, nil
		}

		if err != nil && !os.IsNotExist(err) {
			return "", false, fmt.Errorf("stat config %s: %w", candidate, err)
		}

		parent := filepath.Dir(dir)

		if parent == dir {
			return "", false, nil
		}

		dir = parent
	}
}

func Marshal(cfg Config) ([]byte, error) {
	if cfg.Exclude == nil {
		cfg.Exclude = []string{}
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	return out, nil
}

func WriteFile(path string, cfg Config) error {
	body, err := Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create config %s: %w", path, err)
	}

	defer file.Close()

	if _, err := io.Copy(file, bytes.NewReader(body)); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}

	return nil
}
