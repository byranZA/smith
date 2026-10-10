package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/provider"
)

// probeFailingSSH answers the reach check and refuses every later ssh launch.
type probeFailingSSH struct{ stderr string }

func (p probeFailingSSH) Run(_ context.Context, name string, args []string, _ io.Reader, _, stderr io.Writer) error {
	if name == "ssh" && len(args) > 0 && args[len(args)-1] == "true" {
		return nil
	}
	if _, err := io.WriteString(stderr, p.stderr); err != nil {
		return err
	}
	return refusedExit{}
}

func TestMachineStatusKeepsTheClassifiedConnectFailure(t *testing.T) {
	failures := []struct {
		name   string
		stderr string
		want   []string
	}{
		{
			"key refused",
			"root@203.0.113.10: Permission denied (publickey).\n",
			[]string{"refused the key", "ssh-agent", "Host entry in ~/.ssh/config", "login"},
		},
		{
			"no answer",
			"ssh: connect to host 203.0.113.10 port 22: Connection timed out\n",
			[]string{"did not answer", "root@203.0.113.10", "the address", "sshd is listening on port 22", "provider's firewall"},
		},
		{
			"unrecognised",
			"debug1: reading config\nkex_exchange_identification: read: Connection reset by peer\n",
			[]string{"kex_exchange_identification: read: Connection reset by peer"},
		},
	}
	stages := []struct {
		name string
		exec func(stderr string) connection.Exec
	}{
		{"reach check", func(s string) connection.Exec { return unconnectableSSH{stderr: s} }},
		{"shipped probe", func(s string) connection.Exec { return probeFailingSSH{stderr: s} }},
	}
	for _, stage := range stages {
		for _, tt := range failures {
			t.Run(stage.name+"/"+tt.name, func(t *testing.T) {
				cmd := newMachineCmd(
					func() (config.Home, error) { return config.NewHome(t.TempDir()), nil },
					stage.exec(tt.stderr), &fakeDialer{}, provider.SystemClock(),
				)
				cmd.SetArgs([]string{"status", "root@203.0.113.10"})
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&out)
				cmd.SilenceUsage, cmd.SilenceErrors = true, true

				if code := codeFromError(cmd.Execute()); code != 3 {
					t.Errorf("exit code = %d, want 3 for an unreachable box (output: %s)", code, out.String())
				}
				for _, want := range tt.want {
					if !strings.Contains(out.String(), want) {
						t.Errorf("output = %q, want it to name %q", out.String(), want)
					}
				}
			})
		}
	}
}
