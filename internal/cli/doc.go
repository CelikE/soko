package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

type checkStatus int

const (
	statusPass checkStatus = iota
	statusWarn
	statusError
)

type checkResult struct {
	Name    string
	Status  checkStatus
	Message string
	Fixable bool
	Fixed   bool
}

// newDocCmd creates the cobra command for soko doc.
func newDocCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doc",
		Short: "Check the health of your soko setup",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := cmd.OutOrStdout()
			fixFlag, _ := cmd.Flags().GetBool("fix")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			var results []checkResult

			// Check git is available.
			gitBin := git.Binary()
			gitPath, err := exec.LookPath(gitBin)
			if err != nil {
				results = append(results, checkResult{
					Name:    "git",
					Status:  statusError,
					Message: fmt.Sprintf("%s not found", gitBin),
				})
			} else {
				version, _ := git.Run(ctx, ".", "--version")
				results = append(results, checkResult{
					Name:    "git",
					Status:  statusPass,
					Message: fmt.Sprintf("%s (%s)", gitPath, version),
				})
			}

			// Check config loads.
			cfg, err := config.Load()
			if err != nil {
				results = append(results, checkResult{
					Name:    "config",
					Status:  statusError,
					Message: fmt.Sprintf("failed to load: %s", err),
				})

				if jsonFlag {
					return renderDocJSON(w, results)
				}
				renderDocResults(w, results)
				return nil
			}

			// Path error is non-fatal in doc — config already loaded successfully.
			cfgPath, _ := config.Path()
			results = append(results, checkResult{
				Name:    "config",
				Status:  statusPass,
				Message: fmt.Sprintf("%s (%d repos)", cfgPath, len(cfg.Repos)),
			})

			// Check each repo.
			var toRemove []string
			for _, repo := range cfg.Repos {
				if !pathExists(repo.Path) {
					r := checkResult{
						Name:    repo.Name,
						Status:  statusError,
						Message: fmt.Sprintf("path does not exist (%s)", repo.Path),
						Fixable: true,
					}
					if fixFlag {
						r.Fixed = true
						r.Message += " — removed from config"
						toRemove = append(toRemove, repo.Name)
					} else {
						r.Message += fmt.Sprintf("\n    → run: soko remove %s", repo.Name)
					}
					results = append(results, r)
					continue
				}

				if !git.IsGitRepo(ctx, repo.Path) {
					r := checkResult{
						Name:    repo.Name,
						Status:  statusError,
						Message: fmt.Sprintf("not a git repo (%s)", repo.Path),
						Fixable: true,
					}
					if fixFlag {
						r.Fixed = true
						r.Message += " — removed from config"
						toRemove = append(toRemove, repo.Name)
					} else {
						r.Message += fmt.Sprintf("\n    → run: soko remove %s", repo.Name)
					}
					results = append(results, r)
					continue
				}

				_, remoteErr := git.Run(ctx, repo.Path, "remote", "get-url", "origin")
				if remoteErr != nil {
					results = append(results, checkResult{
						Name:    repo.Name,
						Status:  statusWarn,
						Message: "no remote origin configured",
					})
					continue
				}

				results = append(results, checkResult{
					Name:    repo.Name,
					Status:  statusPass,
					Message: "path exists, git repo, has remote",
				})
			}

			// Check for duplicate names.
			nameCounts := make(map[string]int)
			for _, repo := range cfg.Repos {
				nameCounts[repo.Name]++
			}
			for name, count := range nameCounts {
				if count > 1 {
					results = append(results, checkResult{
						Name:    "duplicate",
						Status:  statusWarn,
						Message: fmt.Sprintf("name %q appears %d times", name, count),
					})
				}
			}

			// Check for duplicate paths.
			pathCounts := make(map[string]int)
			for _, repo := range cfg.Repos {
				pathCounts[repo.Path]++
			}
			for path, count := range pathCounts {
				if count > 1 {
					results = append(results, checkResult{
						Name:    "duplicate",
						Status:  statusWarn,
						Message: fmt.Sprintf("path %q appears %d times", path, count),
						Fixable: true,
					})
				}
			}

			// Check shell-init.
			navPath := navFilePath()
			if navPath != "" {
				// Check if shell hook is likely configured by looking for
				// the function name in common shell profile files.
				shellInitConfigured := false
				for _, profile := range shellProfiles() {
					data, readErr := os.ReadFile(profile)
					if readErr == nil && strings.Contains(string(data), "soko shell-init") {
						shellInitConfigured = true
						break
					}
				}
				if shellInitConfigured {
					results = append(results, checkResult{
						Name:    "shell-init",
						Status:  statusPass,
						Message: "shell integration configured",
					})
				} else {
					hint := `eval "$(soko shell-init)"`
					if runtime.GOOS == "windows" {
						hint = "soko shell-init --pwsh | Invoke-Expression"
					}
					results = append(results, checkResult{
						Name:    "shell-init",
						Status:  statusWarn,
						Message: fmt.Sprintf("not configured — run: %s", hint),
					})
				}
			}

			// Apply fixes.
			if fixFlag && len(toRemove) > 0 {
				for _, name := range toRemove {
					var removeErr error
					cfg, _, removeErr = config.RemoveRepo(cfg, name)
					if removeErr != nil {
						return fmt.Errorf("fixing %s: %w", name, removeErr)
					}
				}
				if saveErr := config.Save(cfg); saveErr != nil {
					return fmt.Errorf("saving config after fix: %w", saveErr)
				}
			}

			if jsonFlag {
				return renderDocJSON(w, results)
			}

			renderDocResults(w, results)
			return nil
		},
	}

	cmd.Flags().Bool("fix", false, "automatically fix issues that can be fixed")

	return cmd
}

func renderDocResults(w io.Writer, results []checkResult) {
	var passed, warned, errored int

	for _, r := range results {
		var icon string
		switch r.Status {
		case statusPass:
			icon = output.Green(output.SymClean)
			passed++
		case statusWarn:
			icon = output.Yellow(output.SymWarning)
			warned++
		case statusError:
			icon = output.Red(output.SymConflict)
			errored++
		}

		_, _ = fmt.Fprintf(w, "  %s %s: %s\n", icon, r.Name, r.Message)
	}

	_, _ = fmt.Fprintf(w, "\n  %s\n", output.Dim(fmt.Sprintf(
		"%d checks · %d passed · %d warnings · %d errors",
		len(results), passed, warned, errored)))
}

type docJSON struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Fixable bool   `json:"fixable"`
	Fixed   bool   `json:"fixed"`
}

func renderDocJSON(w io.Writer, results []checkResult) error {
	entries := make([]docJSON, len(results))
	for i, r := range results {
		var status string
		switch r.Status {
		case statusPass:
			status = "pass"
		case statusWarn:
			status = "warning"
		case statusError:
			status = "error"
		}
		entries[i] = docJSON{
			Name:    r.Name,
			Status:  status,
			Message: r.Message,
			Fixable: r.Fixable,
			Fixed:   r.Fixed,
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	return nil
}

// shellProfiles returns paths to shell profile files to check for soko shell-init.
func shellProfiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	if runtime.GOOS == "windows" {
		// PowerShell profile path.
		psProfile := os.Getenv("USERPROFILE")
		if psProfile == "" {
			psProfile = home
		}
		return []string{
			filepath.Join(psProfile, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1"),
			filepath.Join(psProfile, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"),
		}
	}

	return []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".config", "fish", "config.fish"),
	}
}
