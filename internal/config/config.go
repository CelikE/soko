// Package config handles loading, saving, and path resolution for the soko
// global configuration file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ErrRepoAlreadyExists is returned when attempting to add a repo whose path
// is already registered in the config.
var ErrRepoAlreadyExists = errors.New("repo already exists")

// RepoEntry represents a single registered git repository.
type RepoEntry struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// Config is the top-level structure of the soko config file.
type Config struct {
	Repos []RepoEntry `yaml:"repos"`
}

// Path returns the absolute path to the soko config file. It respects
// $XDG_CONFIG_HOME if set, otherwise falls back to ~/.config/soko/config.yaml.
func Path() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "soko", "config.yaml"), nil
}

// Load reads and parses the config file. If the file does not exist, it
// returns an empty Config. It returns an error only if the file exists but
// cannot be read or parsed.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, fmt.Errorf("resolving config path: %w", err)
	}

	return LoadFrom(path)
}

// LoadFrom reads and parses a config file at the given path. If the file does
// not exist, it returns an empty Config.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return &cfg, nil
}

// Save marshals the config and writes it to the config file, creating the
// parent directory if necessary.
func Save(cfg *Config) error {
	path, err := Path()
	if err != nil {
		return fmt.Errorf("resolving config path: %w", err)
	}

	return SaveTo(cfg, path)
}

// SaveTo marshals the config and writes it to the given path, creating the
// parent directory if necessary.
func SaveTo(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// AddRepo appends a repo entry to the config if no entry with the same path
// already exists. It returns ErrRepoAlreadyExists if the path is a duplicate.
func AddRepo(cfg *Config, entry RepoEntry) (*Config, error) {
	for _, r := range cfg.Repos {
		if r.Path == entry.Path {
			return cfg, ErrRepoAlreadyExists
		}
	}

	cfg.Repos = append(cfg.Repos, entry)
	return cfg, nil
}
