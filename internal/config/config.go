// Package config handles loading, saving, and path resolution for the soko
// global configuration file.
package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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

	// ErrRepoNameExists is returned when a different path owns the requested name.
	ErrRepoNameExists = errors.New("repo name already exists")

	// ErrRepoNotFound is returned when attempting to remove a repo that is not
	// registered in the config.
	ErrRepoNotFound = errors.New("repo not found")

	// ErrAmbiguousRepo is returned when an exact name identifies multiple entries.
	ErrAmbiguousRepo = errors.New("ambiguous repo name")

	// ErrInheritedTag is returned when attempting to remove a tag from a
	// worktree that the worktree only holds by inheritance from its parent.
	ErrInheritedTag = errors.New("tag is inherited from the parent repo; retag the parent to change it")

	// ErrConflict means a snapshot is stale and must be reloaded before retrying.
	ErrConflict = errors.New("config changed while command was running; retry the command")

	// ErrBusy means another process is committing a config update.
	ErrBusy = errors.New("config is being updated by another process; retry the command")
)

// RepoEntry represents a single registered git repository or worktree.
type RepoEntry struct {
	Name       string            `yaml:"name"`
	Path       string            `yaml:"path"`
	Tags       []string          `yaml:"tags,omitempty"`
	WorktreeOf string            `yaml:"worktree_of,omitempty"`
	Meta       map[string]string `yaml:"meta,omitempty"`
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
	GitPath  string                   `yaml:"git_path,omitempty"`
	Aliases  map[string]string        `yaml:"aliases,omitempty"`
	Discover *DiscoverConfig          `yaml:"discover,omitempty"`
	Contexts map[string]*ContextEntry `yaml:"contexts,omitempty"`
	Repos    []RepoEntry              `yaml:"repos"`
	version  configVersion
}

// Keep the loaded byte revision by value: copies of Config must not acquire a
// newer baseline merely because another copy was saved successfully.
type configVersion struct {
	path   string
	hash   [sha256.Size]byte
	exists bool
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
			if PathWithinRoot(path, root) {
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

// PathWithinRoot reports whether path is root itself or nested under it,
// matching on path-segment boundaries so /home/me/proj does not match /home/me/pr.
// Shared by repo discovery and soko apply's destination containment guard.
func PathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(os.PathSeparator))
}

// RepoCount returns the number of registered repos.
func RepoCount(cfg *Config) int {
	return len(cfg.Repos)
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
	if runtime.GOOS == "windows" {
		// Windows files have no Unix executable permission bits. Use the
		// platform's executable lookup rules instead.
		if _, err := exec.LookPath(path); err != nil {
			return fmt.Errorf("%s: not executable: %w", path, err)
		}
	} else if info.Mode()&0o111 == 0 {
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
	path, err := configFilePath(path)
	if err != nil {
		return nil, err
	}
	data, exists, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}
	cfg.version = configVersion{path: path, hash: sha256.Sum256(data), exists: exists}

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

// SaveTo commits a loaded snapshot only if its destination is unchanged. A new
// Config can create a file but cannot replace an existing unobserved snapshot.
// SaveTo to a different path likewise requires an absent destination.
func SaveTo(cfg *Config, path string) error {
	path, err := configFilePath(path)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	expected := cfg.version
	if expected.path != path {
		expected = configVersion{path: path}
	}
	version, err := saveConfigBytes(path, data, expected)
	if err == nil {
		cfg.version = version
	}
	return err
}

// SaveEdited validates and commits raw editor output against the bytes observed
// before editing. Keeping raw bytes preserves comments and user formatting.
// The editor runs before this call, outside the short commit lock.
func SaveEdited(path string, before, after []byte, existed bool) error {
	path, err := configFilePath(path)
	if err != nil {
		return err
	}
	var cfg Config
	if err := yaml.Unmarshal(after, &cfg); err != nil {
		return fmt.Errorf("validating edited config: %w", err)
	}
	_, err = saveConfigBytes(path, after, configVersion{path: path, hash: sha256.Sum256(before), exists: existed})
	return err
}

func saveConfigBytes(path string, data []byte, expected configVersion) (configVersion, error) {
	// The sidecar is never renamed or deleted: every writer must lock the
	// same inode even though the config itself is atomically replaced. OS
	// locks disappear on process exit, so a crashed writer cannot strand one.
	lock, err := openConfigLock(path)
	if err != nil {
		return configVersion{}, err
	}
	defer func() { _ = lock.Close() }()
	if err := tryConfigLock(lock); err != nil {
		return configVersion{}, fmt.Errorf("locking config: %w", err)
	}
	current, exists, err := readConfigFile(path)
	if err != nil {
		return configVersion{}, err
	}
	if exists != expected.exists || (exists && sha256.Sum256(current) != expected.hash) {
		return configVersion{}, ErrConflict
	}
	if err := writeConfigBytes(path, data); err != nil {
		return configVersion{}, err
	}
	return configVersion{path: path, hash: sha256.Sum256(data), exists: true}, nil
}

func openConfigLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating config directory: %w", err)
	}
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening config lock: %w", err)
	}
	return file, nil
}

func writeConfigBytes(path string, data []byte) error {
	// Write atomically via a temp file in the same directory followed by a
	// rename, so concurrent writers (e.g. discover hooks in multiple shells)
	// never observe a truncated or partially written config.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
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

func readConfigFile(path string) (data []byte, exists bool, err error) {
	data, err = os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading config file: %w", err)
	}
	return data, true, nil
}

// Resolve aliases before choosing the lock path. A symlinked config must not
// use a different lock from its target or be replaced with an unrelated file.
func configFilePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving config path: %w", err)
	}
	// Resolve the deepest existing ancestor even before nested directories
	// are created, keeping the baseline stable across the first save.
	probe, missing := abs, ""
	for {
		if resolved, err := filepath.EvalSymlinks(probe); err == nil {
			return filepath.Join(resolved, missing), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("resolving config path: %w", err)
		}
		if info, err := os.Lstat(probe); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("config path contains a dangling symlink: %s", probe)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("no existing config ancestor: %s", abs)
		}
		missing = filepath.Join(filepath.Base(probe), missing)
		probe = parent
	}
}

// AddRepo appends a repo entry to the config if no entry with the same path
// already exists. It returns ErrRepoAlreadyExists if the path is a duplicate.
// New entries receive canonical, unique tags, including in the returned entry.
func AddRepo(cfg *Config, entry *RepoEntry) (*Config, error) {
	for _, r := range cfg.Repos {
		if r.Path == entry.Path {
			return cfg, ErrRepoAlreadyExists
		}
	}
	for _, r := range cfg.Repos {
		if r.Name == entry.Name {
			return cfg, ErrRepoNameExists
		}
	}

	entry.Tags = NormalizeTags(entry.Tags)
	cfg.Repos = append(cfg.Repos, *entry)
	return cfg, nil
}

// RemoveRepo removes a repo from the config by its unique name. It returns
// ErrRepoNotFound for an unknown name or ErrAmbiguousRepo for duplicate names.
func RemoveRepo(cfg *Config, name string) (*Config, RepoEntry, error) {
	i, err := repoIndexByName(cfg, name)
	if err != nil {
		return cfg, RepoEntry{}, err
	}
	removed := cfg.Repos[i]
	cfg.Repos = append(cfg.Repos[:i], cfg.Repos[i+1:]...)
	return cfg, removed, nil
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

// AddTag adds a tag to a uniquely named repo. Returns ErrRepoNotFound if the repo
// doesn't exist or ErrAmbiguousRepo for duplicate names. Existing tags are a no-op.
func AddTag(cfg *Config, repoName, tag string) (*Config, error) {
	r, err := FindRepoByName(cfg, repoName)
	if err != nil {
		return cfg, err
	}
	r.Tags = NormalizeTags(r.Tags, []string{tag})
	return cfg, nil
}

// RemoveTag removes a tag from a uniquely named repo. Returns ErrRepoNotFound if
// the repo doesn't exist or ErrAmbiguousRepo for duplicate names. Absent tags are
// a no-op.
func RemoveTag(cfg *Config, repoName, tag string) (*Config, error) {
	r, err := FindRepoByName(cfg, repoName)
	if err != nil {
		return cfg, err
	}
	tag = normalizeTag(tag)
	ownTags := NormalizeTags(r.Tags)
	owns := slices.Contains(ownTags, tag)
	// On a worktree, a tag the entry doesn't own but matches via
	// inheritance can only be removed by retagging the parent.
	if !owns && r.WorktreeOf != "" && slices.Contains(EffectiveTags(cfg, r), tag) {
		return cfg, ErrInheritedTag
	}
	tags := make([]string, 0, len(ownTags))
	for _, t := range ownTags {
		if t != tag {
			tags = append(tags, t)
		}
	}
	r.Tags = tags
	return cfg, nil
}

// ListTags returns all unique tags across all repos, sorted alphabetically.
func ListTags(cfg *Config) []string {
	seen := make(map[string]bool)
	for _, r := range cfg.Repos {
		for _, t := range NormalizeTags(r.Tags) {
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

// EffectiveTags returns a repo's effective tag set: its own tags plus, for a
// worktree entry, the tags of its parent repo (resolved at call time via
// worktree_of). Inheritance is dynamic — retagging the parent instantly
// re-scopes the worktree. A worktree whose parent is unregistered or ambiguous
// falls back to its own tags only. All results are canonical and deduplicated,
// including legacy values, without changing the stored entry or its parent.
func EffectiveTags(cfg *Config, r *RepoEntry) []string {
	if r.WorktreeOf == "" {
		return NormalizeTags(r.Tags)
	}
	parent, err := FindRepoByName(cfg, r.WorktreeOf)
	if err != nil {
		return NormalizeTags(r.Tags)
	}
	return NormalizeTags(r.Tags, parent.Tags)
}

// FilterByTags returns repos that have at least one of the given tags.
// Multiple tags combine with OR. Worktree entries also match their parent's
// tags via EffectiveTags, so cfg is needed to resolve parents.
func FilterByTags(cfg *Config, repos []RepoEntry, tags []string) []RepoEntry {
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[normalizeTag(t)] = true
	}

	var filtered []RepoEntry
	for i := range repos {
		r := repos[i]
		for _, t := range EffectiveTags(cfg, &r) {
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
	for i := range cfg.Repos {
		for _, t := range EffectiveTags(cfg, &cfg.Repos[i]) {
			counts[t]++
		}
	}
	return counts
}

func normalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

// NormalizeTags combines tag groups in first-seen order, lowercasing and
// trimming each value, omitting blanks and duplicates. Inputs are not modified.
func NormalizeTags(groups ...[]string) []string {
	var tags []string
	seen := make(map[string]bool)
	for _, group := range groups {
		for _, tag := range group {
			tag = normalizeTag(tag)
			if tag != "" && !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

// normalizeMetaKey lowercases and trims a metadata key, the same discipline
// used for tags, so "Owner" and "owner" refer to the same key.
func normalizeMetaKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

// SetMeta upserts a metadata key/value on a repo, allocating the map lazily and
// normalizing the key. Returns ErrRepoNotFound for an unknown repo or
// ErrAmbiguousRepo for duplicate names. Values are stored verbatim.
func SetMeta(cfg *Config, repoName, key, value string) (*Config, error) {
	r, err := FindRepoByName(cfg, repoName)
	if err != nil {
		return cfg, err
	}
	if r.Meta == nil {
		r.Meta = make(map[string]string)
	}
	r.Meta[normalizeMetaKey(key)] = value
	return cfg, nil
}

// UnsetMeta removes a metadata key from a repo. Returns ErrRepoNotFound for an
// unknown repo or ErrAmbiguousRepo for duplicate names. Removing the last key
// sets the map back to nil so the meta block disappears from YAML. Removing an
// absent key is a no-op.
func UnsetMeta(cfg *Config, repoName, key string) (*Config, error) {
	r, err := FindRepoByName(cfg, repoName)
	if err != nil {
		return cfg, err
	}
	delete(r.Meta, normalizeMetaKey(key))
	if len(r.Meta) == 0 {
		r.Meta = nil
	}
	return cfg, nil
}

// ClearMeta removes all metadata from a repo. Returns ErrRepoNotFound for an
// unknown repo or ErrAmbiguousRepo for duplicate names. It touches only the
// named repo's metadata.
func ClearMeta(cfg *Config, repoName string) (*Config, error) {
	r, err := FindRepoByName(cfg, repoName)
	if err != nil {
		return cfg, err
	}
	r.Meta = nil
	return cfg, nil
}

// FilterByMeta returns repos whose metadata contains every key=value in
// constraints (AND). Constraint keys are normalized to match how SetMeta stores
// them. An empty constraint map returns repos unchanged.
func FilterByMeta(repos []RepoEntry, constraints map[string]string) []RepoEntry {
	if len(constraints) == 0 {
		return repos
	}
	norm := make(map[string]string, len(constraints))
	for k, v := range constraints {
		norm[normalizeMetaKey(k)] = v
	}
	var filtered []RepoEntry
	for _, r := range repos {
		match := true
		for k, v := range norm {
			// Require the key to be present, so a constraint with an empty
			// value matches only repos that set the key to "", not every
			// unannotated repo (a nil map yields "" for any key).
			if rv, ok := r.Meta[k]; !ok || rv != v {
				match = false
				break
			}
		}
		if match {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// FindRepoByName returns the entry with a unique exact name match. It returns
// ErrRepoNotFound for an unknown name or ErrAmbiguousRepo with candidate paths
// when a legacy or hand-edited config contains duplicate names.
func FindRepoByName(cfg *Config, name string) (*RepoEntry, error) {
	i, err := repoIndexByName(cfg, name)
	if err != nil {
		return nil, err
	}
	return &cfg.Repos[i], nil
}

func repoIndexByName(cfg *Config, name string) (int, error) {
	index := -1
	var paths []string
	for i, r := range cfg.Repos {
		if r.Name == name {
			index = i
			paths = append(paths, fmt.Sprintf("%q", r.Path))
		}
	}
	if len(paths) > 1 {
		return -1, fmt.Errorf("%w %q: %s; assign unique names with soko config edit", ErrAmbiguousRepo, name, strings.Join(paths, ", "))
	}
	if index == -1 {
		return -1, ErrRepoNotFound
	}
	return index, nil
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
	var exact []RepoEntry
	for _, r := range cfg.Repos {
		if r.Name == query {
			exact = append(exact, r)
		}
	}
	if len(exact) > 0 {
		return exact
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

// UniqueRepoName allocates a readable alias without changing existing names.
func UniqueRepoName(cfg *Config, name string) string {
	candidate := name
	for suffix := 2; ; suffix++ {
		used := false
		for _, r := range cfg.Repos {
			if r.Name == candidate {
				used = true
				break
			}
		}
		if !used {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", name, suffix)
	}
}

// RepoFamily returns the base alias for a registered checkout.
func RepoFamily(r *RepoEntry) string {
	if r.WorktreeOf != "" {
		return r.WorktreeOf
	}
	return r.Name
}
