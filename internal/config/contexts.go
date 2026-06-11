package config

import (
	"sort"
	"time"
)

// ContextRepo records one repo's place inside a saved context: the branch it
// was on (or the detached HEAD commit) and whether soko stashed its dirty
// working tree under the context's stash message.
type ContextRepo struct {
	Name     string `yaml:"name"`
	Branch   string `yaml:"branch"`
	Detached bool   `yaml:"detached,omitempty"`
	Stashed  bool   `yaml:"stashed,omitempty"`
}

// ContextEntry is a named snapshot of where every selected repo was at save
// time. The stashes themselves live in each repo's stash list (found again by
// message), so dropping a context never loses work.
type ContextEntry struct {
	SavedAt time.Time     `yaml:"saved_at"`
	Repos   []ContextRepo `yaml:"repos"`
}

// SetContext upserts a named context, allocating the map lazily.
func SetContext(cfg *Config, name string, entry *ContextEntry) {
	if cfg.Contexts == nil {
		cfg.Contexts = make(map[string]*ContextEntry)
	}
	cfg.Contexts[name] = entry
}

// GetContext returns the named context, or false when it does not exist.
func GetContext(cfg *Config, name string) (*ContextEntry, bool) {
	entry, ok := cfg.Contexts[name]
	return entry, ok
}

// DeleteContext removes a named context. Removing the last one sets the map
// back to nil so the contexts block disappears from YAML. Returns false when
// the context does not exist.
func DeleteContext(cfg *Config, name string) bool {
	if _, ok := cfg.Contexts[name]; !ok {
		return false
	}
	delete(cfg.Contexts, name)
	if len(cfg.Contexts) == 0 {
		cfg.Contexts = nil
	}
	return true
}

// ContextNames returns all saved context names, sorted alphabetically.
func ContextNames(cfg *Config) []string {
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
