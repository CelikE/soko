"""Offline tests for audit helpers; never contact GitHub or install packages."""

import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import audit_distribution
import audit_pr_service


class PRServiceAuditTests(unittest.TestCase):
    def test_normalizes_real_gh_schema(self):
        data = [{"number": 1, "title": "test", "headRefName": "feature", "state": "OPEN",
                 "url": "https://github.com/test/repo/pull/1", "author": {"login": "tester"}}]
        self.assertEqual(audit_pr_service.normalize(data), [
            {"number": 1, "title": "test", "branch": "feature", "state": "OPEN",
             "url": "https://github.com/test/repo/pull/1", "author": "tester"}])
        self.assertEqual(audit_pr_service.normalize([]), [])

    def test_environment_isolates_credentials_and_state(self):
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(os.environ, {"GH_TOKEN": "old-token", "GITHUB_TOKEN": "other-token",
                                        "GH_REPO": "wrong/repo", "GIT_CONFIG_COUNT": "2",
                                        "SOKO_QUIET": "true", "GH_DEBUG": "api"}):
                env = audit_pr_service.isolated_env(Path(tmp), "selected-token")
            self.assertEqual(env["GH_TOKEN"], "selected-token")
            self.assertEqual(env["GH_HOST"], "github.com")
            self.assertEqual(env["GIT_CONFIG_GLOBAL"], os.devnull)
            for key in ("GITHUB_TOKEN", "GH_REPO", "GIT_CONFIG_COUNT", "SOKO_QUIET", "GH_DEBUG"):
                self.assertNotIn(key, env)
            for key in ("HOME", "XDG_CONFIG_HOME", "GH_CONFIG_DIR", "XDG_CACHE_HOME"):
                self.assertTrue(Path(env[key]).is_dir())


class DistributionAuditTests(unittest.TestCase):
    def test_checksum_lookup_is_exact(self):
        text = f"{'a' * 64}  soko_1_linux_amd64.tar.gz\n{'b' * 64}  soko_1_linux_arm64.tar.gz\n"
        result = audit_distribution.checksums(text)
        self.assertEqual(result["soko_1_linux_amd64.tar.gz"], "a" * 64)
        self.assertEqual(len(result), 2)

    def test_rejects_invalid_or_duplicate_checksums(self):
        for text in ("not-a-hash archive\n", f"{'g' * 64} archive\n",
                     f"{'a' * 64} archive\n{'b' * 64} archive\n"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                audit_distribution.checksums(text)

    def test_installed_version_must_match_release(self):
        with patch.object(audit_distribution, "run", return_value="soko 0.22.0 (commit)\n"):
            self.assertEqual(audit_distribution.verify_binary(Path("binary"), "0.22.0"),
                             "soko 0.22.0 (commit)")
        with patch.object(audit_distribution, "run", return_value="soko 0.21.0\n"):
            with self.assertRaises(ValueError):
                audit_distribution.verify_binary(Path("binary"), "0.22.0")


if __name__ == "__main__":
    unittest.main()
