package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

// wellKnownMetaKeys drive richer rendering and completion. The metadata map is
// open — any key works — but these four get dedicated columns in --list.
var wellKnownMetaKeys = []string{"owner", "status", "priority", "note"}

// newAnnotateCmd creates the cobra command for soko annotate.
func newAnnotateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "annotate [repo]",
		Short: "Attach freeform metadata (owner, status, priority, note) to a repo",
		Long: `Attach freeform metadata to a repo beyond boolean tags, then filter and
display by it. Owner, status, priority, and note are well-known keys, but the
map is open — any key=value works.

  soko annotate api                          show metadata for a repo
  soko annotate api --set owner=alice        set a key (repeatable)
  soko annotate api --unset owner            remove a key (repeatable)
  soko annotate api --clear                  remove all metadata
  soko annotate --list                       every repo that has metadata

Filter other commands by metadata with --meta key=value (repeatable, AND):

  soko list --meta status=active
  soko status --meta priority=high --meta status=active`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE:              runAnnotate,
	}

	cmd.Flags().StringSlice("set", nil, "set a metadata key (key=value, repeatable)")
	cmd.Flags().StringSlice("unset", nil, "remove a metadata key (repeatable)")
	cmd.Flags().Bool("clear", false, "remove all metadata from the repo")
	cmd.Flags().Bool("list", false, "list every repo that has metadata")
	cmd.Flags().StringP("repo", "r", "", "target repo name (defaults to current directory)")
	_ = cmd.RegisterFlagCompletionFunc("repo", repoNameCompletionFunc())
	_ = cmd.RegisterFlagCompletionFunc("set", metaKeyCompletionFunc())
	_ = cmd.RegisterFlagCompletionFunc("unset", metaKeyCompletionFunc())

	return cmd
}

func runAnnotate(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	setVals, _ := cmd.Flags().GetStringSlice("set")
	unsetVals, _ := cmd.Flags().GetStringSlice("unset")
	clearFlag, _ := cmd.Flags().GetBool("clear")
	listFlag, _ := cmd.Flags().GetBool("list")
	jsonFlag, _ := cmd.Flags().GetBool("json")

	// --set, --unset, and --clear are mutually exclusive.
	mutations := 0
	for _, on := range []bool{len(setVals) > 0, len(unsetVals) > 0, clearFlag} {
		if on {
			mutations++
		}
	}
	if mutations > 1 {
		return fmt.Errorf("--set, --unset, and --clear are mutually exclusive")
	}

	if listFlag {
		return runAnnotateList(jsonFlag, w)
	}

	repoName, err := annotateTarget(cmd, args)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	switch {
	case clearFlag:
		return runAnnotateClear(cfg, repoName, w)
	case len(unsetVals) > 0:
		return runAnnotateUnset(cfg, repoName, unsetVals, w)
	case len(setVals) > 0:
		return runAnnotateSet(cfg, repoName, setVals, w)
	default:
		return runAnnotateShow(cfg, repoName, jsonFlag, w)
	}
}

// annotateTarget resolves the repo from a positional arg, the -r flag, or the
// current working directory (in that order).
func annotateTarget(cmd *cobra.Command, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	return resolveRepoName(cmd)
}

func runAnnotateSet(cfg *config.Config, repoName string, setVals []string, w io.Writer) error {
	for _, kv := range setVals {
		key, value, err := parseKeyValue("--set", kv)
		if err != nil {
			return err
		}
		if cfg, err = config.SetMeta(cfg, repoName, key, value); err != nil {
			return notFoundOrErr(err, repoName)
		}
	}
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	output.Confirm(w, fmt.Sprintf("annotated %s (%d %s set)",
		repoName, len(setVals), output.Plural(len(setVals), "key")))
	return nil
}

func runAnnotateUnset(cfg *config.Config, repoName string, unsetVals []string, w io.Writer) error {
	for _, key := range unsetVals {
		var err error
		if cfg, err = config.UnsetMeta(cfg, repoName, key); err != nil {
			return notFoundOrErr(err, repoName)
		}
	}
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	output.Confirm(w, fmt.Sprintf("unannotated %s (removed %s)",
		repoName, strings.Join(unsetVals, ", ")))
	return nil
}

func runAnnotateClear(cfg *config.Config, repoName string, w io.Writer) error {
	var err error
	if cfg, err = config.ClearMeta(cfg, repoName); err != nil {
		return notFoundOrErr(err, repoName)
	}
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	output.Confirm(w, fmt.Sprintf("cleared metadata from %s", repoName))
	return nil
}

func runAnnotateShow(cfg *config.Config, repoName string, jsonOut bool, w io.Writer) error {
	entry, ok := findRepoByName(cfg, repoName)
	if !ok {
		return fmt.Errorf("not found: %s", repoName)
	}

	if jsonOut {
		meta := entry.Meta
		if meta == nil {
			meta = map[string]string{}
		}
		return output.RenderJSON(w, annotateJSON{Name: entry.Name, Path: entry.Path, Meta: meta})
	}

	_, _ = fmt.Fprintf(w, "  %s\n", entry.Name)
	if len(entry.Meta) == 0 {
		output.Info(w, "no metadata")
		return nil
	}

	keys := make([]string, 0, len(entry.Meta))
	for k := range entry.Meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	keyWidth := 0
	for _, k := range keys {
		if len(k) > keyWidth {
			keyWidth = len(k)
		}
	}
	keyWidth += 2
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "  %-*s %s\n", keyWidth, k, entry.Meta[k])
	}
	return nil
}

func runAnnotateList(jsonOut bool, w io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	var annotated []config.RepoEntry
	for _, r := range cfg.Repos {
		if len(r.Meta) > 0 {
			annotated = append(annotated, r)
		}
	}

	if jsonOut {
		entries := make([]annotateJSON, len(annotated))
		for i, r := range annotated {
			entries[i] = annotateJSON{Name: r.Name, Path: r.Path, Meta: r.Meta}
		}
		return output.RenderJSON(w, entries)
	}

	if len(annotated) == 0 {
		output.Info(w, "no repos annotated")
		return nil
	}

	renderAnnotateList(w, annotated)
	return nil
}

// renderAnnotateList prints the well-known metadata columns for every annotated
// repo, with a trailing count.
func renderAnnotateList(w io.Writer, repos []config.RepoEntry) {
	headers := append([]string{"NAME"}, upperAll(wellKnownMetaKeys)...)
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	cell := func(r config.RepoEntry, col int) string {
		if col == 0 {
			return r.Name
		}
		if v, ok := r.Meta[wellKnownMetaKeys[col-1]]; ok && v != "" {
			return v
		}
		return "-"
	}
	for _, r := range repos {
		for i := range headers {
			if c := len(cell(r, i)); c > widths[i] {
				widths[i] = c
			}
		}
	}
	for i := range widths {
		widths[i] += 2
	}

	var b strings.Builder
	b.WriteString("  ")
	for i, h := range headers {
		_, _ = fmt.Fprintf(&b, "%-*s", widths[i], h)
	}
	header := strings.TrimRight(b.String(), " ")
	_, _ = fmt.Fprintln(w, output.Dim(header))
	_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

	for _, r := range repos {
		var row strings.Builder
		row.WriteString("  ")
		for i := range headers {
			_, _ = fmt.Fprintf(&row, "%-*s", widths[i], cell(r, i))
		}
		_, _ = fmt.Fprintln(w, strings.TrimRight(row.String(), " "))
	}

	_, _ = fmt.Fprintf(w, "\n  %s\n", output.Dim(fmt.Sprintf(
		"%d %s annotated", len(repos), output.Plural(len(repos), "repo"))))
}

// parseKeyValue splits a "key=value" string. Both a missing "=" and an empty
// key are usage errors; an empty value is allowed.
func parseKeyValue(flag, s string) (key, value string, err error) {
	key, value, ok := strings.Cut(s, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return "", "", fmt.Errorf("invalid %s %q: expected key=value", flag, s)
	}
	return key, value, nil
}

// parseMetaConstraints parses repeated "key=value" --meta flags into a
// constraint map. FilterByMeta normalizes the keys, so this keeps them verbatim.
func parseMetaConstraints(metaArgs []string) (map[string]string, error) {
	constraints := make(map[string]string, len(metaArgs))
	for _, kv := range metaArgs {
		key, value, err := parseKeyValue("--meta", kv)
		if err != nil {
			return nil, err
		}
		constraints[key] = value
	}
	return constraints, nil
}

// findRepoByName returns the entry with an exact name match.
func findRepoByName(cfg *config.Config, name string) (config.RepoEntry, bool) {
	for _, r := range cfg.Repos {
		if r.Name == name {
			return r, true
		}
	}
	return config.RepoEntry{}, false
}

// notFoundOrErr maps ErrRepoNotFound to the "not found: <name>" message used by
// the other registry commands, and wraps anything else.
func notFoundOrErr(err error, name string) error {
	if errors.Is(err, config.ErrRepoNotFound) {
		return fmt.Errorf("not found: %s", name)
	}
	return fmt.Errorf("annotating repo: %w", err)
}

// metaKeyCompletionFunc completes the well-known metadata keys.
func metaKeyCompletionFunc() cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		completions := make([]cobra.Completion, len(wellKnownMetaKeys))
		copy(completions, wellKnownMetaKeys)
		return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
	}
}

func upperAll(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = strings.ToUpper(k)
	}
	return out
}

type annotateJSON struct {
	Name string            `json:"name"`
	Path string            `json:"path"`
	Meta map[string]string `json:"meta"`
}
