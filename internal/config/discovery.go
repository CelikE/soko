package config

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// RegisterDiscovered serializes the shell hook's short registry mutation.
// Unlike interactive commands, hooks cannot report a stale-snapshot failure to
// their caller, so each hook loads and updates under the same commit lock.
// Git probing happens before this call. A nil entry means nothing was added.
func RegisterDiscovered(ctx context.Context, name, repoPath string) (*RepoEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := Path()
	if err != nil {
		return nil, err
	}
	path, err = configFilePath(path)
	if err != nil {
		return nil, err
	}
	lock, err := openConfigLock(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Close() }()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = tryConfigLock(lock)
		if !errors.Is(err, ErrBusy) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
	if err != nil {
		return nil, fmt.Errorf("locking config: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		return nil, err
	}
	if !ShouldDiscover(cfg, repoPath) {
		return nil, nil
	}
	if _, err := FindRepoByPath(cfg, repoPath); err == nil {
		return nil, nil
	}
	entry := &RepoEntry{Name: UniqueRepoName(cfg, name), Path: repoPath, Tags: cfg.Discover.Tags}
	if _, err := AddRepo(cfg, entry); err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}
	if err := writeConfigBytes(path, data); err != nil {
		return nil, err
	}
	return entry, nil
}
