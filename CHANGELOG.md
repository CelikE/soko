# Changelog

## Unreleased

### Features

- Health check for soko setup with auto-fix for stale entries
- Run arbitrary commands across all registered repos with soko exec
- Tab-complete repo names in soko remove and soko cd

### Chores

- Remove unused internal/repo/ package
- Remove CLAUDE.md from git tracking

## v0.2.0

### Features

- Print repo path for quick navigation with prefix matching
- Fetch all registered repos in parallel with optional --prune
- Add integration tests exercising all CLI commands end-to-end
- Remove repos from the registry by name, path, or clear all
- Filter status with --dirty, --clean, --ahead, --behind flags

### Bug Fixes

- Fix golangci-lint v2 configuration for CI compatibility
- Fix index out of range panic when status filters reduce result count
