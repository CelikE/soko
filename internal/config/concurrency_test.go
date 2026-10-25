package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func waitConfigFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestConfigWriterProcess(t *testing.T) {
	name := os.Getenv("SOKO_CONFIG_WRITER")
	if name == "" {
		return
	}
	path := os.Getenv("SOKO_CONFIG_FILE")
	operation := os.Getenv("SOKO_CONFIG_OPERATION")
	if operation == "hold" {
		lock, err := openConfigLock(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := tryConfigLock(lock); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".ready-"+name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		waitConfigFile(t, path+".release-"+name)
		_ = lock.Close()
		os.Exit(0)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".ready-"+name, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitConfigFile(t, path+".release-"+name)
	if operation == "discover" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		entry, err := RegisterDiscovered(ctx, "same", filepath.Join(filepath.Dir(path), name))
		cancel()
		if err != nil || entry == nil {
			t.Fatalf("discovery failed: %+v, %v", entry, err)
		}
		os.Exit(0)
	}
	if cfg.Aliases == nil {
		cfg.Aliases = map[string]string{}
	}
	cfg.Aliases[name] = "status"
	if err := SaveTo(cfg, path); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(23)
	}
	os.Exit(0)
}

func startConfigProcess(ctx context.Context, t *testing.T, path, name, operation string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestConfigWriterProcess$")
	cmd.Env = append(os.Environ(), "SOKO_CONFIG_WRITER="+name, "SOKO_CONFIG_FILE="+path, "SOKO_CONFIG_OPERATION="+operation)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd, stderr
}

func TestSaveToRejectsStaleCrossProcessSnapshot(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing-%v", existing), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if existing {
				if err := SaveTo(&Config{}, path); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			a, aError := startConfigProcess(ctx, t, path, "A", "write")
			b, bError := startConfigProcess(ctx, t, path, "B", "write")
			waitConfigFile(t, path+".ready-A")
			waitConfigFile(t, path+".ready-B")
			if err := os.WriteFile(path+".release-A", nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := a.Wait(); err != nil {
				t.Fatalf("first writer: %v; %s", err, aError.String())
			}
			if err := os.WriteFile(path+".release-B", nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := b.Wait(); err == nil {
				t.Error("stale writer reported success instead of conflict")
			} else if !strings.Contains(bError.String(), "config changed") {
				t.Fatalf("unexpected stale-writer error: %v; %s", err, bError.String())
			}
			cfg, err := LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Aliases["A"] != "status" || cfg.Aliases["B"] != "" {
				t.Errorf("completed update lost: aliases = %v", cfg.Aliases)
			}
		})
	}
}

func TestSaveToSnapshotCopiesAndExistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	copyCfg := *cfg
	cfg.Aliases = map[string]string{"first": "status"}
	if err := SaveTo(cfg, path); err != nil {
		t.Fatal(err)
	}
	copyCfg.Aliases = map[string]string{"stale": "diff"}
	if err := SaveTo(&copyCfg, path); !errors.Is(err, ErrConflict) {
		t.Fatalf("copy acquired newer baseline: %v", err)
	}
	cfg.Aliases["second"] = "diff"
	if err := SaveTo(cfg, path); err != nil {
		t.Fatalf("successful instance did not advance baseline: %v", err)
	}
	if err := SaveTo(&Config{}, path); !errors.Is(err, ErrConflict) {
		t.Fatalf("unobserved replacement allowed: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := SaveTo(cfg, path); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale snapshot recreated removed config: %v", err)
	}
	missing, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTo(missing, path); !errors.Is(err, ErrConflict) {
		t.Fatalf("empty existing file confused with absence: %v", err)
	}
}

func TestConfigLockBusyAndProcessExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &Config{}
	if err := SaveTo(cfg, path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	holder, _ := startConfigProcess(ctx, t, path, "holder", "hold")
	waitConfigFile(t, path+".ready-holder")
	lockInfo, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Aliases = map[string]string{"after": "status"}
	if err := SaveTo(cfg, path); !errors.Is(err, ErrBusy) {
		t.Fatalf("other process's lock ignored: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("busy save changed config: %s, %v", after, err)
	}
	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = holder.Wait()
	if err := SaveTo(cfg, path); err != nil {
		t.Fatalf("dead process stranded lock: %v", err)
	}
	afterInfo, err := os.Stat(path + ".lock")
	if err != nil || !os.SameFile(lockInfo, afterInfo) {
		t.Fatalf("lock inode replaced: %v", err)
	}
	if runtime.GOOS != "windows" {
		for _, file := range []string{path, path + ".lock"} {
			info, err := os.Stat(file)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Errorf("config/lock permissions: %v, %v", info, err)
			}
		}
	}
}

func TestRegisterDiscoveredProcesses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(&Config{Discover: &DiscoverConfig{Enabled: true, Tags: []string{"auto"}}}); err != nil {
		t.Fatal(err)
	}
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := openConfigLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := tryConfigLock(lock); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a, aError := startConfigProcess(ctx, t, path, "A", "discover")
	b, bError := startConfigProcess(ctx, t, path, "B", "discover")
	waitConfigFile(t, path+".ready-A")
	waitConfigFile(t, path+".ready-B")
	for _, name := range []string{"A", "B"} {
		if err := os.WriteFile(path+".release-"+name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = lock.Close()
	if err := a.Wait(); err != nil {
		t.Fatalf("first discovery: %v, %s", err, aError.String())
	}
	if err := b.Wait(); err != nil {
		t.Fatalf("second discovery: %v, %s", err, bError.String())
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 2 || cfg.Repos[0].Name == cfg.Repos[1].Name {
		t.Fatalf("discovery lost registration/alias ownership: %+v", cfg.Repos)
	}
	for _, entry := range cfg.Repos {
		if len(entry.Tags) != 1 || entry.Tags[0] != "auto" {
			t.Errorf("discovery lost current policy tags: %+v", entry)
		}
	}
}

func TestRegisterDiscoveredCancellation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(&Config{Discover: &DiscoverConfig{Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := openConfigLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := tryConfigLock(lock); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := RegisterDiscovered(ctx, "blocked", filepath.Dir(path)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait ignored cancellation: %v", err)
	}
	cfg, err := Load()
	if err != nil || len(cfg.Repos) != 0 {
		t.Fatalf("canceled discovery changed config: %+v, %v", cfg, err)
	}
}

func TestSaveToSequentialSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for _, name := range []string{"A", "B"} {
		cfg, err := LoadFrom(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Aliases == nil {
			cfg.Aliases = map[string]string{}
		}
		cfg.Aliases[name] = "status"
		if err := SaveTo(cfg, path); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Aliases) != 2 {
		t.Errorf("sequential updates lost: %v", cfg.Aliases)
	}
}

func TestSaveToInitialSymlinkAncestor(t *testing.T) {
	root := t.TempDir()
	realDir, alias := filepath.Join(root, "real"), filepath.Join(root, "alias")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(alias, "not-yet-created", "soko", "config.yaml")
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Aliases = map[string]string{"first": "status"}
	if err := SaveTo(cfg, path); err != nil {
		t.Fatal(err)
	}
	cfg.Aliases["second"] = "diff"
	if err := SaveTo(cfg, path); err != nil {
		t.Fatalf("same snapshot falsely conflicted after directory creation: %v", err)
	}
}

func TestSaveToSymlinkUsesTargetLock(t *testing.T) {
	root := t.TempDir()
	target, alias := filepath.Join(root, "target.yaml"), filepath.Join(root, "config.yaml")
	if err := SaveTo(&Config{}, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cfg, err := LoadFrom(alias)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := openConfigLock(target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := tryConfigLock(lock); err != nil {
		t.Fatal(err)
	}
	cfg.Aliases = map[string]string{"saved": "status"}
	if err := SaveTo(cfg, alias); !errors.Is(err, ErrBusy) {
		t.Fatalf("symlink bypassed target lock: %v", err)
	}
	_ = lock.Close()
	if err := SaveTo(cfg, alias); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(alias); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("save replaced config symlink: %v, %v", info, err)
	}
}
