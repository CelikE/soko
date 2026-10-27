#!/usr/bin/env python3
"""Bounded, disposable CLI fault experiments for audit #355 (POSIX only).

No third-party Python packages or product fault hooks. Experiments are local;
the initial Go build may download dependencies that are not already cached.
JSON evidence includes every subprocess, explicit state checkpoints, injected
filesystem/signal actions, assertions, and recovery. See docs/audits/355.md.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import traceback

if os.name == "posix":
    import resource


TIMEOUT = 20
FILE_LIMIT = 1024 * 1024
SOURCE = Path(__file__).resolve().parents[2]


def limit_child_files():
    # Tiny fixtures need no large files. Bound captured output as well as Git's
    # own writes, even if a regression produces output until its timeout.
    _, hard = resource.getrlimit(resource.RLIMIT_FSIZE)
    limit = FILE_LIMIT if hard == resource.RLIM_INFINITY else min(FILE_LIMIT, hard)
    resource.setrlimit(resource.RLIMIT_FSIZE, (limit, limit))


class Blocked(Exception):
    """An experiment's platform prerequisites are unavailable."""


def require(condition, message):
    if not condition:
        raise AssertionError(message)


class Fixture:
    def __init__(self, root, binary, record):
        self.root, self.binary, self.record = root, binary, record
        self.processes = []
        self.sequence = 0
        self.env = {
            k: v for k, v in os.environ.items()
            if not k.startswith(("GIT_", "SOKO_", "GH_", "GITHUB_", "XDG_", "AUDIT_"))
        }
        for name in ("home", "config", "cache", "tmp", "barrier"):
            (root / name).mkdir()
        self.env.update(
            HOME=str(root / "home"), XDG_CONFIG_HOME=str(root / "config"),
            XDG_CACHE_HOME=str(root / "cache"), TMPDIR=str(root / "tmp"),
            GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1",
            GIT_TERMINAL_PROMPT="0", GIT_ALLOW_PROTOCOL="file",
            GIT_TEMPLATE_DIR=str(root / "home"), NO_COLOR="1",
            SOKO_QUIET="0", SOKO_PERF="0",
        )
        self.git_binary = shutil.which("git")
        require(self.git_binary, "git is required")
        self.config = root / "config/soko/config.yaml"
        self.journal = self.config.with_name("journal.yaml")
        record["isolation"] = {k: self.env[k] for k in (
            "HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "TMPDIR",
            "GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM", "GIT_ALLOW_PROTOCOL",
            "GIT_TEMPLATE_DIR", "GIT_TERMINAL_PROMPT", "NO_COLOR",
        )}

    def order(self):
        self.sequence += 1
        return self.sequence

    def start(self, argv, cwd=None, env=None):
        effective = self.env | (env or {})
        argv = list(map(str, argv))
        event = {"argv": argv, "cwd": str(cwd or self.root), "env": env or {},
                 "started": self.order()}
        self.record["commands"].append(event)
        # File-backed output cannot fill a pipe while the controller waits at a
        # barrier. Fixture sizes and all command runtimes are bounded.
        stdout = tempfile.TemporaryFile(dir=self.root / "tmp")
        stderr = tempfile.TemporaryFile(dir=self.root / "tmp")
        process = subprocess.Popen(
            argv, cwd=cwd or self.root, env=effective, stdin=subprocess.DEVNULL,
            stdout=stdout, stderr=stderr, start_new_session=True,
            preexec_fn=limit_child_files,
        )
        handle = (process, event, stdout, stderr, time.monotonic())
        self.processes.append(handle)
        return handle

    def finish(self, handle, expected=0):
        process, event, stdout, stderr, started = handle
        try:
            process.wait(timeout=max(0.01, TIMEOUT - (time.monotonic() - started)))
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)
            event["timeout"] = True
        stdout.seek(0)
        stderr.seek(0)
        event.update(exit=process.returncode, completed=self.order(),
                     stdout=stdout.read().decode(errors="replace"),
                     stderr=stderr.read().decode(errors="replace"))
        stdout.close()
        stderr.close()
        self.processes.remove(handle)
        require(not event.get("timeout"), f"command timed out: {event}")
        if expected is not None:
            require(event["exit"] == expected, f"unexpected exit: {event}")
        return event

    def run(self, argv, cwd=None, expected=0, env=None):
        return self.finish(self.start(argv, cwd, env), expected)

    def cli(self, *args, expected=0):
        return self.run([self.binary, *args], expected=expected)

    def git(self, repo, *args):
        return self.run([self.git_binary, *args], cwd=repo)["stdout"].strip()

    def action(self, description, **details):
        self.record["actions"].append({"description": description, "order": self.order(), **details})

    def write(self, path, text):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
        self.action("write fixture file", path=str(path), contents=text)

    def repo(self, name):
        repo = self.root / name
        repo.mkdir()
        self.git(repo, "init", "-b", "main")
        self.git(repo, "config", "user.name", "Audit User")
        self.git(repo, "config", "user.email", "audit@example.invalid")
        self.git(repo, "config", "core.autocrlf", "false")
        self.write(repo / "tracked.txt", "base\n")
        self.git(repo, "add", ".")
        self.git(repo, "commit", "-m", "base")
        self.run([self.binary, "init"], cwd=repo)
        return repo

    def state(self, label, *repos):
        state = {"label": label, "repos": {}}
        for repo in repos:
            files = {}
            # Deliberately small fixtures; include ignored and submodule files,
            # but never traverse Git object stores or read developer files.
            for directory, dirs, names in os.walk(repo):
                dirs[:] = [d for d in dirs if d != ".git"]
                for name in names:
                    path = Path(directory) / name
                    if name != ".git":
                        files[str(path.relative_to(repo))] = path.read_text()
            state["repos"][repo.name] = {
                "head": self.git(repo, "rev-parse", "HEAD"),
                "refs": self.git(repo, "for-each-ref", "--format=%(refname) %(objectname)"),
                "status": self.git(repo, "status", "--porcelain", "--untracked-files=all", "--ignore-submodules=none"),
                "index": self.git(repo, "diff", "--cached", "--binary"),
                "worktree": self.git(repo, "diff", "--binary"),
                "stashes": self.git(repo, "stash", "list", "--format=%H %gs"),
                "files": files,
            }
        for name, path in (("config", self.config), ("journal", self.journal)):
            state[name] = path.read_text() if path.exists() else None
        state["order"] = self.order()
        self.record["states"].append(state)
        return state

    def barrier(self, mode):
        wrapper = self.root / "git-barrier"
        # Match a completed, specific Git operation. Only this process receives
        # AUDIT_MODE, so other CLI processes can run through the same wrapper.
        wrapper.write_text(f"#!{sys.executable}\n" + '''import json, os, pathlib, subprocess, sys, time
args = sys.argv[1:]
mode = os.environ.get("AUDIT_MODE")
root = pathlib.Path(os.environ["AUDIT_BARRIER"])
event = {"argv": [os.environ["AUDIT_GIT"], *args], "cwd": os.getcwd(), "mode": mode}
if mode == "fail-pin" and args[:1] == ["update-ref"]:
    event.update(exit=73, stdout="", stderr="injected ref-write failure\\n")
    (root / (str(time.monotonic_ns()) + ".json")).write_text(json.dumps(event))
    print("injected ref-write failure", file=sys.stderr)
    sys.exit(73)
result = subprocess.run([os.environ["AUDIT_GIT"], *args], capture_output=True)
sys.stdout.buffer.write(result.stdout)
sys.stderr.buffer.write(result.stderr)
sys.stdout.flush()
sys.stderr.flush()
status = result.returncode
matched = (
    (mode == "ctx-pin" and args[:1] == ["update-ref"] and args[1].startswith("refs/worktree/soko/ctx/")) or
    (mode == "ctx-apply" and args[:2] == ["stash", "apply"]) or
    (mode == "clean-restore" and args[:2] == ["branch", "feature"]) or
    (mode == "pull-reset" and args[:2] == ["reset", "--keep"])
)
event.update(exit=status, stdout=result.stdout.decode(errors="replace"),
             stderr=result.stderr.decode(errors="replace"), barrier=bool(status == 0 and matched))
(root / (str(time.monotonic_ns()) + ".json")).write_text(json.dumps(event))
if status == 0 and matched:
    (root / "ready").write_text("ready")
    deadline = time.monotonic() + 15
    while not (root / "release").exists():
        if time.monotonic() > deadline:
            print("barrier timeout", file=sys.stderr)
            sys.exit(74)
        time.sleep(0.01)
sys.exit(status)
''')
        wrapper.chmod(0o700)
        self.cli("config", "set", "git_path", wrapper)
        self.env.update(AUDIT_GIT=self.git_binary, AUDIT_BARRIER=str(self.root / "barrier"))
        return {"AUDIT_MODE": mode}

    def wait(self, handle):
        deadline = time.monotonic() + 10
        while not (self.root / "barrier/ready").exists():
            require(handle[0].poll() is None, "process exited before barrier")
            require(time.monotonic() < deadline, "controller barrier timeout")
            time.sleep(0.01)
        self.action("completed Git operation held at barrier")

    def release(self):
        self.write(self.root / "barrier/release", "release")

    def close(self):
        for handle in list(self.processes):
            try:
                os.killpg(handle[0].pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            self.finish(handle, expected=None)
        # Permission experiments must never strand an undeletable temp tree.
        if self.config.parent.exists():
            self.config.parent.chmod(0o700)
        self.record["git_hooks"] = [json.loads(p.read_text())
                                    for p in sorted((self.root / "barrier").glob("*.json"))]
        self.record["cleanup"] = "owned process groups terminated and children reaped; permissions restored; disposable tree removed by runner"


def work(f, repo):
    f.write(repo / "tracked.txt", "staged\n")
    f.git(repo, "add", "tracked.txt")
    f.write(repo / "tracked.txt", "unstaged\n")
    f.write(repo / "untracked.txt", "untracked\n")
    f.git(repo, "config", "status.showUntrackedFiles", "all")


def same_work(before, after, repo):
    a, b = before["repos"][repo.name], after["repos"][repo.name]
    for key in ("head", "status", "index", "worktree", "files"):
        require(a[key] == b[key], f"user state changed: {repo.name} {key}")


def denied_directory(f):
    f.config.parent.chmod(0o500)
    f.action("deny config directory writes", mode="0500")
    try:
        with tempfile.TemporaryFile(dir=f.config.parent):
            pass
    except PermissionError:
        f.action("write probe returned PermissionError (not simulated)")
        return
    raise Blocked("uid bypasses directory write restrictions; requires unprivileged POSIX user")


def ctx_failure(f, fault):
    repo = f.repo("repo")
    f.write(repo / "canary.txt", "older backup\n")
    f.git(repo, "stash", "push", "-u", "-m", "canary")
    canary = f.git(repo, "rev-parse", "refs/stash")
    if fault == "force-conflict":
        work(f, repo)
        f.cli("ctx", "save", "work")
    work(f, repo)
    mode = f.barrier("fail-pin" if fault == "pin" else "ctx-pin")
    before = f.state("before failed save", repo)
    args = [f.binary, "ctx", "save", "work", "--json"]
    if fault == "force-conflict":
        args.append("--force")
    handle = f.start(args, env=mode)
    try:
        if fault != "pin":
            f.wait(handle)
            if fault == "permission":
                denied_directory(f)
            else:
                f.cli("alias", "set", "canary", "status")
            f.state("stash pinned, config commit about to fail", repo)
            persisted = f.config.read_text()
            f.release()
        result = f.finish(handle, expected=1)
        require("saving config" in result["stderr"] if fault != "pin" else
                "no repo state could be saved" in result["stderr"], "missing failure diagnostic")
    finally:
        f.config.parent.chmod(0o700)
    after = f.state("rollback completed", repo)
    same_work(before, after, repo)
    require(canary in after["repos"][repo.name]["stashes"], "older backup lost")
    old, new = before["repos"][repo.name], after["repos"][repo.name]
    for key in ("refs", "stashes"):
        for line in old[key].splitlines():
            if key == "refs" and line.startswith("refs/stash "):
                continue  # New backup advances the tip; old stash entries must remain.
            require(line in new[key].splitlines(), f"existing {key} preimage lost: {line}")
    if fault != "pin":
        require(after["config"] == persisted, "failed save replaced live config")
    else:
        require(after["config"] == before["config"], "failed capture changed config")
    require(len(new["stashes"].splitlines()) == len(old["stashes"].splitlines()) + 1, "rollback backup lost")
    f.action("recovery verified: rollback restored index, working tree and untracked files; retain stash backups")


def interrupted_ctx(f, phase):
    repo = f.repo("repo")
    work(f, repo)
    before = f.state("original staged and unstaged work", repo)
    if phase == "ctx-apply":
        f.cli("ctx", "save", "work")
    env = f.barrier(phase)
    verb = "save" if phase == "ctx-pin" else "switch"
    handle = f.start([f.binary, "ctx", verb, "work", "--json"], env=env)
    f.wait(handle)
    f.state("interruption boundary", repo)
    f.action("SIGKILL isolated CLI process group", signal=signal.SIGKILL)
    os.killpg(handle[0].pid, signal.SIGKILL)
    f.finish(handle, expected=-signal.SIGKILL)
    f.state("after SIGKILL", repo)
    oid = f.git(repo, "rev-parse", "refs/stash")
    f.git(repo, "cat-file", "-e", oid + "^{commit}")
    f.git(repo, "cat-file", "-e", oid + "^3^{commit}")
    for prefix in ("refs/soko/ctx-backups/", "refs/worktree/soko/ctx/"):
        require(f.git(repo, "rev-parse", prefix + oid) == oid, "interruption lost pinned backup")
    if phase == "ctx-pin":
        require(json.loads(f.cli("ctx", "list", "--json")["stdout"]) == [], "uncommitted context published")
        f.git(repo, "stash", "apply", "--index", oid)
        same_work(before, f.state("manual stash recovery", repo), repo)
    else:
        result = f.cli("ctx", "switch", "work", "--json", expected=1)
        require("dirty" in result["stdout"], "retry should refuse restored dirty files")
        after = f.state("retry refused without changing restored files", repo)
        require(after["repos"][repo.name]["files"] == before["repos"][repo.name]["files"], "restored contents lost")
        # After inspecting the restored work and exact saved OID, record the
        # missing completion marker. This is manual recovery, not atomicity.
        f.git(repo, "update-ref", "refs/soko/ctx-restored/" + oid, oid)
        f.git(repo, "add", ".")
        f.git(repo, "commit", "-m", "recovered work")
        f.cli("ctx", "switch", "work", "--json")
        f.state("manual completion recovery and safe retry", repo)


def clean_overlap(f):
    a, b = f.repo("a"), f.repo("b")
    tips = {}
    for repo in (a, b):
        tips[repo.name] = f.git(repo, "rev-parse", "HEAD")
        f.git(repo, "branch", "feature")
    f.cli("clean", "a", "--force", "--json")
    env = f.barrier("clean-restore")
    f.state("before undo", a, b)
    handle = f.start([f.binary, "undo"], env=env)
    f.wait(handle)
    f.cli("clean", "b", "--force", "--json")
    appended = f.state("other process appended while reversal held", a, b)
    f.release()
    result = f.finish(handle, expected=1)
    require("journal changed" in result["stderr"], "stale pop not refused")
    after = f.state("conflicting pop refused", a, b)
    require(after["journal"] == appended["journal"], "concurrent preimages lost")
    for repo in (b, a):
        f.cli("undo")
        require(f.git(repo, "rev-parse", "refs/heads/feature") == tips[repo.name], "wrong restored tip")
    require(json.loads(f.cli("undo", "--list", "--json")["stdout"]) == [], "journal not drained")
    f.state("newest entry undone first, original retry skipped existing branch", a, b)


def seed_pulls(f, repos):
    pulls = []
    for repo in repos:
        before = f.git(repo, "rev-parse", "HEAD")
        f.write(repo / "tracked.txt", "post-pull\n")
        f.git(repo, "commit", "-am", "post-pull fixture")
        pulls.append(dict(repo=repo.name, path=str(repo), sha=before,
                          branch="refs/heads/main", post_sha=f.git(repo, "rev-parse", "HEAD")))
    f.write(f.journal, json.dumps({"entries": [{"op": "pull", "time": "2026-10-27T00:00:00Z",
                                             "summary": "synthetic fast-forward preimages", "pulls": pulls}]}))
    return pulls


def pull_failure(f, fault):
    a, b = f.repo("a"), f.repo("b")
    pulls = seed_pulls(f, (a, b))
    c = None
    if fault == "overlap":
        c = f.repo("c")
        f.git(c, "branch", "feature")
    else:
        # Keep the sidecar writable so this reaches atomic temp-file creation,
        # rather than failing earlier while opening the journal lock.
        f.write(f.journal.with_suffix(".yaml.lock"), "")
    env = f.barrier("pull-reset")
    before = f.state("before undo", a, b)
    handle = f.start([f.binary, "undo"], env=env)
    try:
        f.wait(handle)
        if c:
            f.cli("clean", "c", "--force", "--json")
        else:
            denied_directory(f)
        pending = f.state("first reset complete, checkpoint cannot commit", a, b)
        f.release()
        result = f.finish(handle, expected=1)
        require("recording undo progress" in result["stderr"], "checkpoint failure not visible")
    finally:
        f.config.parent.chmod(0o700)
    after = f.state("checkpoint refused; second target untouched", a, b)
    require(after["journal"] == pending["journal"], "successful preimages or appended entry lost")
    require(after["repos"]["a"]["head"] == pulls[0]["sha"], "first reset did not finish")
    same_work(before, after, b)
    if c:
        f.cli("undo")
    # A failed checkpoint cannot promise automatic retry. Verify exact pre-tip,
    # archive the journal, then explicitly checkpoint only the verified target.
    f.write(f.root / "journal-recovery-backup.yaml", f.journal.read_text())
    pulls[0]["undone"] = True
    f.write(f.journal, json.dumps({"entries": [{"op": "pull", "time": "2026-10-27T00:00:00Z",
                                             "summary": "verified manual checkpoint", "pulls": pulls}]}))
    f.write(a / "newer.txt", "work after reset\n")
    f.cli("undo")
    require(f.git(b, "rev-parse", "HEAD") == pulls[1]["sha"], "pending target not recovered")
    require((a / "newer.txt").read_text() == "work after reset\n", "retry revisited completed target")
    f.state("manual checkpoint recovery; pending target retried", a, b)


def partial_retry(f):
    a, b = f.repo("a"), f.repo("b")
    pulls = seed_pulls(f, (a, b))
    f.write(b / "tracked.txt", "user blocking work\n")
    before = f.state("before partial undo", a, b)
    f.cli("undo", expected=1)
    partial = f.state("first target checkpointed, second refused", a, b)
    same_work(before, partial, b)
    require(f.git(a, "rev-parse", "HEAD") == pulls[0]["sha"], "successful target not reversed")
    f.write(a / "newer.txt", "new work must survive retry\n")
    f.git(b, "stash", "push", "-u", "-m", "manual blocker backup")
    f.cli("undo")
    require(f.git(b, "rev-parse", "HEAD") == pulls[1]["sha"], "retry did not reverse pending target")
    require((a / "newer.txt").read_text() == "new work must survive retry\n", "retry destroyed newer work")
    require("manual blocker backup" in f.git(b, "stash", "list"), "blocker backup lost")
    require(json.loads(f.cli("undo", "--list", "--json")["stdout"]) == [], "retry did not consume completed entry")
    f.state("retry completed with newer work and blocker backup retained", a, b)


def layouts(f, mode):
    source, repo = f.repo("module-source"), f.repo("super")
    f.cli("remove", "module-source", "--force")
    f.git(repo, "submodule", "add", source, "module")
    f.git(repo, "commit", "-am", "add submodule")
    module = repo / "module"
    module_tip = f.git(module, "rev-parse", "HEAD")
    for checkout in (repo, module):
        gitdir = Path(f.git(checkout, "rev-parse", "--absolute-git-dir"))
        f.write(gitdir / "info/exclude", "ignored.txt\n")
        f.write(checkout / "ignored.txt", "ignored user data\n")
    if mode == "ctx":
        work(f, repo)
        f.cli("ctx", "save", "layout")
        args = ("ctx", "switch", "layout", "--json")
    elif mode == "snapshot":
        f.cli("snapshot", "save", "layout")
        f.write(repo / "tracked.txt", "later tip\n")
        f.git(repo, "commit", "-am", "later tip")
        args = ("snapshot", "restore", "layout", "--json")
    else:
        seed_pulls(f, (repo,))
        args = ("undo",)
    target = f.git(repo, "rev-parse", "HEAD") if mode == "ctx" else f.git(repo, "rev-parse", "HEAD^")
    f.write(module / "tracked.txt", "dirty submodule work\n")
    before = f.state("dirty submodule and unrelated ignored files", repo, module)
    result = f.cli(*args, expected=1)
    require("dirty" in result["stdout"] + result["stderr"], "dirty submodule not diagnosed")
    refused = f.state("restore refused dirty submodule", repo, module)
    for checkout in (repo, module):
        same_work(before, refused, checkout)
        for key in ("refs", "stashes"):
            require(before["repos"][checkout.name][key] == refused["repos"][checkout.name][key],
                    f"refused restore changed {checkout.name} {key}")
    require(before["journal"] == refused["journal"], "refused restore changed journal")
    # Preserve blocker outside the checkout; restore its committed contents.
    f.write(f.root / "submodule-recovery.txt", (module / "tracked.txt").read_text())
    f.write(module / "tracked.txt", "base\n")
    f.cli(*args)
    require(f.git(repo, "rev-parse", "HEAD") == target, "wrong restored parent tip")
    require(f.git(module, "rev-parse", "HEAD") == module_tip, "submodule ref changed")
    for checkout in (repo, module):
        require((checkout / "ignored.txt").read_text() == "ignored user data\n", "ignored file lost")
    if mode == "ctx":
        require((repo / "tracked.txt").read_text() == "unstaged\n", "context work lost")
        require((repo / "untracked.txt").read_text() == "untracked\n", "untracked work lost")
    f.state("retry succeeded; ignored files and submodule tip preserved", repo, module)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, help="existing source-built soko (otherwise build disposable binary)")
    parser.add_argument("--output", type=Path, required=True, help="JSON evidence destination")
    args = parser.parse_args()
    report = {"platform": platform.platform(), "python": sys.version, "experiments": [],
              "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              "limits": {"process_seconds": TIMEOUT, "child_file_bytes": FILE_LIMIT,
                         "top_level_cli_concurrency": 2},
              "unavailable": [
                  {"result": "BLOCKED", "scenario": "real ENOSPC / quota exhaustion",
                   "reason": "requires disposable size-limited filesystem; not provisioned; never fill host disk"},
                  {"result": "BLOCKED", "scenario": "multi-user ownership / ACL denial",
                   "reason": "requires provisioned second UID and isolated ownership sandbox; no privilege escalation"},
              ]}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    if os.name != "posix":
        report["blocked"] = "requires POSIX process groups, signals and permissions; no native Windows claim"
        args.output.write_text(json.dumps(report, indent=2) + "\n")
        print(report["blocked"])
        return 2
    with tempfile.TemporaryDirectory(prefix="soko-audit-355-") as temporary:
        root = Path(temporary).resolve()
        binary = args.binary.resolve() if args.binary else root / "soko"
        if not args.binary:
            build_argv = ["go", "build", "-o", str(binary), "./cmd/soko"]
            built = subprocess.run(build_argv,
                                   cwd=SOURCE, capture_output=True, text=True, timeout=120)
            report["build"] = dict(argv=build_argv, cwd=str(SOURCE), exit=built.returncode,
                                   stdout=built.stdout, stderr=built.stderr)
            require(built.returncode == 0, built.stderr)
        report["source"] = subprocess.run(["git", "rev-parse", "HEAD"], cwd=SOURCE,
                                          capture_output=True, text=True, timeout=5).stdout.strip()
        report["git_version"] = subprocess.run(["git", "--version"], capture_output=True,
                                               text=True, timeout=5).stdout.strip()
        report["binary"] = str(binary)
        report["binary_sha256"] = hashlib.sha256(binary.read_bytes()).hexdigest()
        experiments = [("ctx-" + fault, lambda f, fault=fault: ctx_failure(f, fault))
                       for fault in ("conflict", "force-conflict", "permission", "pin")]
        experiments += [("kill-" + phase, lambda f, phase=phase: interrupted_ctx(f, phase))
                        for phase in ("ctx-pin", "ctx-apply")]
        experiments += [("clean-overlap", clean_overlap), ("pull-partial-retry", partial_retry)]
        experiments += [("pull-" + fault, lambda f, fault=fault: pull_failure(f, fault))
                        for fault in ("overlap", "permission")]
        experiments += [("layout-" + mode, lambda f, mode=mode: layouts(f, mode))
                        for mode in ("ctx", "snapshot", "pull")]
        for name, experiment in experiments:
            record = dict(name=name, commands=[], actions=[], states=[])
            report["experiments"].append(record)
            directory = root / name
            directory.mkdir()
            fixture = Fixture(directory, binary, record)
            try:
                experiment(fixture)
                record["result"] = "PASS"
            except Blocked as error:
                record.update(result="BLOCKED", reason=str(error))
            except Exception:
                record.update(result="FAIL", error=traceback.format_exc())
            finally:
                fixture.close()
            print(f"{record['result']}: {name}")
            if record["result"] == "FAIL":
                print(record["error"], file=sys.stderr)
            # Preserve exact argv/output while normalizing only disposable root.
            args.output.write_text(json.dumps(report, indent=2).replace(str(root), "<fixture>") + "\n")
    if any(e["result"] == "FAIL" for e in report["experiments"]):
        return 1
    return 2 if any(e["result"] == "BLOCKED" for e in report["experiments"]) else 0


if __name__ == "__main__":
    sys.exit(main())
