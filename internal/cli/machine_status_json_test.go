package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/provider"
)

// probeBoxSSH stands in for ssh to a reachable box: it answers the reach check
// and the shipping of the script, and prints probeOutput for the probe.
type probeBoxSSH struct{ probeOutput string }

func (p probeBoxSSH) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	if name != "ssh" || len(args) < 2 {
		return nil
	}
	remoteCmd := args[len(args)-1]
	if answered, err := answerShip(remoteCmd, stdout); answered {
		return err
	}
	if isSubcommand(remoteCmd, "probe") {
		_, err := io.WriteString(stdout, p.probeOutput)
		return err
	}
	return nil
}

func probeOutputWithMarker(markerJSON, access string) string {
	return "marker-begin\n" + markerJSON + "\nmarker-end\n" +
		"smith-user-exists=yes\npasswordless-sudo=yes\n" +
		"ufw-active=active\nufw-default-deny=deny\nufw-ssh-allow=" + access + "\n" +
		"permit-root-login=no\npassword-authentication=no\n" +
		"fail2ban=running\nauto-updates=enabled\n" +
		"mem-total-kb=4026532\nswap-total-kb=0\n" +
		"docker-ports=ok\ndocker-container=web 0.0.0.0:8083->80/tcp\n"
}

func TestMachineStatusJSONPrintsOneDocumentPerVerdict(t *testing.T) {
	allPhases := `["` + strings.Join(bootstrap.Phases, `","`) + `"]`
	tests := []struct {
		name     string
		exec     connection.Exec
		wantOut  string
		wantCode int
	}{
		{
			"matches",
			probeBoxSSH{probeOutputWithMarker(`{"schema_version":2,"access_mode":"public","completed_phases":`+allPhases+`}`, "allow")},
			`{"schema_version":1,"box":"root@203.0.113.10","verdict":"matches","access_mode":"public","findings":[{"phase":"smith-user","fact":"smith user","expected":"present","actual":"present","status":"match"},{"phase":"smith-user","fact":"passwordless sudo","expected":"present","actual":"present","status":"match"},{"phase":"firewall","fact":"ufw active","expected":"active","actual":"active","status":"match"},{"phase":"firewall","fact":"ufw default-deny incoming","expected":"deny","actual":"deny","status":"match"},{"phase":"firewall","fact":"public SSH (port 22)","expected":"allow","actual":"allow","status":"match"},{"phase":"ssh-hardening","fact":"PermitRootLogin","expected":"no","actual":"no","status":"match"},{"phase":"ssh-hardening","fact":"PasswordAuthentication","expected":"no","actual":"no","status":"match"},{"phase":"fail2ban","fact":"fail2ban service","expected":"running","actual":"running","status":"match"},{"phase":"auto-updates","fact":"unattended security upgrades","expected":"enabled","actual":"enabled","status":"match"}],"memory_kib":4026532,"swap_kib":0,"docker":{"ports":[{"binding":"web 0.0.0.0:8083-\u003e80/tcp","exposed":true}],"unreadable":false}}` + "\n", 0,
		},
		{
			"drifted",
			probeBoxSSH{probeOutputWithMarker(`{"schema_version":2,"access_mode":"public","completed_phases":`+allPhases+`}`, "deny")},
			`{"schema_version":1,"box":"root@203.0.113.10","verdict":"drifted","access_mode":"public","findings":[{"phase":"smith-user","fact":"smith user","expected":"present","actual":"present","status":"match"},{"phase":"smith-user","fact":"passwordless sudo","expected":"present","actual":"present","status":"match"},{"phase":"firewall","fact":"ufw active","expected":"active","actual":"active","status":"match"},{"phase":"firewall","fact":"ufw default-deny incoming","expected":"deny","actual":"deny","status":"match"},{"phase":"firewall","fact":"public SSH (port 22)","expected":"allow","actual":"deny","status":"drift"},{"phase":"ssh-hardening","fact":"PermitRootLogin","expected":"no","actual":"no","status":"match"},{"phase":"ssh-hardening","fact":"PasswordAuthentication","expected":"no","actual":"no","status":"match"},{"phase":"fail2ban","fact":"fail2ban service","expected":"running","actual":"running","status":"match"},{"phase":"auto-updates","fact":"unattended security upgrades","expected":"enabled","actual":"enabled","status":"match"}],"memory_kib":4026532,"swap_kib":0,"docker":{"ports":[{"binding":"web 0.0.0.0:8083-\u003e80/tcp","exposed":true}],"unreadable":false}}` + "\n", 1,
		},
		{
			"partial",
			probeBoxSSH{probeOutputWithMarker(`{"schema_version":2,"access_mode":"public","completed_phases":["swap"]}`, "allow")},
			`{"schema_version":1,"box":"root@203.0.113.10","verdict":"partial","access_mode":"public","findings":[],"missing_phases":["packages","tmux-oom-policy","smith-user","smith-keys","firewall","ssh-hardening","fail2ban","auto-updates","access"],"memory_kib":4026532,"swap_kib":0,"docker":{"ports":[{"binding":"web 0.0.0.0:8083-\u003e80/tcp","exposed":true}],"unreadable":false}}` + "\n", 2,
		},
		{
			"never set up",
			probeBoxSSH{"mem-total-kb=4026532\nswap-total-kb=0\n"},
			`{"schema_version":1,"box":"root@203.0.113.10","verdict":"never_set_up","findings":[],"memory_kib":4026532,"swap_kib":0,"docker":{"ports":[],"unreadable":false}}` + "\n", 2,
		},
		{
			"unreachable",
			unconnectableSSH{stderr: "ssh: connect to host 203.0.113.10 port 22: Connection timed out\n"},
			`{"schema_version":1,"box":"root@203.0.113.10","verdict":"unreachable","findings":[],"error":"ssh root@203.0.113.10: could not connect to box: the box did not answer; check the address, that sshd is listening on port 22, and the provider's firewall"}` + "\n", 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runStatus(t, t.TempDir(), tt.exec, "root@203.0.113.10", "--json")

			if stdout != tt.wantOut {
				t.Errorf("stdout = %s, want %s", stdout, tt.wantOut)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
		})
	}
}

func runStatus(t *testing.T, dir string, exec connection.Exec, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(
		func() (config.Home, error) { return config.NewHome(dir), nil },
		exec, &fakeDialer{}, provider.SystemClock(),
	)
	cmd.SetArgs(append([]string{"status"}, args...))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

func TestMachineStatusJSONOfAnUnregisteredNameThatDoesNotAnswerPrintsNoDocument(t *testing.T) {
	stdout, stderr, code := runStatus(t, t.TempDir(), unconnectableSSH{}, "ghost", "--json")

	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "ghost is not a registered box") {
		t.Errorf("stderr = %q, want the prose saying why", stderr)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3 as without --json", code)
	}
}
