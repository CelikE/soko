// Package journal is soko's undo log: a small, capped record of the destructive
// operations soko itself performs, with just enough pre-image data to reverse
// the most recent one. It stores refs, registry entries, and file pre-images —
// never working-tree dirt — so undo is for "oops, just now", not a backup.
//
// Journal writes use atomic replacement and a cross-process lock. Reverting an
// entry (which needs git or the registry) lives in the cli layer, outside the
// short journal transaction.
package journal

import (
	"crypto/sha256"
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

// PullRef records both ends of a fast-forward and the branch it advanced.
// Pending undo requires the checkout to still match Branch and PostSHA.
// Undone checkpoints a successful reset so retries never revisit that checkout.
type PullRef struct {
	Repo    string `yaml:"repo"`
	Path    string `yaml:"path"`
	SHA     string `yaml:"sha"`
	Branch  string `yaml:"branch,omitempty"`
	PostSHA string `yaml:"post_sha,omitempty"`
	Undone  bool   `yaml:"undone,omitempty"`
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
	version journalVersion
}

type journalVersion struct {
	path   string
	hash   [sha256.Size]byte
	exists bool
}

// MaxEntries caps the journal. Undo is for recent mistakes, not history — old
// entries fall off the front so the file never grows unbounded.
const MaxEntries = 20

// ErrEmpty is returned when there is nothing left to undo.
var ErrEmpty = errors.New("journal is empty")

// ErrConflict is returned when a loaded journal has changed on disk. Reload
// before retrying so a checkpoint or completed undo cannot erase newer entries.
var ErrConflict = errors.New("journal changed; reload before retrying")

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
	return load(path)
}

func load(path string) (*Journal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Journal{version: journalVersion{path: path}}, nil
		}
		return nil, fmt.Errorf("reading journal: %w", err)
	}
	var j Journal
	if err := yaml.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("parsing journal: %w", err)
	}
	j.version = journalVersion{path: path, hash: sha256.Sum256(data), exists: true}
	return &j, nil
}

// Save atomically commits a loaded journal if its on-disk version is unchanged,
// capping it to the most recent MaxEntries. A new Journal can create a file but
// cannot replace existing history without loading it first.
func Save(j *Journal) error {
	path, err := Path()
	if err != nil {
		return err
	}
	lock, err := lockJournal(path)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	current, err := load(path)
	if err != nil {
		return err
	}
	expected := j.version
	if expected.path != path {
		expected = journalVersion{path: path}
	}
	if current.version != expected {
		return ErrConflict
	}
	return save(path, j)
}

// save requires the caller to hold the journal's sidecar lock.
func save(path string, j *Journal) error {
	if len(j.Entries) > MaxEntries {
		j.Entries = j.Entries[len(j.Entries)-MaxEntries:]
	}
	data, err := yaml.Marshal(j)
	if err != nil {
		return fmt.Errorf("encoding journal: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".journal-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp journal: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("writing journal: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("syncing journal: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp journal: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing journal: %w", err)
	}
	j.version = journalVersion{path: path, hash: sha256.Sum256(data), exists: true}
	return nil
}

func lockJournal(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating journal directory: %w", err)
	}
	// Never remove or replace the sidecar: all writers must lock the same
	// inode even when the journal itself is replaced. Process exit releases
	// the OS lock, so a crashed writer cannot strand it.
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening journal lock: %w", err)
	}
	if err := lockJournalFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("locking journal: %w", err)
	}
	return file, nil
}

// Append records a new entry at the end, capping the log.
func Append(e *Entry) error {
	path, err := Path()
	if err != nil {
		return err
	}
	lock, err := lockJournal(path)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	j, err := load(path)
	if err != nil {
		return err
	}
	j.Entries = append(j.Entries, *e)
	return save(path, j)
}

// Last returns the most recent entry, or false if the journal is empty.
func (j *Journal) Last() (Entry, bool) {
	if len(j.Entries) == 0 {
		return Entry{}, false
	}
	return j.Entries[len(j.Entries)-1], true
}

// PopLast removes and returns the most recent entry in a single locked
// transaction. It returns ErrEmpty when there is nothing to pop.
func PopLast() (Entry, error) {
	path, err := Path()
	if err != nil {
		return Entry{}, err
	}
	lock, err := lockJournal(path)
	if err != nil {
		return Entry{}, err
	}
	defer func() { _ = lock.Close() }()
	j, err := load(path)
	if err != nil {
		return Entry{}, err
	}
	last, ok := j.Last()
	if !ok {
		return Entry{}, ErrEmpty
	}
	j.Entries = j.Entries[:len(j.Entries)-1]
	if err := save(path, j); err != nil {
		return Entry{}, err
	}
	return last, nil
}

// PopLast consumes the loaded journal's latest entry only if no other writer
// has changed it. Use this after reverting an entry outside the journal lock.
func (j *Journal) PopLast() (Entry, error) {
	last, ok := j.Last()
	if !ok {
		return Entry{}, ErrEmpty
	}
	next := *j
	next.Entries = j.Entries[:len(j.Entries)-1]
	if err := Save(&next); err != nil {
		return Entry{}, err
	}
	*j = next
	return last, nil
}
