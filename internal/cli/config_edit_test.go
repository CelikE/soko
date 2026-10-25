package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kballard/go-shellquote"

	"github.com/CelikE/soko/internal/config"
)

func TestConfigEdit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		before    string
		after     string
		action    string
		missing   bool
		editor    string
		wantErr   string
		wantSaved bool
	}{
		{name: "changed valid config", before: "repos: []\n", after: "repos: []\n# edited\n", action: "write", wantSaved: true},
		{name: "unchanged config", before: "repos: []\n", action: "noop"},
		{name: "invalid edit", before: "repos: []\n", after: "repos: [\n", action: "write", wantErr: "validating edited config"},
		{name: "unchanged invalid config", before: "repos: [\n", action: "noop", wantErr: "validating edited config"},
		{name: "repair invalid config", before: "repos: [\n", after: "repos: []\n", action: "write", wantSaved: true},
		{name: "create config", missing: true, after: "repos: []\n", action: "write", wantSaved: true},
		{name: "create empty config", missing: true, action: "write", wantSaved: true},
		{name: "quit without creating config", missing: true, action: "noop"},
		{name: "editor failure", before: "repos: []\n", action: "fail", wantErr: "opening editor"},
		{name: "malformed editor", before: "repos: []\n", editor: "'unterminated", wantErr: "parsing EDITOR"},
		{name: "whitespace editor", before: "repos: []\n", editor: " \t ", wantErr: "EDITOR must not be empty"},
		{name: "empty executable", before: "repos: []\n", editor: "'' --wait", wantErr: "EDITOR must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path, err := config.Path()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.before), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("SOKO_CONFIG_EDIT_HELPER", tt.action)
			t.Setenv("SOKO_CONFIG_EDIT_CONTENT", tt.after)
			editor := tt.editor
			if editor == "" {
				editor = shellquote.Join(executable, "-test.run=^TestConfigEditHelperProcess$", "--", "--wait", "argument with spaces", "")
			}
			t.Setenv("EDITOR", editor)

			var stdout, stderr bytes.Buffer
			cmd := NewRootCmd("test")
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"config", "edit"})
			err = cmd.Execute()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("config edit: %v", err)
			}
			if saved := strings.Contains(stderr.String(), "config saved"); saved != tt.wantSaved {
				t.Errorf("stderr = %q, want saved confirmation: %v", stderr.String(), tt.wantSaved)
			}
			if stdout.Len() != 0 {
				t.Errorf("unexpected stdout: %q", stdout.String())
			}

			data, err := os.ReadFile(path)
			if tt.missing && tt.action == "noop" {
				if !os.IsNotExist(err) {
					t.Fatalf("config should remain absent, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := tt.before
			if tt.action == "write" {
				want = tt.after
			}
			if string(data) != want {
				t.Errorf("config = %q, want %q", data, want)
			}
		})
	}
}

// TestConfigEditHelperProcess acts as a real editor subprocess, including argv
// verification, without depending on an installed editor or platform shell.
func TestConfigEditHelperProcess(t *testing.T) {
	action := os.Getenv("SOKO_CONFIG_EDIT_HELPER")
	if action == "" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing argument separator")
	}
	args := os.Args[separator+1:]
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(args, []string{"--wait", "argument with spaces", "", path}) {
		t.Fatalf("editor arguments = %q", args)
	}
	switch action {
	case "write":
		if err := os.WriteFile(path, []byte(os.Getenv("SOKO_CONFIG_EDIT_CONTENT")), 0o600); err != nil {
			t.Fatal(err)
		}
	case "fail":
		os.Exit(1)
	}
	os.Exit(0)
}
