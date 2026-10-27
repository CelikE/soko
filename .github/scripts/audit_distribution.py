#!/usr/bin/env python3
"""Validate existing public release archives and install into temporary paths.

Does not publish releases or update manifests. Package-manager checks run in
separate disposable-runner workflow steps; this script is also safe locally.
"""

import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import platform
import stat
import subprocess
import tarfile
import tempfile
import zipfile


SOURCE = Path(__file__).resolve().parents[2]
RELEASE = "https://github.com/CelikE/soko/releases/download"


def run(args, *, env=None, cwd=SOURCE):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True,
                                   encoding="utf-8", stderr=subprocess.STDOUT, timeout=300)


def download(url, destination):
    run(["curl", "-fsSL", "--retry", "2", url, "-o", str(destination)])


def checksums(text):
    result = {}
    for line in text.splitlines():
        digest, name = line.split()
        if len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
            raise ValueError("invalid release SHA256")
        if name in result:
            raise ValueError("duplicate release checksum entry")
        result[name] = digest
    return result


def verify_binary(binary, version):
    actual = run([str(binary), "version"]).strip()
    if actual.split()[1].lstrip("v") != version:
        raise ValueError(f"unexpected installed version: {actual}")
    return actual


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="v0.22.0")
    parser.add_argument("--source-install", action="store_true", help="also exercise the documented go install @latest path")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    version = args.version.removeprefix("v")
    if not version or any(c not in "0123456789." for c in version):
        parser.error("--version must be a stable numeric release tag")
    system = {"Darwin": "darwin", "Linux": "linux", "Windows": "windows"}[platform.system()]
    arch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64", "amd64": "amd64"}[platform.machine().lower()]
    report = {"source": run(["git", "rev-parse", "HEAD"]).strip(),
              "utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "platform": platform.platform(), "release": args.version, "checks": []}

    def record(name, **details):
        report["checks"].append({"name": name, "result": "PASS", **details})

    try:
        with tempfile.TemporaryDirectory(prefix="soko-distribution-") as tmp:
            root = Path(tmp)
            checksum_file = root / "checksums.txt"
            download(f"{RELEASE}/{args.version}/checksums.txt", checksum_file)
            expected = checksums(checksum_file.read_text())
            ext = "zip" if system == "windows" else "tar.gz"
            name = f"soko_{version}_{system}_{arch}.{ext}"
            archive_file = root / name
            download(f"{RELEASE}/{args.version}/{name}", archive_file)
            actual = hashlib.sha256(archive_file.read_bytes()).hexdigest()
            assert actual == expected[name], "release checksum mismatch"
            record("public native archive checksum", asset=name, sha256=actual)
            binary = root / ("soko.exe" if system == "windows" else "soko")
            if system == "windows":
                with zipfile.ZipFile(archive_file) as archive:
                    binary.write_bytes(archive.read("soko.exe"))
                    assert archive.read("man/soko.1")
            else:
                with tarfile.open(archive_file) as archive:
                    binary.write_bytes(archive.extractfile("soko").read())
                    binary.chmod(archive.getmember("soko").mode)
                    assert archive.extractfile("man/soko.1").read()
            record("native released binary invocation", version=verify_binary(binary, version),
                   manpage="present in archive", executable=os.access(binary, os.X_OK))

            if system != "windows":
                for label, url in (("checkout", None), ("published", "https://raw.githubusercontent.com/CelikE/soko/master/install.sh")):
                    script = SOURCE / "install.sh"
                    if url:
                        script = root / "published-install.sh"
                        download(url, script)
                    destination = root / label
                    bin_dir, man_dir = destination / "bin", destination / "man"
                    env = os.environ | {"MAN_DIR": str(man_dir), "PATH": str(bin_dir) + os.pathsep + os.environ["PATH"]}
                    output = run(["sh", str(script), "--dir", str(bin_dir), "--version", args.version], env=env)
                    assert "checksum verified." in output
                    installed = bin_dir / "soko"
                    actual_version = verify_binary(installed, version)
                    binary_mode = stat.S_IMODE(installed.stat().st_mode)
                    man_mode = stat.S_IMODE((man_dir / "soko.1").stat().st_mode)
                    assert binary_mode & 0o111 and not binary_mode & 0o022
                    assert man_mode & 0o444 and not man_mode & 0o022
                    record(f"{label} installer native invocation", version=actual_version,
                           binary_mode=oct(binary_mode), manpage_mode=oct(man_mode), checksum="verified")

            if args.source_install:
                source_bin = root / "go-bin"
                source_bin.mkdir()
                env = os.environ | {"GOBIN": str(source_bin), "GOWORK": "off"}
                run(["go", "install", "github.com/CelikE/soko/cmd/soko@latest"], env=env, cwd=root)
                installed = source_bin / binary.name
                actual_version = run([str(installed), "version"]).strip()
                metadata = run(["go", "version", "-m", str(installed)])
                module = next(line.strip() for line in metadata.splitlines()
                              if line.strip().startswith("mod\tgithub.com/CelikE/soko\t"))
                record("documented go install @latest", version=actual_version, module=module,
                       note="go install does not set release ldflags; version may be dev")
    except Exception as error:
        report["checks"].append({"name": "execution", "result": "FAIL", "error_type": type(error).__name__})
        raise
    finally:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(f"PASS: {len(report['checks'])} native distribution checks; evidence: {args.output}")


if __name__ == "__main__":
    main()
