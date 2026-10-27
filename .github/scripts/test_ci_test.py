"""Check CI shard completeness and preserve actionable failure diagnostics."""

import argparse
import contextlib
import io
import json
import re
import sys
import unittest
from unittest.mock import patch

import ci_test


class RunnerTests(unittest.TestCase):
    def test_windows_failure_diagnostics_are_utf8(self):
        raw = io.BytesIO()
        console = io.TextIOWrapper(raw, encoding="cp1252")
        events = [
            {"Action": "output", "Package": "example/cli", "Test": "TestUnicode", "Output": "✓ blå — failure\n"},
            {"Action": "fail", "Package": "example/cli", "Test": "TestUnicode", "Elapsed": 0.1},
        ]
        script = f"import sys; print({chr(10).join(map(json.dumps, events))!r}); sys.exit(1)"
        with patch.object(sys, "stdout", console):
            ci_test.configure_console()
            result = ci_test.run([sys.executable, "-c", script], io.StringIO(), [])
        console.flush()
        self.assertEqual(result, 1)
        self.assertIn("✓ blå — failure", raw.getvalue().decode("utf-8"))
        console.detach()

    def test_shards_are_stable_disjoint_and_complete(self):
        names = [f"TestCase{index:03}" for index in range(101)]
        shards = [ci_test.shard_tests(names, index, 3) for index in range(1, 4)]
        flattened = [name for shard in shards for name in shard]
        self.assertCountEqual(flattened, names)
        self.assertEqual(len(flattened), len(set(flattened)))
        self.assertLessEqual(max(map(len, shards)) - min(map(len, shards)), 1)
        self.assertEqual(ci_test.shard_tests(list(reversed(names)), 1, 3), shards[0])

    def test_commands_run_every_cli_test_and_other_packages_once(self):
        names = ["TestOne", "TestTwo", "TestThree", "ExampleCLI", "FuzzCLI"]
        listing = "\n".join(names + ["ok example/internal/cli 0.1s"])
        selected = []
        other_runs = []
        for shard in range(1, 5):
            args = argparse.Namespace(shard=shard, shards=4, race=True, coverprofile=None)
            responses = [listing, "example/internal/cli\nexample/internal/config\n"] if shard == 1 else [listing]
            with patch("ci_test.subprocess.check_output", side_effect=responses), contextlib.redirect_stdout(io.StringIO()):
                runs = ci_test.commands(args)
            cli_run = runs[-1]
            pattern = cli_run[cli_run.index("-run") + 1]
            selected.extend(name for name in names if re.fullmatch(pattern, name))
            self.assertFalse(re.fullmatch(pattern, "TestOneExtra"))
            self.assertIn("-race", cli_run)
            other_runs.extend(runs[:-1])
        self.assertCountEqual(selected, names)
        self.assertEqual(len(other_runs), 1)
        self.assertEqual(other_runs[0][-1], "example/internal/config")

    def test_slow_tests_emit_progress_without_counting_subtests_twice(self):
        events = [
            {"Action": "pass", "Package": "example/cli", "Test": "TestSlow/child", "Elapsed": 6},
            {"Action": "pass", "Package": "example/cli", "Test": "TestSlow", "Elapsed": 6.25},
        ]
        script = f"print({chr(10).join(map(json.dumps, events))!r})"
        console, timings = io.StringIO(), []
        with contextlib.redirect_stdout(console):
            result = ci_test.run([sys.executable, "-c", script], io.StringIO(), timings)
        self.assertEqual(result, 0)
        self.assertIn("TestSlow (6.25s; 1 tests completed)", console.getvalue())
        self.assertEqual(timings, [events[1]])

    def test_empty_discovery_fails_instead_of_silently_skipping_tests(self):
        args = argparse.Namespace(shard=1, shards=3, race=False, coverprofile=None)
        with patch("ci_test.subprocess.check_output", return_value="ok example/internal/cli 0.1s"):
            with self.assertRaisesRegex(ValueError, "shard is empty"):
                ci_test.commands(args)

    def test_test_failures_propagate_and_retain_output_and_timings(self):
        events = [
            {"Action": "output", "Package": "example/cli", "Test": "TestFailure", "Output": "expected value, got wrong value\n"},
            {"Action": "fail", "Package": "example/cli", "Test": "TestFailure", "Elapsed": 0.1},
            {"Action": "fail", "Package": "example/cli", "Elapsed": 0.2},
        ]
        script = f"import sys; print({chr(10).join(map(json.dumps, events))!r}); sys.exit(1)"
        captured, console, timings = io.StringIO(), io.StringIO(), []
        with contextlib.redirect_stdout(console):
            result = ci_test.run([sys.executable, "-c", script], captured, timings)
        self.assertEqual(result, 1)
        self.assertIn("expected value, got wrong value", console.getvalue())
        self.assertEqual([json.loads(line) for line in captured.getvalue().splitlines()], events)
        self.assertEqual(timings, [events[1]])

    def test_build_failures_retain_diagnostics(self):
        event = {"Action": "build-output", "ImportPath": "example/cli", "Output": "undefined: missingFunction\n"}
        script = f"import sys; print({json.dumps(event)!r}); sys.exit(1)"
        console = io.StringIO()
        with contextlib.redirect_stdout(console):
            result = ci_test.run([sys.executable, "-c", script], io.StringIO(), [])
        self.assertEqual(result, 1)
        self.assertIn("undefined: missingFunction", console.getvalue())


if __name__ == "__main__":
    unittest.main()
