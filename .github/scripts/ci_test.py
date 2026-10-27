#!/usr/bin/env python3
"""Run Go tests in isolated CLI shards and retain JSON events for profiling."""

import argparse
import json
from pathlib import Path
import re
import subprocess
import sys
import time


def shard_tests(names, shard, shards):
    """Keep top-level tests (and all their subtests) together in stable shards."""
    return sorted(names)[shard - 1 :: shards]


def configure_console():
    """Windows redirected consoles may default to CP1252, unlike Go's UTF-8."""
    for stream in (sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if reconfigure:
            reconfigure(encoding="utf-8", errors="backslashreplace")


def commands(args):
    flags = ["-json", "-count=1", "-timeout=15m"]
    if args.race:
        flags.append("-race")
    if args.coverprofile:
        flags.append(f"-coverprofile={args.coverprofile}")
    if args.shards == 1:
        return [["go", "test", *flags, "./..."]]

    print("Building test binary and discovering CLI tests...", flush=True)
    started = time.perf_counter()
    listing = subprocess.check_output(
        ["go", "test", *(["-race"] if args.race else []), "-list", ".", "./internal/cli"],
        text=True, encoding="utf-8", errors="replace",
    )
    print(f"CLI test discovery/build: {time.perf_counter() - started:.2f}s", flush=True)
    names = [line for line in listing.splitlines() if re.fullmatch(r"(?:Test|Example|Fuzz)\w*", line)]
    selected = shard_tests(names, args.shard, args.shards)
    if not selected:
        raise ValueError("CLI shard is empty; check test discovery and shard count")
    print(f"CLI shard {args.shard}/{args.shards}: {len(selected)} of {len(names)} top-level tests", flush=True)

    runs = []
    if args.shard == 1:
        packages = subprocess.check_output(
            ["go", "list", "./..."], text=True, encoding="utf-8", errors="replace",
        ).splitlines()
        packages = [package for package in packages if not package.endswith("/internal/cli")]
        if packages:
            runs.append(["go", "test", *flags, *packages])
    pattern = "^(" + "|".join(re.escape(name) for name in selected) + ")$"
    runs.append(["go", "test", *flags, "-run", pattern, "./internal/cli"])
    return runs


def run(command, events, timings):
    print(f"Running Go tests ({'race' if '-race' in command else 'normal'})", flush=True)
    output = {}
    completed = 0
    last_progress = time.monotonic()
    with subprocess.Popen(
        command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        text=True, encoding="utf-8", errors="replace",
    ) as process:
        for line in process.stdout:
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                print(line, end="", flush=True)
                continue
            events.write(line)
            events.flush()
            key = (event.get("Package", ""), event.get("Test", ""))
            if event.get("Output"):
                output.setdefault(key, []).append(event["Output"])
            action = event.get("Action")
            if action == "build-output":
                print(event.get("Output", ""), end="", flush=True)
            if action == "fail":
                print("".join(output.get(key, [])), end="", flush=True)
            if action in ("pass", "fail", "skip"):
                if not key[1]:
                    print(f"{action}: {key[0]} ({event.get('Elapsed', 0):.2f}s)", flush=True)
                elif "/" not in key[1]:
                    timings.append(event)
                    completed += 1
                    now = time.monotonic()
                    if event.get("Elapsed", 0) >= 5 or now - last_progress >= 30:
                        print(f"{action}: {key[1]} ({event.get('Elapsed', 0):.2f}s; {completed} tests completed)", flush=True)
                        last_progress = now
        return process.wait()


def main():
    configure_console()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--shard", type=int, default=1)
    parser.add_argument("--shards", type=int, default=1)
    parser.add_argument("--race", action="store_true")
    parser.add_argument("--coverprofile")
    parser.add_argument("--timings", type=Path, required=True)
    args = parser.parse_args()
    if not 1 <= args.shard <= args.shards:
        parser.error("--shard must be between 1 and --shards")
    if args.coverprofile and args.shards != 1:
        parser.error("coverage requires an unsharded run")

    args.timings.parent.mkdir(parents=True, exist_ok=True)
    timings = []
    result = 0
    with args.timings.open("w", encoding="utf-8") as events:
        for command in commands(args):
            result = run(command, events, timings)
            if result:
                break
    print("Slowest top-level tests:", flush=True)
    for event in sorted(timings, key=lambda item: item.get("Elapsed", 0), reverse=True)[:10]:
        print(f"  {event.get('Elapsed', 0):7.2f}s {event['Package']}/{event['Test']}", flush=True)
    return result


if __name__ == "__main__":
    sys.exit(main())
