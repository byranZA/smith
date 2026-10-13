package connection_test

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startSSHD runs an unprivileged sshd on a free loopback port that accepts the
// LANG and LC_* a client forwards, and returns an ssh config whose testbox host
// reaches it with its own identity and forwards the operator's locale.
func startSSHD(t *testing.T) string {
	t.Helper()
	sshd, err := exec.LookPath("sshd")
	if err != nil {
		sshd = "/usr/sbin/sshd"
	}
	if _, err := os.Stat(sshd); err != nil {
		t.Skipf("no sshd to run against: %v", err)
	}
	me, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current() err = %v", err)
	}
	dir := t.TempDir()
	for _, key := range []string{"host_key", "client_key"} {
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(dir, key)).CombinedOutput(); err != nil {
			t.Fatalf("ssh-keygen %s err = %v: %s", key, err, out)
		}
	}
	port := freePort(t)
	writeFile(t, filepath.Join(dir, "sshd_config"), fmt.Sprintf(`Port %d
ListenAddress 127.0.0.1
HostKey %[2]s/host_key
AuthorizedKeysFile %[2]s/client_key.pub
PasswordAuthentication no
KbdInteractiveAuthentication no
UsePAM no
StrictModes no
PidFile %[2]s/sshd.pid
AcceptEnv LANG LC_*
`, port, dir))
	writeFile(t, filepath.Join(dir, "ssh_config"), fmt.Sprintf(`Host testbox
  HostName 127.0.0.1
  Port %d
  User %s
  IdentityFile %[3]s/client_key
  IdentitiesOnly yes
  UserKnownHostsFile %[3]s/known_hosts
  StrictHostKeyChecking accept-new
  BatchMode yes
  LogLevel ERROR
  SendEnv LANG LC_*
`, port, me.Username, dir))

	daemon := exec.CommandContext(t.Context(), sshd, "-D", "-e", "-f", filepath.Join(dir, "sshd_config"))
	if err := daemon.Start(); err != nil {
		t.Fatalf("start sshd err = %v", err)
	}
	t.Cleanup(func() {
		var killed *exec.ExitError
		if err := daemon.Wait(); err != nil && !errors.As(err, &killed) {
			t.Errorf("stop sshd err = %v", err)
		}
	})
	waitForPort(t, port)
	return filepath.Join(dir, "ssh_config")
}

// freePort returns a loopback TCP port nothing is listening on.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for a free port err = %v", err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", l.Addr())
	}
	if err := l.Close(); err != nil {
		t.Fatalf("release port %d err = %v", addr.Port, err)
	}
	return addr.Port
}

// waitForPort waits until something accepts connections on the loopback port.
func waitForPort(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			if err := conn.Close(); err != nil {
				t.Fatalf("close probe of port %d err = %v", port, err)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("sshd never listened on port %d", port)
}

// writeFile writes content to path or fails the test.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s err = %v", path, err)
	}
}

// localeLines returns the LANG and LC_* lines of an env listing.
func localeLines(env string) []string {
	var lines []string
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(line, "LANG=") || strings.HasPrefix(line, "LC_") {
			lines = append(lines, line)
		}
	}
	return lines
}

// fakeOnPath puts an executable name running script first on PATH.
func fakeOnPath(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
		t.Fatalf("write fake %s err = %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
