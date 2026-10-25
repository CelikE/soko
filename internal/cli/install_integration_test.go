package cli_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIntegration_InstallDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX installer permission checks require a Unix shell")
	}
	script, err := filepath.Abs("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		protected bool
		missing   bool
		failSudo  string
	}{
		{name: "writable existing directory"},
		{name: "writable new directory", missing: true},
		{name: "protected existing directory", protected: true},
		{name: "protected new directory", protected: true, missing: true},
		{name: "sudo mkdir failure", protected: true, missing: true, failSudo: "mkdir"},
		{name: "sudo copy failure", protected: true, failSudo: "cp"},
		{name: "sudo chmod failure", protected: true, failSudo: "chmod"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.protected && os.Geteuid() == 0 {
				t.Skip("root bypasses directory write permissions")
			}
			base := t.TempDir()
			tools := filepath.Join(base, "tools")
			installDir := filepath.Join(base, "install dir", "bin")
			manDir := filepath.Join(base, "man")
			log := filepath.Join(base, "sudo.log")
			if err := os.MkdirAll(tools, 0o755); err != nil {
				t.Fatal(err)
			}
			if !tc.missing {
				if err := os.MkdirAll(installDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if tc.protected {
					if err := os.Chmod(installDir, 0o555); err != nil {
						t.Fatal(err)
					}
				}
			}
			// Restore directory permissions before TempDir's cleanup.
			t.Cleanup(func() { _ = os.Chmod(installDir, 0o755) })
			for _, name := range []string{"uname", "curl", "sha256sum", "sudo", "mkdir", "cp", "chmod"} {
				if err := os.WriteFile(filepath.Join(tools, name), []byte(installTestTool), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			archive := installTestArchive(t)
			archivePath := filepath.Join(base, "release.tar.gz")
			if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", tools+string(os.PathListSeparator)+installDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("MAN_DIR", manDir)
			t.Setenv("INSTALL_TEST_DIR", installDir)
			t.Setenv("INSTALL_TEST_ARCHIVE", archivePath)
			t.Setenv("INSTALL_TEST_CHECKSUM", fmt.Sprintf("%x", sha256.Sum256(archive)))
			t.Setenv("INSTALL_TEST_LOG", log)
			t.Setenv("INSTALL_TEST_PROTECTED", fmt.Sprint(tc.protected))
			t.Setenv("INSTALL_TEST_MISSING", fmt.Sprint(tc.missing))
			t.Setenv("INSTALL_TEST_FAIL_SUDO", tc.failSudo)
			t.Setenv("INSTALL_TEST_SUDO", "")
			cmd := exec.CommandContext(context.Background(), "sh", script, "--version", "v0.0.0", "--dir", installDir)
			out, err := cmd.CombinedOutput()
			if tc.failSudo != "" {
				if err == nil || !strings.Contains(string(out), "simulated sudo "+tc.failSudo+" failure") {
					t.Fatalf("installer should report sudo %s failure: %v\n%s", tc.failSudo, err, out)
				}
				if strings.Contains(string(out), "installed to") {
					t.Fatalf("failed install reported success:\n%s", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("installer failed: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "installed to "+installDir+"/soko") ||
				!strings.Contains(string(out), "soko v0.0.0\n") {
				t.Fatalf("installer did not finish and verify the installed binary:\n%s", out)
			}
			if _, err := os.Stat(filepath.Join(manDir, "soko.1")); err != nil {
				t.Fatalf("installer did not reach man page installation: %v", err)
			}
			calls, err := os.ReadFile(log)
			if !tc.protected {
				if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if len(calls) != 0 {
					t.Fatalf("writable install unnecessarily invoked sudo: %s", calls)
				}
				return
			}
			want := "cp\nchmod\n"
			if tc.missing {
				want = "mkdir\n" + want
			}
			if err != nil || string(calls) != want {
				t.Fatalf("sudo calls = %q, %v; want %q", calls, err, want)
			}
		})
	}
}

func TestIntegration_InstallWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX installer checks require a Unix shell")
	}
	script, err := filepath.Abs("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, osName := range []string{"MINGW64_NT-10.0", "MSYS_NT-10.0", "CYGWIN_NT-10.0"} {
		t.Run(osName, func(t *testing.T) {
			base := t.TempDir()
			tools := filepath.Join(base, "tools")
			installDir := filepath.Join(base, "install dir", "bin")
			manDir := filepath.Join(base, "man")
			if err := os.MkdirAll(tools, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"uname", "curl", "sha256sum"} {
				if err := os.WriteFile(filepath.Join(tools, name), []byte(installTestTool), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			archive := installTestWindowsArchive(t)
			archivePath := filepath.Join(base, "release.zip")
			if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", tools+string(os.PathListSeparator)+installDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("MAN_DIR", manDir)
			t.Setenv("INSTALL_TEST_OS", osName)
			t.Setenv("INSTALL_TEST_FILENAME", "soko_0.0.0_windows_amd64.zip")
			t.Setenv("INSTALL_TEST_ARCHIVE", archivePath)
			t.Setenv("INSTALL_TEST_CHECKSUM", fmt.Sprintf("%x", sha256.Sum256(archive)))
			cmd := exec.CommandContext(context.Background(), "sh", script, "--version", "v0.0.0", "--dir", installDir)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Windows installer failed: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "installed to "+installDir+"/soko.exe") ||
				!strings.Contains(string(out), "soko v0.0.0\n") {
				t.Fatalf("installer did not finish and verify soko.exe:\n%s", out)
			}
			binary, err := os.ReadFile(filepath.Join(installDir, "soko.exe"))
			if err != nil || string(binary) != installTestBinary {
				t.Fatalf("installed binary = %q, %v; want archive's soko.exe", binary, err)
			}
			if _, err := os.Stat(filepath.Join(manDir, "soko.1")); !os.IsNotExist(err) {
				t.Fatalf("Windows installer should skip man page installation: %v", err)
			}
		})
	}
}

const installTestBinary = "#!/bin/sh\n[ \"$1\" = version ] || exit 1\nprintf 'soko v0.0.0\\n'\n"

func installTestArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, file := range []struct{ name, content string }{
		{"soko", installTestBinary},
		{"soko.1", ".TH SOKO 1\n"},
	} {
		// Leave the binary non-executable so chmod must run on both install paths.
		if err := tw.WriteHeader(&tar.Header{Name: file.name, Mode: 0o644, Size: int64(len(file.content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(file.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func installTestWindowsArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range []struct{ name, content string }{
		{"soko.exe", installTestBinary},
		{"soko.1", ".TH SOKO 1\n"},
	} {
		entry, err := zw.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(file.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Stub downloads and privilege boundaries, while using real extraction, file
// copies, permission changes, and installed-binary execution. No network or
// actual sudo is needed, and protected paths stay inside the test directory.
const installTestTool = `#!/bin/sh
set -eu
tool=${0##*/}
case "$tool" in
  uname)
    case "$1" in -s) printf '%s\n' "${INSTALL_TEST_OS:-Linux}" ;; -m) printf 'x86_64\n' ;; esac
    ;;
  curl)
    case "$2" in
      */checksums.txt) printf '%s  %s\n' "$INSTALL_TEST_CHECKSUM" "${INSTALL_TEST_FILENAME:-soko_0.0.0_linux_amd64.tar.gz}" > "$4" ;;
      *) command -p cp "$INSTALL_TEST_ARCHIVE" "$4" ;;
    esac
    ;;
  sha256sum) printf '%s  %s\n' "$INSTALL_TEST_CHECKSUM" "$1" ;;
  sudo)
    printf '%s\n' "$1" >> "$INSTALL_TEST_LOG"
    if [ "$1" = "$INSTALL_TEST_FAIL_SUDO" ]; then
      printf 'simulated sudo %s failure\n' "$1" >&2
      exit 1
    fi
    INSTALL_TEST_SUDO=1 "$@"
    ;;
  mkdir)
    if [ "$2" = "$INSTALL_TEST_DIR" ] && [ "$INSTALL_TEST_PROTECTED" = true ] && [ "$INSTALL_TEST_MISSING" = true ]; then
      if [ "$INSTALL_TEST_SUDO" != 1 ]; then
        printf 'mkdir: permission denied\n' >&2
        exit 1
      fi
      command -p mkdir "$@"
      command -p chmod a-w "$INSTALL_TEST_DIR"
    else
      command -p mkdir "$@"
    fi
    ;;
  cp)
    if [ "$2" = "$INSTALL_TEST_DIR/" ] && [ "$INSTALL_TEST_PROTECTED" = true ]; then
      [ "$INSTALL_TEST_SUDO" = 1 ] || exit 1
      command -p chmod u+w "$INSTALL_TEST_DIR"
      command -p cp "$@"
      command -p chmod a-w "$INSTALL_TEST_DIR"
    else
      command -p cp "$@"
    fi
    ;;
  chmod)
    if [ "$2" = "$INSTALL_TEST_DIR/soko" ] && [ "$INSTALL_TEST_PROTECTED" = true ] && [ "$INSTALL_TEST_SUDO" != 1 ]; then
      printf 'chmod: operation not permitted\n' >&2
      exit 1
    fi
    command -p chmod "$@"
    ;;
esac
`
