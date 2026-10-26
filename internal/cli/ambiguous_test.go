package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestIntegration_DuplicateNameCommands(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	first := filepath.Join(base, "work", "api")
	second := filepath.Join(base, "oss", "api")
	initRepo(t, first)
	initRepo(t, second)
	t.Chdir(second)
	cfg := &config.Config{Repos: []config.RepoEntry{
		{Name: "api", Path: first, Tags: []string{"backend"}, Meta: map[string]string{"owner": "alice"}},
		{Name: "api", Path: second, Tags: []string{"public"}, Meta: map[string]string{"owner": "bob"}},
	}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"remove", "api"},
		{"remove", "api", "--json"},
		{"tag", "add", "-r", "api", "new"},
		{"tag", "remove", "-r", "api", "backend"},
		{"tag", "new"},
		{"tag", "remove", "public"},
		{"annotate", "api", "--set", "owner=carol"},
		{"annotate", "api", "--unset", "owner"},
		{"annotate", "api", "--clear"},
		{"annotate", "api"},
		{"annotate", "api", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if err := os.WriteFile(path, before, 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := runSokoErr(t, args...)
			if err == nil {
				t.Errorf("soko %v succeeded: %s", args, out)
			} else {
				for _, want := range []string{"ambiguous", fmt.Sprintf("%q", first), fmt.Sprintf("%q", second), "soko config edit"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error = %q, want %q", err, want)
					}
				}
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("command changed config:\n%s", after)
			}
		})
	}
}

func TestIntegration_DuplicateNameRemoveByPath(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	first := config.RepoEntry{Name: "api", Path: filepath.Join(base, "work", "api"), Tags: []string{"keep"}}
	second := config.RepoEntry{Name: "api", Path: filepath.Join(base, "oss", "api")}
	if err := config.Save(&config.Config{Repos: []config.RepoEntry{first, second}}); err != nil {
		t.Fatal(err)
	}
	runSoko(t, "remove", "--path", second.Path)
	got := loadRegistry(t).Repos
	if len(got) != 1 || got[0].Path != first.Path || strings.Join(got[0].Tags, ",") != "keep" {
		t.Fatalf("remove --path changed wrong entry: %+v", got)
	}
}
