package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/cli"
	"github.com/CelikE/soko/internal/config"
)

func TestAlias_SetAndList(t *testing.T) {
	testEnv(t)

	runSoko(t, "alias", "set", "morning", "sync --tag work")
	runSoko(t, "alias", "set", "deploy", "exec --tag production -- make deploy")

	out := runSoko(t, "alias", "list")
	if !strings.Contains(out, "morning") {
		t.Errorf("alias list missing 'morning': %s", out)
	}
	if !strings.Contains(out, "sync --tag work") {
		t.Errorf("alias list missing command: %s", out)
	}
	if !strings.Contains(out, "deploy") {
		t.Errorf("alias list missing 'deploy': %s", out)
	}
}

func TestAlias_Remove(t *testing.T) {
	testEnv(t)

	runSoko(t, "alias", "set", "morning", "sync --tag work")
	out := runSoko(t, "alias", "remove", "morning")
	if !strings.Contains(out, "removed") {
		t.Errorf("alias remove output = %q, want 'removed'", out)
	}

	out = runSoko(t, "alias", "list")
	if strings.Contains(out, "morning") {
		t.Errorf("alias list should not contain 'morning' after removal: %s", out)
	}
}

func TestAlias_RemoveNonExistent(t *testing.T) {
	testEnv(t)

	var stderr bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"alias", "remove", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error removing non-existent alias")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err.Error())
	}
}

func TestAlias_SetInvalidName(t *testing.T) {
	testEnv(t)

	var stderr bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"alias", "set", "has space", "status"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for alias name with spaces")
	}
}

func TestAlias_SetEmptyCommand(t *testing.T) {
	testEnv(t)

	var stderr bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"alias", "set", "empty", "  "})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for empty alias command")
	}
}

func TestAlias_ListJSON(t *testing.T) {
	testEnv(t)

	runSoko(t, "alias", "set", "morning", "sync --tag work")
	runSoko(t, "alias", "set", "tidy", "exec --tag go -- go mod tidy")

	out := runSoko(t, "alias", "list", "--json")

	var entries []struct {
		Name    string `json:"name"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(entries))
	}
	// Sorted alphabetically.
	if entries[0].Name != "morning" {
		t.Errorf("first alias = %q, want 'morning'", entries[0].Name)
	}
	if entries[1].Name != "tidy" {
		t.Errorf("second alias = %q, want 'tidy'", entries[1].Name)
	}
}

func TestAlias_ListEmpty(t *testing.T) {
	testEnv(t)

	out := runSoko(t, "alias", "list")
	if !strings.Contains(out, "no aliases") {
		t.Errorf("alias list output = %q, want 'no aliases'", out)
	}
}

func TestAlias_OverwriteExisting(t *testing.T) {
	testEnv(t)

	runSoko(t, "alias", "set", "morning", "sync --tag work")
	runSoko(t, "alias", "set", "morning", "status")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if cfg.Aliases["morning"] != "status" {
		t.Errorf("alias morning = %q, want 'status'", cfg.Aliases["morning"])
	}
}

func TestAlias_ConfigPersistence(t *testing.T) {
	cfgDir := testEnv(t)

	runSoko(t, "alias", "set", "morning", "sync --tag work")

	// Verify config file on disk.
	cfgPath := filepath.Join(cfgDir, "soko", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), "morning") {
		t.Errorf("config file missing alias 'morning': %s", string(data))
	}
}
