"""Regression checks for fault-harness isolation, evidence, and process bounds."""

import os
from pathlib import Path
import signal
import sys
import tempfile
import unittest
from unittest.mock import patch

import audit_destructive as audit


@unittest.skipUnless(os.name == "posix", "fault harness requires POSIX process groups")
class FaultHarnessTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="soko-fault-harness-test-")
        self.addCleanup(self.temporary.cleanup)
        self.record = dict(commands=[], actions=[], states=[])
        self.fixture = audit.Fixture(Path(self.temporary.name), Path(sys.executable), self.record)
        self.addCleanup(self.fixture.close)

    def test_captures_separate_streams_exit_and_order(self):
        event = self.fixture.run([sys.executable, "-c",
                                  "import sys; print('out'); print('err', file=sys.stderr); sys.exit(7)"],
                                 expected=7)
        self.assertEqual(event["stdout"], "out\n")
        self.assertEqual(event["stderr"], "err\n")
        self.assertEqual(event["exit"], 7)
        self.assertGreater(event["completed"], event["started"])
        self.assertEqual(self.fixture.processes, [])

    def test_timeout_kills_and_reaps_owned_process_group(self):
        with patch.object(audit, "TIMEOUT", 0.1):
            with self.assertRaisesRegex(AssertionError, "command timed out"):
                self.fixture.run([sys.executable, "-c", "import time; time.sleep(30)"])
        self.assertTrue(self.record["commands"][0]["timeout"])
        self.assertEqual(self.record["commands"][0]["exit"], -signal.SIGKILL)
        self.assertEqual(self.fixture.processes, [])

    def test_child_output_is_size_limited(self):
        script = f"import os; os.write(1, b'x' * {audit.FILE_LIMIT}); os.write(1, b'y')"
        event = self.fixture.run([sys.executable, "-c", script], expected=None)
        self.assertNotEqual(event["exit"], 0)
        self.assertLessEqual(len(event["stdout"]), audit.FILE_LIMIT)
        self.assertEqual(self.fixture.processes, [])

    def test_cleanup_reaps_process_even_before_config_exists(self):
        handle = self.fixture.start([sys.executable, "-c", "import time; time.sleep(30)"])
        self.fixture.close()
        self.assertEqual(handle[0].returncode, -signal.SIGKILL)
        self.assertEqual(self.fixture.processes, [])

    def test_developer_git_and_soko_settings_do_not_leak(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.dict(os.environ, {"GIT_DIR": "/developer/repo/.git",
                                         "SOKO_QUIET": "1", "GH_TOKEN": "must-not-leak",
                                         "AUDIT_MODE": "fail-pin"}):
                fixture = audit.Fixture(Path(directory), Path(sys.executable),
                                        dict(commands=[], actions=[], states=[]))
            try:
                self.assertNotIn("GIT_DIR", fixture.env)
                self.assertNotIn("GH_TOKEN", fixture.env)
                self.assertNotIn("AUDIT_MODE", fixture.env)
                self.assertEqual(fixture.env["SOKO_QUIET"], "0")
                self.assertEqual(fixture.env["HOME"], str(Path(directory) / "home"))
                self.assertEqual(fixture.env["GIT_ALLOW_PROTOCOL"], "file")
            finally:
                fixture.close()


if __name__ == "__main__":
    unittest.main()
