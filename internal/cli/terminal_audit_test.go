//go:build !windows

package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"gopkg.in/yaml.v3"

	"github.com/CelikE/soko/internal/journal"
)

// terminalAudit runs the real binary with isolated configuration and captures
// the complete PTY transcript. Waits are bounded and synchronize on rendered
// output, rather than assuming a particular speed for Git or the terminal.
type terminalAudit struct {
	t         *testing.T
	root, bin string
	env       []string
}

func newTerminalAudit(t *testing.T) *terminalAudit {
	t.Helper()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	a := &terminalAudit{t: t, root: root, bin: filepath.Join(root, "soko")}
	for _, dir := range []string{"home", "config", "cache"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "SOKO_") || strings.HasPrefix(key, "GH_") || strings.HasPrefix(key, "GITHUB_") || key == "HOME" || key == "XDG_CONFIG_HOME" || key == "XDG_CACHE_HOME" || key == "TERM" || key == "NO_COLOR" {
			continue
		}
		a.env = append(a.env, e)
	}
	a.env = append(a.env, "HOME="+filepath.Join(root, "home"), "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "XDG_CACHE_HOME="+filepath.Join(root, "cache"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "TERM=xterm-256color", "NO_COLOR=1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", a.bin, "../../cmd/soko")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build soko: %v\n%s", err, out)
	}
	return a
}

func (a *terminalAudit) run(dir, name string, args ...string) string {
	a.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = dir, a.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		a.t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (a *terminalAudit) git(dir string, args ...string) string {
	return a.run(dir, "git", args...)
}

func (a *terminalAudit) fixture() (checkout, before, after string) {
	a.t.Helper()
	remote := filepath.Join(a.root, "alpha.git")
	seed := filepath.Join(a.root, "seed")
	checkout = filepath.Join(a.root, "alpha")
	a.git(a.root, "init", "--bare", remote)
	a.git(a.root, "init", "-b", "main", seed)
	a.git(seed, "config", "user.name", "Test")
	a.git(seed, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(seed, "tracked"), []byte("before\n"), 0o600); err != nil {
		a.t.Fatal(err)
	}
	a.git(seed, "add", "tracked")
	a.git(seed, "commit", "-m", "before")
	a.git(seed, "remote", "add", "origin", remote)
	a.git(seed, "push", "-u", "origin", "main")
	a.git(a.root, "clone", "--branch", "main", remote, checkout)
	a.run(checkout, a.bin, "init")
	before = a.git(checkout, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(seed, "tracked"), []byte("after\n"), 0o600); err != nil {
		a.t.Fatal(err)
	}
	a.git(seed, "commit", "-am", "after")
	a.git(seed, "push")
	after = a.git(seed, "rev-parse", "HEAD")
	return checkout, before, after
}

type terminalSession struct {
	t          *testing.T
	file       *os.File
	cmd        *exec.Cmd
	mu         sync.Mutex
	transcript bytes.Buffer
	changed    chan struct{}
	done       chan error
	readDone   chan struct{}
}

func (a *terminalAudit) start(args ...string) *terminalSession {
	a.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, a.bin, args...)
	cmd.Dir, cmd.Env = a.root, a.env
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 180})
	if err != nil {
		cancel()
		a.t.Fatal(err)
	}
	s := &terminalSession{t: a.t, file: f, cmd: cmd, changed: make(chan struct{}, 1), done: make(chan error, 1), readDone: make(chan struct{})}
	go func() {
		defer close(s.readDone)
		buf := make([]byte, 4096)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				s.mu.Lock()
				_, _ = s.transcript.Write(buf[:n])
				s.mu.Unlock()
				select {
				case s.changed <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { s.done <- cmd.Wait() }()
	a.t.Cleanup(func() {
		cancel()
		_ = f.Close()
		select {
		case <-s.readDone:
		case <-time.After(2 * time.Second):
			a.t.Error("terminal reader did not stop")
		}
		s.mu.Lock()
		a.t.Logf("PTY transcript for soko %v:\n%s", args, s.transcript.String())
		s.mu.Unlock()
	})
	return s
}

func (s *terminalSession) mark() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transcript.Len()
}

func (s *terminalSession) send(keys string) int {
	s.t.Helper()
	mark := s.mark()
	if _, err := s.file.WriteString(keys); err != nil {
		s.t.Fatal(err)
	}
	return mark
}

func (s *terminalSession) wait(mark int, text string) {
	s.t.Helper()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		s.mu.Lock()
		out := s.transcript.String()
		s.mu.Unlock()
		if strings.Contains(out[mark:], text) {
			return
		}
		select {
		case <-s.changed:
		case <-s.readDone:
			s.mu.Lock()
			out = s.transcript.String()
			s.mu.Unlock()
			if strings.Contains(out[mark:], text) {
				return
			}
			s.t.Fatalf("terminal closed before %q\n%s", text, out)
		case <-timer.C:
			s.t.Fatalf("timed out waiting for %q\n%s", text, out)
		}
	}
}

func (a *terminalAudit) readJournal() journal.Journal {
	a.t.Helper()
	data, err := os.ReadFile(filepath.Join(a.root, "config", "soko", "journal.yaml"))
	if err != nil {
		a.t.Fatal(err)
	}
	var j journal.Journal
	if err := yaml.Unmarshal(data, &j); err != nil {
		a.t.Fatal(err)
	}
	return j
}

func TestTerminalAuditPullUndoAndDirtyRetry(t *testing.T) {
	a := newTerminalAudit(t)
	checkout, before, after := a.fixture()
	s := a.start("ui")
	s.wait(0, "› alpha")
	s.wait(s.send("P"), "pull alpha? [y/N]")
	s.wait(s.send("y"), "alpha: pulled")
	if got := a.git(checkout, "rev-parse", "HEAD"); got != after {
		t.Fatalf("pull HEAD = %s, want %s", got, after)
	}
	j := a.readJournal()
	if len(j.Entries) != 1 || len(j.Entries[0].Pulls) != 1 {
		t.Fatalf("journal = %+v", j.Entries)
	}
	p := j.Entries[0].Pulls[0]
	if p.SHA != before || p.PostSHA != after || p.Branch != "refs/heads/main" || p.Path != checkout || p.Repo != "alpha" {
		t.Fatalf("pull journal = %+v", p)
	}
	path := filepath.Join(checkout, "tracked")
	if err := os.WriteFile(path, []byte("user work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.wait(s.send("u"), "undo last pull? [y/N]")
	s.wait(s.send("y"), "error:")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "user work\n" {
		t.Fatalf("dirty file = %q, err %v", data, err)
	}
	if got := a.git(checkout, "rev-parse", "HEAD"); got != after {
		t.Fatalf("ref changed on refused undo: %s", got)
	}
	if len(a.readJournal().Entries) != 1 {
		t.Fatal("refused undo lost journal")
	}
	a.git(checkout, "restore", "tracked")
	s.wait(s.send("u"), "undo last pull? [y/N]")
	s.wait(s.send("y"), "undid pull (1 repo reset)")
	if got := a.git(checkout, "rev-parse", "HEAD"); got != before {
		t.Fatalf("undo HEAD = %s, want %s", got, before)
	}
	if len(a.readJournal().Entries) != 0 {
		t.Fatal("successful undo retained journal")
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "before\n" {
		t.Fatalf("restored file = %q, err %v", data, err)
	}
	// Each refusal uses a new UI-created journal entry, not a fabricated log.
	for _, scenario := range []string{"untracked", "branch", "newer HEAD"} {
		t.Run(scenario, func(t *testing.T) {
			s.wait(s.send("P"), "pull alpha? [y/N]")
			s.wait(s.send("y"), "alpha: pulled")
			var expectedHead = after
			var expectedBranch = "main"
			var message string
			switch scenario {
			case "untracked":
				if err := os.WriteFile(filepath.Join(checkout, "untracked"), []byte("keep me\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				message = "working tree is dirty"
			case "branch":
				a.git(checkout, "switch", "-c", "other")
				expectedBranch = "other"
				message = "branch changed since pull"
			case "newer HEAD":
				a.git(checkout, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "newer")
				expectedHead = a.git(checkout, "rev-parse", "HEAD")
				message = "HEAD changed since pull"
			}
			s.wait(s.send("u"), "undo last pull? [y/N]")
			s.wait(s.send("y"), message)
			if got := a.git(checkout, "rev-parse", "HEAD"); got != expectedHead {
				t.Fatalf("refused undo changed HEAD: %s", got)
			}
			if got := a.git(checkout, "branch", "--show-current"); got != expectedBranch {
				t.Fatalf("refused undo changed branch: %s", got)
			}
			if len(a.readJournal().Entries) != 1 {
				t.Fatal("refused undo lost journal")
			}
			switch scenario {
			case "untracked":
				data, err := os.ReadFile(filepath.Join(checkout, "untracked"))
				if err != nil || string(data) != "keep me\n" {
					t.Fatalf("untracked content = %q, err %v", data, err)
				}
				if err := os.Remove(filepath.Join(checkout, "untracked")); err != nil {
					t.Fatal(err)
				}
			case "branch":
				a.git(checkout, "switch", "main")
			case "newer HEAD":
				a.git(checkout, "reset", "--keep", after)
			}
			s.wait(s.send("u"), "undo last pull? [y/N]")
			s.wait(s.send("y"), "undid pull (1 repo reset)")
			if got := a.git(checkout, "rev-parse", "HEAD"); got != before {
				t.Fatalf("retry HEAD = %s, want %s", got, before)
			}
		})
	}
	s.send("q")
	select {
	case err := <-s.done:
		if err != nil {
			t.Fatal(fmt.Errorf("ui exit: %w", err))
		}
	case <-time.After(15 * time.Second):
		t.Fatal("ui did not exit")
	}
}

func TestTerminalAuditBatchUndoRetry(t *testing.T) {
	a := newTerminalAudit(t)
	alpha, before, after := a.fixture()
	remote := filepath.Join(a.root, "beta.git")
	beta := filepath.Join(a.root, "beta")
	a.git(a.root, "clone", "--bare", filepath.Join(a.root, "alpha.git"), remote)
	a.git(a.root, "clone", "--branch", "main", remote, beta)
	a.git(beta, "reset", "--keep", before)
	a.run(beta, a.bin, "init")
	s := a.start("ui")
	s.wait(0, "› alpha")
	s.wait(s.send("*"), "•")
	s.wait(s.send("P"), "pull 2 repos (alpha, beta)? [y/N]")
	s.wait(s.send("y"), "pulled 2")
	j := a.readJournal()
	if len(j.Entries) != 1 || len(j.Entries[0].Pulls) != 2 {
		t.Fatalf("batch journal = %+v", j.Entries)
	}
	for _, p := range j.Entries[0].Pulls {
		if p.SHA != before || p.PostSHA != after || p.Branch != "refs/heads/main" {
			t.Fatalf("batch pull journal = %+v", p)
		}
	}
	path := filepath.Join(beta, "tracked")
	if err := os.WriteFile(path, []byte("user work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.wait(s.send("u"), "undo last pull? [y/N]")
	s.wait(s.send("y"), "working tree is dirty")
	if got := a.git(alpha, "rev-parse", "HEAD"); got != before {
		t.Fatalf("first repo not undone: %s", got)
	}
	if got := a.git(beta, "rev-parse", "HEAD"); got != after {
		t.Fatalf("dirty repo changed: %s", got)
	}
	j = a.readJournal()
	if len(j.Entries) != 1 || len(j.Entries[0].Pulls) != 2 || !j.Entries[0].Pulls[0].Undone || j.Entries[0].Pulls[1].Undone {
		t.Fatalf("retry checkpoint = %+v", j.Entries)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "user work\n" {
		t.Fatalf("user work = %q, err %v", data, err)
	}
	// A new commit in the already-undone repo must not be revisited on retry.
	a.git(alpha, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "after checkpoint")
	newHead := a.git(alpha, "rev-parse", "HEAD")
	a.git(beta, "restore", "tracked")
	s.wait(s.send("u"), "undo last pull? [y/N]")
	s.wait(s.send("y"), "undid pull (1 repo reset)")
	if got := a.git(alpha, "rev-parse", "HEAD"); got != newHead {
		t.Fatalf("retry changed checkpointed repo: %s", got)
	}
	if got := a.git(beta, "rev-parse", "HEAD"); got != before {
		t.Fatalf("retry did not undo beta: %s", got)
	}
	if len(a.readJournal().Entries) != 0 {
		t.Fatal("successful retry retained journal")
	}
	s.send("q")
}

func TestTerminalAuditSelectors(t *testing.T) {
	for _, operation := range []string{"remove", "prune", "clean"} {
		t.Run(operation, func(t *testing.T) {
			a := newTerminalAudit(t)
			paths := map[string]string{}
			for _, name := range []string{"alpha", "beta", "gamma"} {
				path := filepath.Join(a.root, name)
				paths[name] = path
				a.git(a.root, "init", "-b", "main", path)
				a.git(path, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
				a.git(path, "branch", "stale")
				if name == "gamma" {
					a.run(path, a.bin, "init")
				} else {
					a.run(path, a.bin, "init", "--tag", "candidate")
				}
			}
			if operation == "prune" {
				for _, path := range paths {
					if err := os.Rename(path, path+"-moved"); err != nil {
						t.Fatal(err)
					}
				}
			}
			var args []string
			switch operation {
			case "remove":
				args = []string{"remove", "--all", "--select"}
			case "prune":
				args = []string{"prune", "--tag", "candidate", "--select"}
			case "clean":
				args = []string{"clean", "--tag", "candidate", "--select"}
			}
			title := "Select repos to " + operation
			registry := func() map[string]bool {
				t.Helper()
				var entries []struct {
					Name string `json:"name"`
				}
				out := a.run(a.root, a.bin, "list", "--json")
				if err := json.Unmarshal([]byte(out), &entries); err != nil {
					t.Fatal(err)
				}
				got := map[string]bool{}
				for _, e := range entries {
					got[e.Name] = true
				}
				return got
			}
			// Cancellation before confirmation leaves all registry entries and refs.
			cancelled := a.start(args...)
			cancelled.wait(0, title)
			cancelled.wait(cancelled.send("\x03"), "aborted")
			if got := registry(); len(got) != 3 {
				t.Fatalf("cancel changed registry: %v", got)
			}
			if operation == "clean" && a.git(paths["beta"], "branch", "--list", "stale") != "stale" {
				t.Fatal("cancel deleted branch")
			}
			declined := a.start(args...)
			declined.wait(0, title)
			declined.wait(declined.send("\r"), "[y/N]")
			declined.wait(declined.send("n\n"), "aborted")
			if got := registry(); len(got) != 3 {
				t.Fatalf("declined confirmation changed registry: %v", got)
			}
			if operation == "clean" && a.git(paths["beta"], "branch", "--list", "stale") != "stale" {
				t.Fatal("declined confirmation deleted branch")
			}

			s := a.start(args...)
			s.wait(0, title)
			// Fixture paths exceed this width; redraws must clear wrapped rows
			// without changing which repository is selected.
			if err := pty.Setsize(s.file, &pty.Winsize{Rows: 12, Cols: 55}); err != nil {
				t.Fatal(err)
			}
			// Filtering does not silently discard checked items. Uncheck alpha;
			// the surviving candidates remain selected, with gamma out of scope
			// for clean/prune due to the command's tag filter.
			s.wait(s.send("alpha"), "alpha")
			s.wait(s.send(" "), title)
			s.wait(s.send("\r"), "[y/N]")
			s.wait(s.send("y\n"), "✓")
			select {
			case err := <-s.done:
				if err != nil {
					t.Fatalf("selector exit: %v", err)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("selector did not exit")
			}
			got := registry()
			switch operation {
			case "remove":
				if len(got) != 1 || !got["alpha"] {
					t.Fatalf("remove selection widened: %v", got)
				}
			case "prune":
				if len(got) != 2 || !got["alpha"] || !got["gamma"] {
					t.Fatalf("prune selection widened: %v", got)
				}
			case "clean":
				if len(got) != 3 {
					t.Fatalf("clean changed registry: %v", got)
				}
				for _, name := range []string{"alpha", "beta", "gamma"} {
					branch := a.git(paths[name], "branch", "--list", "stale")
					if (branch != "") != (name != "beta") {
						t.Fatalf("%s stale branch = %q", name, branch)
					}
				}
			}
		})
	}
}

func TestTerminalAuditDashboardInteractions(t *testing.T) {
	a := newTerminalAudit(t)
	checkout, _, _ := a.fixture()
	bin := filepath.Join(a.root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{
		"gh":       "#!/bin/sh\nprintf '%s\\n' '[{\"number\":42,\"title\":\"fixture pull request\",\"headRefName\":\"topic\",\"state\":\"OPEN\",\"url\":\"https://example.invalid/pr/42\"}]'\n",
		"pbcopy":   "#!/bin/sh\ncat > \"$AUDIT_CLIP\"\n",
		"wl-copy":  "#!/bin/sh\ncat > \"$AUDIT_CLIP\"\n",
		"open":     "#!/bin/sh\nprintf '%s' \"$1\" > \"$AUDIT_BROWSER\"\n",
		"xdg-open": "#!/bin/sh\nprintf '%s' \"$1\" > \"$AUDIT_BROWSER\"\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	clip := filepath.Join(a.root, "clipboard")
	browser := filepath.Join(a.root, "browser")
	a.env = append(a.env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AUDIT_CLIP="+clip, "AUDIT_BROWSER="+browser, "SOKO_NAV_SESSION=12345")
	s := a.start("ui")
	s.wait(0, "› alpha")
	s.wait(s.send("s"), "sort:dirty")
	s.wait(s.send("f"), "filter:dirty")
	s.wait(s.send("F"), "sort:dirty")
	s.wait(s.send("/"), "▏")
	s.wait(s.send("z"), "no repos match search")
	s.wait(s.send("\r"), "search:z")
	s.wait(s.send("\x1b"), "alpha")
	s.wait(s.send("*"), "1 marked")
	s.wait(s.send("*"), "1 repo")
	s.wait(s.send("?"), "press any key to close")
	s.wait(s.send("?"), "REPO")
	s.wait(s.send("L"), "fixture pull request")
	s.send("\r")
	// Browser.Open starts its adapter asynchronously; wait for the observable
	// side effect rather than reading the file before the child has run.
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(browser)
		if err == nil {
			if string(data) != "https://example.invalid/pr/42" {
				t.Fatalf("browser URL = %q", data)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("browser adapter did not run: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.wait(s.send("q"), "REPO")
	s.wait(s.send("y"), "copied")
	data, err := os.ReadFile(clip)
	if err != nil || string(data) != checkout {
		t.Fatalf("clipboard = %q, err %v", data, err)
	}
	if err := pty.Setsize(s.file, &pty.Winsize{Rows: 12, Cols: 55}); err != nil {
		t.Fatal(err)
	}
	s.wait(s.send("?"), "press any key to close")
	resized := s.send("?")
	s.wait(resized, "REPO")
	s.wait(resized, "…") // the long selected path is clipped at the narrow width
	s.send("\r")
	select {
	case err := <-s.done:
		if err != nil {
			t.Fatalf("navigation exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("navigation did not exit")
	}
	data, err = os.ReadFile(filepath.Join(a.root, "config", "soko", ".nav.12345"))
	if err != nil || string(data) != checkout {
		t.Fatalf("navigation file = %q, err %v", data, err)
	}
}

func TestTerminalAuditFetchOverlap(t *testing.T) {
	a := newTerminalAudit(t)
	_, _, _ = a.fixture()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(a.root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	starts := filepath.Join(a.root, "fetch-starts")
	release := filepath.Join(a.root, "fetch-release")
	// The wrapper holds the actual Git fetch until released. It logs each
	// invocation before blocking, so duplicate in-flight fetches are observable.
	script := "#!/bin/sh\nif [ \"$1\" = fetch ]; then\n  printf 'start\\n' >> \"$AUDIT_STARTS\"\n  while [ ! -f \"$AUDIT_RELEASE\" ]; do sleep 0.05; done\nfi\nexec \"$AUDIT_GIT\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	a.env = append(a.env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AUDIT_GIT="+realGit, "AUDIT_STARTS="+starts, "AUDIT_RELEASE="+release)
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })
	s := a.start("ui", "--fetch", "30s")
	s.wait(0, "› alpha")
	s.wait(s.send("r"), "fetching")
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(starts)
		if err == nil && string(data) == "start\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fetch did not reach barrier: %v, %q", err, data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Manual requests arrive while the first fetch is held. The periodic tick
	// has a 30-second minimum, so keep the barrier closed through that tick.
	s.send("R")
	time.Sleep(100 * time.Millisecond) // allow the terminal decoder to separate independent keypresses
	s.wait(s.send("s"), "sort:dirty")
	s.send("r")
	time.Sleep(100 * time.Millisecond)
	s.wait(s.send("s"), "sort:behind")
	timer := time.NewTimer(31 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case err := <-s.done:
		t.Fatalf("dashboard exited during held fetch: %v", err)
	}
	data, err := os.ReadFile(starts)
	if err != nil || string(data) != "start\n" {
		t.Fatalf("overlapping fetches: %q, err %v", data, err)
	}
	mark := s.mark()
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s.wait(mark, "fetched")
	// A completed fetch must not leave the dashboard permanently busy.
	s.wait(s.send("R"), "alpha: fetched")
	data, err = os.ReadFile(starts)
	if err != nil || string(data) != "start\nstart\n" {
		t.Fatalf("fetch retry count = %q, err %v", data, err)
	}
	// Close the gate again and let the next periodic tick initiate a fetch.
	// Manual requests must also be rejected while that timer fetch is blocked.
	if err := os.Remove(release); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(40 * time.Second)
	for {
		data, err = os.ReadFile(starts)
		if err == nil && string(data) == "start\nstart\nstart\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timer fetch did not reach barrier: %v, %q", err, data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.send("R")
	time.Sleep(100 * time.Millisecond)
	s.wait(s.send("S"), "sort:dirty")
	s.send("r")
	time.Sleep(100 * time.Millisecond)
	s.wait(s.send("S"), "sort:name")
	data, err = os.ReadFile(starts)
	if err != nil || string(data) != "start\nstart\nstart\n" {
		t.Fatalf("manual request overlapped timer fetch: %q, err %v", data, err)
	}
	mark = s.mark()
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s.wait(mark, "fetched")
	s.wait(s.send("R"), "alpha: fetched")
	data, err = os.ReadFile(starts)
	if err != nil || string(data) != "start\nstart\nstart\nstart\n" {
		t.Fatalf("fetch retry after timer = %q, err %v", data, err)
	}
	s.send("q")
}
