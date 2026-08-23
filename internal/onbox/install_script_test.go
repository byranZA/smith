package onbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// installFixture is a box the embedded install.sh runs against: a temp
// directory standing in for the box's filesystem, with the release assets the
// fake curl serves and the install path the script writes to.
type installFixture struct {
	dir        string
	scriptPath string
	env        []string
	// installPath is where the script installs smith on this fake box.
	installPath string
	// curlLog records every URL the script fetched, so a test can prove a
	// failure was not retried.
	curlLog string
	// installLog records the argv of every install(1) the script ran, which is
	// what carries the mode and owner the binary lands with.
	installLog string
}

// newInstallFixture writes the embedded script, the fake binaries it drives,
// and a release archive holding a smith that reports version, into a temp box.
// checksum is what the served checksums.txt claims the archive hashes to.
func newInstallFixture(t *testing.T, version string, corruptChecksums bool) *installFixture {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(scriptPath, []byte(Script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	archive := filepath.Join(dir, "smith_linux_amd64.tar.gz")
	body := tarball(t, "smith", fmt.Sprintf("#!/usr/bin/env bash\necho \"smith %s\"\n", version))
	if err := os.WriteFile(archive, body, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if corruptChecksums {
		digest = strings.Repeat("0", len(digest))
	}
	checksums := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(checksums, []byte(fmt.Sprintf(
		"%s  smith_0.2.0_linux_arm64.tar.gz\n%s  smith_linux_amd64.tar.gz\n",
		strings.Repeat("a", 64), digest)), 0o644); err != nil {
		t.Fatalf("write checksums: %v", err)
	}

	f := &installFixture{
		dir:         dir,
		scriptPath:  scriptPath,
		installPath: filepath.Join(dir, "bin-usr-local", "smith"),
		curlLog:     filepath.Join(dir, "curl.log"),
		installLog:  filepath.Join(dir, "install.log"),
	}
	if err := os.MkdirAll(filepath.Dir(f.installPath), 0o755); err != nil {
		t.Fatalf("mkdir install dir: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	writeInstallFakeBins(t, binDir)
	f.env = append(os.Environ()[:0:0],
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SMITH_INSTALL_PATH="+f.installPath,
		"ARCHIVE_FIXTURE="+archive,
		"CHECKSUMS_FIXTURE="+checksums,
		"CURL_LOG="+f.curlLog,
		"INSTALL_LOG="+f.installLog,
	)
	return f
}

// run drives the script's subcommand and returns its combined output and exit
// code, as the Go side would see them over a connection.
func (f *installFixture) run(t *testing.T, args ...string) (output string, code int) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "bash", append([]string{f.scriptPath}, args...)...)
	cmd.Env = f.env
	cmd.Dir = f.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("run %v: %v", args, err)
		}
		code = exit.ExitCode()
	}
	return string(out), code
}

// installArgs runs the install subcommand against the fixture's served assets.
func (f *installFixture) installArgs() []string {
	return []string{"install",
		"--url", "https://example.test/v0.2.0/smith_linux_amd64.tar.gz",
		"--checksums-url", "https://example.test/v0.2.0/checksums.txt"}
}

// fileContains reports whether the named file holds want.
func (f *installFixture) fileContains(t *testing.T, path, want string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), want)
}

// tarball renders a one-file gzipped tar, the shape of a published release
// archive.
func tarball(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatalf("tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// writeInstallFakeBins installs the fakes the script drives on a box: the
// download, the privileged install, sudo, and the machine hardware name. Each
// one stands in for a boundary the script cannot have on a developer's machine,
// and nothing else about the script is replaced.
func writeInstallFakeBins(t *testing.T, binDir string) {
	t.Helper()
	// Fake curl: serves the fixture asset for a URL and records every fetch, so
	// a test can prove a failed download was not retried. CURL_FAIL makes every
	// fetch fail the way an unreachable release does.
	writeInstallFakeBin(t, binDir, "curl", `#!/usr/bin/env bash
out=""; url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2;;
    -*) shift;;
    *) url="$1"; shift;;
  esac
done
printf '%s\n' "$url" >> "$CURL_LOG"
if [ -n "${CURL_FAIL:-}" ]; then exit 22; fi
case "$url" in
  *checksums.txt) cp "$CHECKSUMS_FIXTURE" "$out";;
  *) cp "$ARCHIVE_FIXTURE" "$out";;
esac
`)
	// Fake sudo drops its leading options and execs the rest, so the privileged
	// install works without real privilege.
	writeInstallFakeBin(t, binDir, "sudo", `#!/usr/bin/env bash
while [ "$1" = "-n" ] || [ "$1" = "-E" ]; do shift; done
exec "$@"
`)
	// Fake install records the argv it was handed — the mode and owner the
	// binary lands with — and then does the copy for real.
	writeInstallFakeBin(t, binDir, "install", `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$INSTALL_LOG"
args=("$@")
dest="${args[${#args[@]}-1]}"
src="${args[${#args[@]}-2]}"
cp "$src" "$dest"
chmod 0755 "$dest"
`)
	// Fake sha256sum: a developer's machine may only have shasum, so this is a
	// compatibility shim over whichever one exists, implementing the
	// --ignore-missing check the script relies on.
	writeInstallFakeBin(t, binDir, "sha256sum", `#!/usr/bin/env bash
set -uo pipefail
list="${@: -1}"
verified=0
while read -r want name; do
  [ -n "$name" ] || continue
  name="${name#\*}"
  [ -f "$name" ] || continue
  if command -v /usr/bin/sha256sum >/dev/null 2>&1; then
    got="$(/usr/bin/sha256sum "$name" | awk '{print $1}')"
  else
    got="$(shasum -a 256 "$name" | awk '{print $1}')"
  fi
  if [ "$got" != "$want" ]; then echo "$name: FAILED"; exit 1; fi
  verified=1
done < "$list"
if [ "$verified" -ne 1 ]; then echo "no file was verified"; exit 1; fi
echo "OK"
`)
	// Fake uname reports a machine hardware name smith publishes an asset for.
	writeInstallFakeBin(t, binDir, "uname", `#!/usr/bin/env bash
echo x86_64
`)
}

// writeInstallFakeBin writes one executable fake into binDir.
func writeInstallFakeBin(t *testing.T, binDir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

func TestScriptInstallVerifiesThenInstallsRootOwnedAndExecutable(t *testing.T) {
	f := newInstallFixture(t, "0.2.0", false)

	out, code := f.run(t, f.installArgs()...)
	if code != 0 {
		t.Fatalf("install exited %d, want 0\n%s", code, out)
	}

	info, err := os.Stat(f.installPath)
	if err != nil {
		t.Fatalf("smith was not installed: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("installed mode = %v, want 0755", info.Mode().Perm())
	}
	if !f.fileContains(t, f.installLog, "-m 0755") || !f.fileContains(t, f.installLog, "root") {
		b, _ := os.ReadFile(f.installLog)
		t.Errorf("install argv = %q, want it root-owned and 0755", b)
	}
	if !f.fileContains(t, f.curlLog, "checksums.txt") {
		t.Error("the release's checksums were never fetched, so nothing was verified")
	}
}

func TestScriptInstallAbortsOnAChecksumMismatchWithoutRetryingOrInstalling(t *testing.T) {
	f := newInstallFixture(t, "0.2.0", true)
	if err := os.WriteFile(f.installPath, []byte("#!/usr/bin/env bash\necho \"smith 0.1.0\"\n"), 0o755); err != nil {
		t.Fatalf("seed the box's existing smith: %v", err)
	}

	out, code := f.run(t, f.installArgs()...)
	if code == 0 {
		t.Fatalf("install exited 0 on a checksum mismatch, want non-zero\n%s", out)
	}
	if !strings.Contains(out, "checksum") {
		t.Errorf("output %q does not report the checksum mismatch", out)
	}
	if !f.fileContains(t, f.installPath, "0.1.0") {
		t.Error("the box's existing smith was replaced, want it untouched")
	}
	b, err := os.ReadFile(f.curlLog)
	if err != nil {
		t.Fatalf("read curl log: %v", err)
	}
	if got := strings.Count(string(b), "smith_linux_amd64.tar.gz"); got != 1 {
		t.Errorf("the archive was fetched %d times, want it fetched once and not retried", got)
	}
}

func TestScriptInstallLeavesAnExistingBinaryAloneWhenTheDownloadFails(t *testing.T) {
	f := newInstallFixture(t, "0.2.0", false)
	f.env = append(f.env, "CURL_FAIL=1")
	if err := os.WriteFile(f.installPath, []byte("#!/usr/bin/env bash\necho \"smith 0.1.0\"\n"), 0o755); err != nil {
		t.Fatalf("seed the box's existing smith: %v", err)
	}

	out, code := f.run(t, f.installArgs()...)
	if code == 0 {
		t.Fatalf("install exited 0 on a failed download, want non-zero\n%s", out)
	}
	if !f.fileContains(t, f.installPath, "0.1.0") {
		t.Error("the box's existing smith was replaced, want it untouched")
	}
}

func TestScriptProbeReportsTheArchitectureAndTheInstalledVersion(t *testing.T) {
	f := newInstallFixture(t, "0.2.0", false)
	if err := os.WriteFile(f.installPath, []byte("#!/usr/bin/env bash\necho \"smith 0.1.0\"\necho \"commit: abc1234\"\n"), 0o755); err != nil {
		t.Fatalf("seed the box's existing smith: %v", err)
	}

	out, code := f.run(t, "probe")
	if code != 0 {
		t.Fatalf("probe exited %d, want 0\n%s", code, out)
	}

	if got := parseProbe(out); got.machine != "x86_64" || got.version != "0.1.0" {
		t.Errorf("probe reported %+v, want x86_64 running 0.1.0", got)
	}
}

func TestScriptProbeReportsNoVersionForABoxWithNoSmith(t *testing.T) {
	f := newInstallFixture(t, "0.2.0", false)

	out, code := f.run(t, "probe")
	if code != 0 {
		t.Fatalf("probe exited %d, want 0\n%s", code, out)
	}

	if got := parseProbe(out); got.machine != "x86_64" || got.version != "" {
		t.Errorf("probe reported %+v, want x86_64 with no smith installed", got)
	}
}

func TestScriptInstallLeavesTheMarkerAlone(t *testing.T) {
	f := newInstallFixture(t, "0.2.0", false)

	if strings.Contains(Script, "bootstrap.json") {
		t.Error("the install script touches the marker, which records nothing about the installed binary")
	}
	if _, code := f.run(t, f.installArgs()...); code != 0 {
		t.Fatalf("install exited %d, want 0", code)
	}
}
