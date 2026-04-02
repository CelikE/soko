# Changelog

## Unreleased

### Features

- View config path or open in editor with soko config
- Doc checks if shell-init is configured
- Show which command is being run in soko exec output
- Fish shell support via soko shell-init --fish
- Interactive repo picker with arrow-key navigation
- Tree view for soko list --group grouped by tags
- Show tags column in soko list when repos have tags
- Shell hook for direct navigation with soko go and soko cd
- Tag commands detect current repo from working directory

### Bug Fixes

- Include tags in soko list --json output
- Unified output style with dimmed headers and consistent summaries
- Fix picker colors when stdout is piped
- Show error messages on stderr instead of silent exit

## v0.4.0

### Features

- Tag repos with labels and filter any command with --tag
- Add --fetch flag to status for accurate ahead/behind counts
