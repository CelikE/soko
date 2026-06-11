// Package journal is soko's undo log: a small, capped record of the destructive
// operations soko itself performs, with just enough pre-image data to reverse
// the most recent one. It stores refs, registry entries, and file pre-images —
// never working-tree dirt — so undo is for "oops, just now", not a backup.
//
// The package is pure data: load, append, pop, and a cap. Reverting an entry
// (which needs git or the registry) lives in the cli layer so this package
// stays free of side effects and trivially testable.
package journal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/CelikE/soko/internal/config"
)

// Op identifies what kind of operation an entry can reverse.
type Op string

const (
	// OpClean reverses `soko clean` — recreate the deleted branches at their
	// recorded SHAs.
	OpClean Op = "clean"
	// OpPull reverses a fast-forward pull (e.g. from `soko ui`) — reset the
	// branch back to the pre-pull SHA.
	OpPull Op = "pull"
)

// BranchRef is a deleted branch that undo can recreate.
type BranchRef struct {
	Repo   string `yaml:"repo"`
	Path   string `yaml:"path"`
	Branch string `yaml:"branch"`
	SHA    string `yaml:"sha"`
}

// PullRef is a fast-forwarded repo that undo can reset to its pre-pull SHA.
type PullRef struct {
	Repo string `yaml:"repo"`
	Path string `yaml:"path"`
	SHA  string `yaml:"sha"`
}

// Entry is one reversible operation.
type Entry struct {
	Op       Op          `yaml:"op"`
	Time     time.Time   `yaml:"time"`
	Summary  string      `yaml:"summary"`
	Branches []BranchRef `yaml:"branches,omitempty"`
	Pulls    []PullRef   `yaml:"pulls,omitempty"`
}

// Journal is the on-disk log, oldest first.
type Journal struct {
	Entries []Entry `yaml:"entries"`
}

// MaxEntries caps the journal. Undo is for recent mistakes, not history — old
// entries fall off the front so the file never grows unbounded.
const MaxEntries = 20

// ErrEmpty is returned when there is nothing left to undo.
var ErrEmpty = errors.New("journal is empty")

// Path returns the journal file path, alongside the soko config file.
func Path() (string, error) {
	cfgPath, err := config.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfgPath), "journal.yaml"), nil
}

// Load reads the journal, returning an empty one if the file does not exist.
func Load() (*Journal, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Journal{}, nil
		}
		return nil, fmt.Errorf("reading journal: %w", err)
	}
	var j Journal
	if err := yaml.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("parsing journal: %w", err)
	}
	return &j, nil
}

// Save writes the journal, capping it to the most recent MaxEntries.
func Save(j *Journal) error {
	if len(j.Entries) > MaxEntries {
		j.Entries = j.Entries[len(j.Entries)-MaxEntries:]
	}

	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating journal directory: %w", err)
	}
	data, err := yaml.Marshal(j)
	if err != nil {
		return fmt.Errorf("encoding journal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing journal: %w", err)
	}
	return nil
}

// Append records a new entry at the end, capping the log.
func Append(e *Entry) error {
	j, err := Load()
	if err != nil {
		return err
	}
	j.Entries = append(j.Entries, *e)
	return Save(j)
}

// Last returns the most recent entry, or false if the journal is empty.
func (j *Journal) Last() (Entry, bool) {
	if len(j.Entries) == 0 {
		return Entry{}, false
	}
	return j.Entries[len(j.Entries)-1], true
}

// PopLast removes and returns the most recent entry, persisting the shorter
// journal. It returns ErrEmpty when there is nothing to pop.
func PopLast() (Entry, error) {
	j, err := Load()
	if err != nil {
		return Entry{}, err
	}
	last, ok := j.Last()
	if !ok {
		return Entry{}, ErrEmpty
	}
	j.Entries = j.Entries[:len(j.Entries)-1]
	if err := Save(j); err != nil {
		return Entry{}, err
	}
	return last, nil
}
