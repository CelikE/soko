# Changelog

## Unreleased

### Features

- Print repo path for quick navigation with prefix matching
- Fetch all registered repos in parallel with optional --prune
- Add integration tests exercising all CLI commands end-to-end
- Remove repos from the registry by name, path, or clear all
- Filter status with --dirty, --clean, --ahead, --behind flags

### Bug Fixes

- Fix golangci-lint v2 configuration for CI compatibility
- Fix index out of range panic when status filters reduce result count

## v0.1.1

### Bug Fixes

- Fix golangci-lint v2 configuration for CI compatibility

### Chores

- Add CI and automated release GitHub Actions workflows
