// Package snapshot stores named records of every repo's branch, HEAD SHA, and
// dirty state as YAML files under the soko config directory. A snapshot is the
// "save game before the boss fight": soko snapshot restore moves each repo
// back to the recorded branch@SHA.
package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/CelikE/soko/internal/config"
)

// ErrNotFound is returned when the named snapshot does not exist.
var ErrNotFound = errors.New("snapshot not found")

// Repo is one repo's recorded state inside a snapshot.
type Repo struct {
	Name     string `yaml:"name"`
	Path     string `yaml:"path"`
	Branch   string `yaml:"branch,omitempty"`
	Detached bool   `yaml:"detached,omitempty"`
	SHA      string `yaml:"sha"`
	Dirty    bool   `yaml:"dirty,omitempty"`
}

// Snapshot is a named record of the workspace at a point in time.
type Snapshot struct {
	Name    string    `yaml:"name"`
	Created time.Time `yaml:"created"`
	Repos   []Repo    `yaml:"repos"`
}

// nameRe restricts snapshot names to safe filename characters, so a name can
// never escape the snapshots directory or collide with path syntax.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// ValidateName rejects names that are empty, contain path separators, or
// otherwise can't serve as a filename.
func ValidateName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid snapshot name: %q (use letters, digits, '.', '_', '-'; must start with a letter or digit)", name)
	}
	return nil
}

// Dir returns the directory snapshots are stored in, next to the config file.
func Dir() (string, error) {
	cfgPath, err := config.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfgPath), "snapshots"), nil
}

// path returns the file path for a named snapshot.
func path(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".yaml"), nil
}

// Exists reports whether a snapshot with the given name is stored.
func Exists(name string) (bool, error) {
	p, err := path(name)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Save writes the snapshot to its YAML file, creating the directory if needed.
func Save(s *Snapshot) error {
	if err := ValidateName(s.Name); err != nil {
		return err
	}
	p, err := path(s.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("creating snapshots directory: %w", err)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshaling snapshot: %w", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("writing snapshot file: %w", err)
	}
	return nil
}

// Load reads the named snapshot. Returns ErrNotFound if it doesn't exist.
func Load(name string) (*Snapshot, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	p, err := path(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading snapshot file: %w", err)
	}
	var s Snapshot
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing snapshot file %s: %w", p, err)
	}
	return &s, nil
}

// List returns all stored snapshots, sorted newest first.
func List() ([]*Snapshot, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading snapshots directory: %w", err)
	}

	var snaps []*Snapshot
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".yaml")
		if !ok || e.IsDir() {
			continue
		}
		s, err := Load(name)
		if err != nil {
			// A corrupt or foreign file shouldn't hide the valid snapshots.
			continue
		}
		snaps = append(snaps, s)
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Created.After(snaps[j].Created) })
	return snaps, nil
}

// Names returns the stored snapshot names, sorted newest first.
func Names() ([]string, error) {
	snaps, err := List()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(snaps))
	for i, s := range snaps {
		names[i] = s.Name
	}
	return names, nil
}

// Delete removes the named snapshot. Returns ErrNotFound if it doesn't exist.
func Delete(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	p, err := path(name)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("deleting snapshot file: %w", err)
	}
	return nil
}
