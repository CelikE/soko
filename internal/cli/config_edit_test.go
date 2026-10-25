package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
		{name: "concurrent writer", before: "repos: []\n", after: "repos: []\n# edited\n", action: "concurrent", wantErr: "config changed"},
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
			copies, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".config-edit-*", "config.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if tt.action == "write" && tt.wantErr != "" || tt.action == "concurrent" {
				if len(copies) != 1 {
					t.Fatalf("failed edit did not preserve recovery copy: %v", copies)
				}
				data, err := os.ReadFile(copies[0])
				if err != nil || string(data) != tt.after {
					t.Errorf("edited recovery copy = %q, %v; want %q", data, err, tt.after)
				}
			} else if tt.wantErr == "" && len(copies) != 0 {
				t.Errorf("successful edit left temporary copies: %v", copies)
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
			if tt.action == "concurrent" {
				cfg, err := config.Load()
				if err != nil || cfg.Aliases["concurrent"] != "status" {
					t.Fatalf("editor erased completed update: %s, %v", data, err)
				}
				return
			}
			want := tt.before
			if tt.action == "write" && tt.wantErr == "" {
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
	if len(args) != 4 || !slices.Equal(args[:3], []string{"--wait", "argument with spaces", ""}) || filepath.Base(args[3]) != "config.yaml" {
		t.Fatalf("editor arguments = %q", args)
	}
	path := args[3]
	switch action {
	case "block":
		if err := os.WriteFile(path, []byte(os.Getenv("SOKO_CONFIG_EDIT_CONTENT")), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("SOKO_CONFIG_EDIT_READY"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Second)
		}
	case "concurrent":
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		cfg.Aliases = map[string]string{"concurrent": "status"}
		if err := config.Save(cfg); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(os.Getenv("SOKO_CONFIG_EDIT_CONTENT")), 0o600); err != nil {
			t.Fatal(err)
		}
	case "write":
		if err := os.WriteFile(path, []byte(os.Getenv("SOKO_CONFIG_EDIT_CONTENT")), 0o600); err != nil {
			t.Fatal(err)
		}
	case "fail":
		os.Exit(1)
	}
	os.Exit(0)
}

func TestConfigEditCancellationPreservesConcurrentWriter(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.Save(&config.Config{}); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "editor-ready")
	edited := "repos: []\n# unfinished editor work\n"
	t.Setenv("SOKO_CONFIG_EDIT_HELPER", "block")
	t.Setenv("SOKO_CONFIG_EDIT_CONTENT", edited)
	t.Setenv("SOKO_CONFIG_EDIT_READY", ready)
	t.Setenv("EDITOR", shellquote.Join(executable, "-test.run=^TestConfigEditHelperProcess$", "--", "--wait", "argument with spaces", ""))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := NewRootCmd("test")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"config", "edit"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("editor did not reach barrier")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Aliases = map[string]string{"concurrent": "status"}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("editor held commit lock during user interaction: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "opening editor") {
			t.Fatalf("canceled editor reported success: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancellation did not stop editor")
	}
	cfg, err = config.Load()
	if err != nil || cfg.Aliases["concurrent"] != "status" {
		t.Fatalf("canceled editor lost concurrent update: %+v, %v", cfg, err)
	}
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	copies, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".config-edit-*", "config.yaml"))
	if err != nil || len(copies) != 1 {
		t.Fatalf("canceled editor lost recovery copy: %v, %v", copies, err)
	}
	data, err := os.ReadFile(copies[0])
	if err != nil || string(data) != edited {
		t.Errorf("canceled editor recovery data = %q, %v", data, err)
	}
}
