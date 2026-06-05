// Package config handles loading, saving, and path resolution for the soko
// global configuration file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrRepoAlreadyExists is returned when attempting to add a repo whose path
// is already registered in the config.
var (
	// ErrRepoAlreadyExists is returned when attempting to add a repo whose path
	// is already registered in the config.
	ErrRepoAlreadyExists = errors.New("repo already exists")

	// ErrRepoNotFound is returned when attempting to remove a repo that is not
	// registered in the config.
	ErrRepoNotFound = errors.New("repo not found")
)

// RepoEntry represents a single registered git repository or worktree.
type RepoEntry struct {
	Name       string   `yaml:"name"`
	Path       string   `yaml:"path"`
	Tags       []string `yaml:"tags,omitempty"`
	WorktreeOf string   `yaml:"worktree_of,omitempty"`
}

// IsWorktreeEntry returns true if this entry is a linked worktree.
func (r *RepoEntry) IsWorktreeEntry() bool {
	return r.WorktreeOf != ""
}

// DiscoverConfig controls automatic repo discovery driven by the shell hook.
// When Enabled, navigating into a git repo registers it without soko scan.
type DiscoverConfig struct {
	// Enabled turns auto-discovery on. Off by default.
	Enabled bool `yaml:"enabled"`
	// Roots, when non-empty, restricts discovery to repos under these paths.
	// Empty means discover anywhere. Paths must be absolute (the CLI resolves
	// them on input); a hand-edited "~/..." root will not match.
	Roots []string `yaml:"roots,omitempty"`
	// Ignore holds glob patterns; a repo whose path matches any pattern (on a
	// path segment or the full path) is skipped. Built-in ignores always apply.
	Ignore []string `yaml:"ignore,omitempty"`
	// Tags are applied to every auto-discovered repo.
	Tags []string `yaml:"tags,omitempty"`
}

// Config is the top-level structure of the soko config file.
type Config struct {
	GitPath  string            `yaml:"git_path,omitempty"`
	Aliases  map[string]string `yaml:"aliases,omitempty"`
	Discover *DiscoverConfig   `yaml:"discover,omitempty"`
	Repos    []RepoEntry       `yaml:"repos"`
}

// builtinDiscoverIgnores are path segments never auto-discovered. These are
// dependency/vendor trees that frequently contain nested git repos a developer
// would not want registered.
var builtinDiscoverIgnores = []string{"node_modules", "vendor"}

// DiscoverEnabled reports whether auto-discovery is turned on.
func (c *Config) DiscoverEnabled() bool {
	return c.Discover != nil && c.Discover.Enabled
}

// EnsureDiscover returns the discover config, allocating it if absent.
func (c *Config) EnsureDiscover() *DiscoverConfig {
	if c.Discover == nil {
		c.Discover = &DiscoverConfig{}
	}
	return c.Discover
}

// ShouldDiscover reports whether a repo at the given (cleaned, absolute) path
// is eligible for auto-discovery: discovery must be enabled, the path must sit
// under a configured root (if any), and it must not match a built-in or
// user-configured ignore pattern.
func ShouldDiscover(cfg *Config, path string) bool {
	if !cfg.DiscoverEnabled() {
		return false
	}
	d := cfg.Discover

	if len(d.Roots) > 0 {
		within := false
		for _, root := range d.Roots {
			if pathWithinRoot(path, root) {
				within = true
				break
			}
		}
		if !within {
			return false
		}
	}

	for _, seg := range strings.Split(path, string(os.PathSeparator)) {
		for _, ig := range builtinDiscoverIgnores {
			if seg == ig {
				return false
			}
		}
		for _, pat := range d.Ignore {
			if matched, _ := filepath.Match(pat, seg); matched {
				return false
			}
		}
	}
	for _, pat := range d.Ignore {
		if matched, _ := filepath.Match(pat, path); matched {
			return false
		}
	}

	return true
}

// pathWithinRoot reports whether path is root itself or nested under it,
// matching on path-segment boundaries so /home/me/proj does not match /home/me/pr.
func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(os.PathSeparator))
}

// GitBinary returns the git binary path. If GitPath is set in the config,
// it uses that. Otherwise falls back to "git" (resolved via PATH).
func (c *Config) GitBinary() string {
	if c.GitPath != "" {
		return c.GitPath
	}
	return "git"
}

// Set sets a config key to a value. Returns an error for unknown keys.
func Set(cfg *Config, key, value string) error {
	switch key {
	case "git_path":
		if value != "" {
			if err := validateExecutable(value); err != nil {
				return fmt.Errorf("invalid git_path: %w", err)
			}
		}
		cfg.GitPath = value
		return nil
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
}

// ValidateGitPath checks that path points to a regular, executable file.
// Exported for use in main.go startup validation.
func ValidateGitPath(path string) error {
	return validateExecutable(path)
}

// validateExecutable checks that path points to a regular, executable file.
func validateExecutable(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Resolve symlink and re-stat to check the target.
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolving symlink: %w", err)
		}
		info, err = os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("%s: %w", resolved, err)
		}
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: not a regular file", path)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s: not executable", path)
	}
	return nil
}

// Get returns the value of a config key. Returns an error for unknown keys.
func Get(cfg *Config, key string) (string, error) {
	switch key {
	case "git_path":
		if cfg.GitPath == "" {
			return "git (default)", nil
		}
		return cfg.GitPath, nil
	default:
		return "", fmt.Errorf("unknown config key: %s", key)
	}
}

// Path returns the absolute path to the soko config file. It respects
// $XDG_CONFIG_HOME if set. On Unix, falls back to ~/.config/soko/config.yaml.
// On Windows, falls back to %LOCALAPPDATA%\soko\config.yaml.
func Path() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		if runtime.GOOS == "windows" {
			dir = os.Getenv("LOCALAPPDATA")
		}
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolving home directory: %w", err)
			}
			dir = filepath.Join(home, ".config")
		}
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
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	// Write atomically via a temp file in the same directory followed by a
	// rename, so concurrent writers (e.g. discover hooks in multiple shells)
	// never observe a truncated or partially written config.
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp config file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed away

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing config file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting config permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp config file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing config file: %w", err)
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

// RemoveRepo removes a repo from the config by name. It returns
// ErrRepoNotFound if no repo with the given name exists.
func RemoveRepo(cfg *Config, name string) (*Config, RepoEntry, error) {
	for i, r := range cfg.Repos {
		if r.Name == name {
			removed := cfg.Repos[i]
			cfg.Repos = append(cfg.Repos[:i], cfg.Repos[i+1:]...)
			return cfg, removed, nil
		}
	}
	return cfg, RepoEntry{}, ErrRepoNotFound
}

// RemoveRepoByPath removes a repo from the config by path. It returns
// ErrRepoNotFound if no repo with the given path exists.
func RemoveRepoByPath(cfg *Config, path string) (*Config, RepoEntry, error) {
	for i, r := range cfg.Repos {
		if r.Path == path {
			removed := cfg.Repos[i]
			cfg.Repos = append(cfg.Repos[:i], cfg.Repos[i+1:]...)
			return cfg, removed, nil
		}
	}
	return cfg, RepoEntry{}, ErrRepoNotFound
}

// Clear removes all repos from the config.
func Clear(cfg *Config) *Config {
	cfg.Repos = nil
	return cfg
}

// AddTag adds a tag to a repo. Returns ErrRepoNotFound if the repo doesn't
// exist. No-op if the tag already exists on the repo.
func AddTag(cfg *Config, repoName, tag string) (*Config, error) {
	tag = normalizeTag(tag)
	for i, r := range cfg.Repos {
		if r.Name == repoName {
			for _, t := range r.Tags {
				if t == tag {
					return cfg, nil
				}
			}
			cfg.Repos[i].Tags = append(cfg.Repos[i].Tags, tag)
			return cfg, nil
		}
	}
	return cfg, ErrRepoNotFound
}

// RemoveTag removes a tag from a repo. Returns ErrRepoNotFound if the repo
// doesn't exist. No-op if the tag doesn't exist on the repo.
func RemoveTag(cfg *Config, repoName, tag string) (*Config, error) {
	tag = normalizeTag(tag)
	for i, r := range cfg.Repos {
		if r.Name == repoName {
			tags := make([]string, 0, len(r.Tags))
			for _, t := range r.Tags {
				if t != tag {
					tags = append(tags, t)
				}
			}
			cfg.Repos[i].Tags = tags
			return cfg, nil
		}
	}
	return cfg, ErrRepoNotFound
}

// ListTags returns all unique tags across all repos, sorted alphabetically.
func ListTags(cfg *Config) []string {
	seen := make(map[string]bool)
	for _, r := range cfg.Repos {
		for _, t := range r.Tags {
			seen[t] = true
		}
	}

	tags := make([]string, 0, len(seen))
	for t := range seen {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// FilterByTags returns repos that have at least one of the given tags.
// Multiple tags combine with OR.
func FilterByTags(repos []RepoEntry, tags []string) []RepoEntry {
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[normalizeTag(t)] = true
	}

	var filtered []RepoEntry
	for _, r := range repos {
		for _, t := range r.Tags {
			if tagSet[t] {
				filtered = append(filtered, r)
				break
			}
		}
	}
	return filtered
}

// TagCount returns a map of tag name to the number of repos that have it.
func TagCount(cfg *Config) map[string]int {
	counts := make(map[string]int)
	for _, r := range cfg.Repos {
		for _, t := range r.Tags {
			counts[t]++
		}
	}
	return counts
}

func normalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

// FindRepoByPath returns the repo entry whose path matches, or ErrRepoNotFound.
func FindRepoByPath(cfg *Config, path string) (*RepoEntry, error) {
	for i, r := range cfg.Repos {
		if r.Path == path {
			return &cfg.Repos[i], nil
		}
	}
	return nil, ErrRepoNotFound
}

// FindWorktrees returns all entries that are worktrees of the named parent repo.
func FindWorktrees(cfg *Config, parentName string) []RepoEntry {
	var worktrees []RepoEntry
	for _, r := range cfg.Repos {
		if r.WorktreeOf == parentName {
			worktrees = append(worktrees, r)
		}
	}
	return worktrees
}

// FilterNoWorktrees returns only entries that are NOT worktrees.
func FilterNoWorktrees(repos []RepoEntry) []RepoEntry {
	var filtered []RepoEntry
	for _, r := range repos {
		if r.WorktreeOf == "" {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// FindRepo searches for repos matching the query. It first tries an exact
// match on Name, then falls back to prefix matching. Returns all matches.
func FindRepo(cfg *Config, query string) []RepoEntry {
	// Exact match.
	for _, r := range cfg.Repos {
		if r.Name == query {
			return []RepoEntry{r}
		}
	}

	// Prefix match.
	var matches []RepoEntry
	for _, r := range cfg.Repos {
		if strings.HasPrefix(r.Name, query) {
			matches = append(matches, r)
		}
	}

	return matches
}
