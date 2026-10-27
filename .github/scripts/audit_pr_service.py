#!/usr/bin/env python3
"""Read-only live gh/soko PR checks using isolated local Git/config fixtures.

Requires authorization to read the selected public repositories. Never creates
repositories, pull requests, commits or releases. Credentials stay in memory.
"""

import argparse
import datetime
import json
import os
from pathlib import Path
import platform
import re
import socket
import subprocess
import tempfile
import urllib.request


SOURCE = Path(__file__).resolve().parents[2]
FIELDS = "number,title,headRefName,state,url,author"


def run(args, *, cwd=SOURCE, env=None, check=True):
    result = subprocess.run(args, cwd=cwd, env=env, capture_output=True,
                            text=True, encoding="utf-8", timeout=90)
    if check and result.returncode:
        # Do not serialize credential-bearing environments or subprocess output.
        raise RuntimeError(f"{Path(str(args[0])).name} failed (exit {result.returncode})")
    return result


def isolated_env(root, token):
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(("GH_", "GITHUB_", "GIT_", "SOKO_"))}
    for name in ("home", "config", "gh", "cache"):
        (root / name).mkdir()
    env.update(HOME=str(root / "home"), USERPROFILE=str(root / "home"),
               XDG_CONFIG_HOME=str(root / "config"), GH_CONFIG_DIR=str(root / "gh"),
               XDG_CACHE_HOME=str(root / "cache"), GH_TOKEN=token, GH_HOST="github.com",
               GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1",
               GIT_TERMINAL_PROMPT="0", NO_COLOR="1", TERM="dumb")
    return env


def normalize(items):
    return [{"number": p["number"], "title": p["title"], "branch": p["headRefName"],
             "state": p["state"], "url": p["url"], "author": p["author"]["login"]}
            for p in items]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--populated", default="golang/go")
    parser.add_argument("--empty", default="CelikE/homebrew-tap")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    for repo in (args.populated, args.empty):
        if not re.fullmatch(r"[\w.-]+/[\w.-]+", repo):
            parser.error("repositories must be owner/name")
    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    credential_source = "caller-provided token"
    if not token:
        token = run(["gh", "auth", "token", "--hostname", "github.com"]).stdout.strip()
        credential_source = "existing gh credential, reused with explicit authorization"
    report = {"source": run(["git", "rev-parse", "HEAD"]).stdout.strip(),
              "utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "platform": platform.platform(), "gh": run(["gh", "--version"]).stdout.splitlines()[0],
              "host": "github.com", "credentials": credential_source,
              "isolation": "temporary HOME/config/cache/GH_CONFIG_DIR; token only in child environment",
              "fixtures": {"populated": args.populated, "empty": args.empty},
              "fixture_policy": "read-only public snapshots, not dedicated test repositories",
              "checks": []}

    def record(name, **details):
        report["checks"].append({"name": name, "result": "PASS", **details})

    try:
        with tempfile.TemporaryDirectory(prefix="soko-live-pr-") as tmp:
            root = Path(tmp)
            binary = root / ("soko.exe" if os.name == "nt" else "soko")
            run(["go", "build", "-o", str(binary), "./cmd/soko"])
            env = isolated_env(root, token)
            for label, remote in (("populated", args.populated), ("empty", args.empty)):
                repo = root / label
                run(["git", "init", str(repo)], env=env)
                run(["git", "remote", "add", "origin", f"https://github.com/{remote}.git"], cwd=repo, env=env)
                nested = repo / "nested"
                nested.mkdir()

                def gh(limit):
                    return json.loads(run(["gh", "pr", "list", "--state", "open", "--limit", str(limit),
                                           "--json", FIELDS], cwd=repo, env=env).stdout)

                def soko(*flags, environment=env, check=True):
                    return run([str(binary), "pr", *flags], cwd=nested, env=environment, check=check)

                baseline = gh(101)
                if label == "empty":
                    if baseline:
                        raise RuntimeError("empty public fixture now has PRs; choose another fixture")
                    for flags in (("--json",), ("--quiet", "--json")):
                        result = soko(*flags)
                        assert json.loads(result.stdout) == [] and not result.stderr.strip()
                    assert "no open pull requests" in soko().stdout
                    assert not soko("--quiet").stdout.strip()
                    record("live empty JSON/text/quiet", count=0)
                    continue

                if len(baseline) < 101:
                    raise RuntimeError("populated fixture needs at least 101 open PRs for pagination")
                for limit in (1, 50, 101):
                    flags = [] if limit == 50 else ["--limit", str(limit)]
                    expected = normalize(gh(limit))
                    result = soko(*flags, "--json")
                    actual = json.loads(result.stdout)
                    # Public fixture contents can change: compare exact snapshots,
                    # and fail rather than silently treating drift as a pass.
                    assert actual == expected, "live PR snapshot changed; rerun with a stable fixture"
                    assert len(actual) == limit and f"limit ({limit})" in result.stderr
                    quiet = soko(*flags, "--quiet", "--json")
                    assert json.loads(quiet.stdout) == actual and not quiet.stderr.strip()
                    record("live current-repo/JSON/limit/quiet", limit=limit, count=len(actual),
                           pagination="crosses gh's 100-item page boundary" if limit == 101 else "capped")
                first = baseline[0]
                url = f"https://github.com/{args.populated}/pull/{first['number']}"
                assert first["url"] == url
                with urllib.request.urlopen(url, timeout=30) as response:
                    assert response.status == 200
                record("live PR browser URL", url=url, http_status=200, browser_opened=False)

                unauthorized = env | {"GH_TOKEN": "soko-audit-intentionally-invalid-token"}
                result = soko("--quiet", "--json", environment=unauthorized, check=False)
                assert result.returncode != 0 and not result.stdout.strip()
                assert "gh pr list" in result.stderr and "401" in result.stderr
                record("live authentication failure", exit=result.returncode, stdout="empty", http_status=401)
                # Reserve a local port without listening; no other service can
                # occupy it. Real gh must report proxy connection failure.
                with socket.socket() as unavailable:
                    unavailable.bind(("127.0.0.1", 0))
                    proxy = f"http://127.0.0.1:{unavailable.getsockname()[1]}"
                    offline = env | {"HTTPS_PROXY": proxy, "HTTP_PROXY": proxy, "ALL_PROXY": proxy,
                                     "NO_PROXY": "", "no_proxy": "", "https_proxy": proxy}
                    result = soko("--quiet", "--json", environment=offline, check=False)
                    assert result.returncode != 0 and not result.stdout.strip()
                    assert "gh pr list" in result.stderr and "connect" in result.stderr.lower()
                    record("real gh controlled network failure", exit=result.returncode,
                           stdout="empty", mechanism="non-listening loopback proxy, not service outage")
    except Exception as error:
        report["checks"].append({"name": "execution", "result": "FAIL", "error_type": type(error).__name__})
        raise
    finally:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(f"PASS: {len(report['checks'])} live checks; evidence: {args.output}")


if __name__ == "__main__":
    main()
