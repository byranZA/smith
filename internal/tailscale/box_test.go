package tailscale

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/shipped"
)

// address is the box reached at one host; the driver only hands it to its
// shipped script, so it never answers a call itself.
type address string

func (a address) Copy(context.Context, string, string) error {
	return fmt.Errorf("copy over %s: the shipped script owns copies", a)
}

func (a address) Run(context.Context, string, io.Writer, io.Writer) error {
	return fmt.Errorf("run over %s: the shipped script owns runs", a)
}

func (a address) RunWithInput(context.Context, string, io.Reader, io.Writer, io.Writer) error {
	return fmt.Errorf("run over %s: the shipped script owns runs", a)
}

func dialAddress(host string) shipped.Conn { return address(host) }

// bootstrapAt returns a recording bootstrap.sh that starts over the public
// host and answers each subcommand with its scripted reply.
func bootstrapAt(replies map[string]shipped.Reply) *shipped.Fake {
	return &shipped.Fake{Conn: address("203.0.113.10"), Replies: replies}
}

func TestBoxDriverReadsTheTailnetIPFromTailscaleStatus(t *testing.T) {
	script := bootstrapAt(map[string]shipped.Reply{"tailscale-status": {Stdout: "tailscale-ip=100.64.0.1\n"}})

	ip, err := NewBox(script, dialAddress).CurrentIP(context.Background())
	if err != nil {
		t.Fatalf("CurrentIP() error = %v", err)
	}

	if ip != "100.64.0.1" {
		t.Errorf("CurrentIP() = %q, want 100.64.0.1", ip)
	}
}

func TestBoxDriverEnrollsWithTheAuthKeyOnStdin(t *testing.T) {
	script := bootstrapAt(map[string]shipped.Reply{"enroll": {Stdout: "tailscale-ip=100.64.0.2\n"}})

	if _, err := NewBox(script, dialAddress).Enroll(context.Background(), EnrollOptions{Host: "dev", AuthKey: "tskey-test"}); err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}

	want := shipped.Call{Sub: "enroll", Args: []string{"--hostname", "smith-dev"}, Input: "tskey-test", Over: address("203.0.113.10")}
	if len(script.Calls) != 1 || !sameCall(script.Calls[0], want) {
		t.Errorf("calls = %+v, want %+v", script.Calls, want)
	}
}

func TestBoxDriverEnrollThatNeverReachesRunningIsAttributed(t *testing.T) {
	tests := []struct {
		name  string
		reply shipped.Reply
	}{
		{"enroll fails", shipped.Reply{Err: errors.New("exit status 1")}},
		{"enroll reports no IP", shipped.Reply{Stdout: "waiting\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := bootstrapAt(map[string]shipped.Reply{"enroll": tt.reply})

			_, err := NewBox(script, dialAddress).Enroll(context.Background(), EnrollOptions{Host: "dev", AuthKey: "tskey-test"})

			if !errors.Is(err, ErrEnrollNotRunning) {
				t.Errorf("Enroll() error = %v, want ErrEnrollNotRunning", err)
			}
		})
	}
}

func TestBoxDriverEnrollThatNeverRanIsNotAttributedToRunning(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"cannot connect", fmt.Errorf("ssh: %w", connection.ErrConnect)},
		{"cannot ship", fmt.Errorf("ship bootstrap.sh: %w", shipped.ErrNotShipped)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := bootstrapAt(map[string]shipped.Reply{"enroll": {Err: tt.err}})

			_, err := NewBox(script, dialAddress).Enroll(context.Background(), EnrollOptions{Host: "dev", AuthKey: "tskey-test"})

			if !errors.Is(err, tt.err) || errors.Is(err, ErrEnrollNotRunning) {
				t.Errorf("Enroll() error = %v, want %v unattributed to Running", err, tt.err)
			}
		})
	}
}

func TestBoxDriverMovedToTheTailnetClosesPublicSSHAndCleansUpThere(t *testing.T) {
	script := bootstrapAt(nil)
	driver := NewBox(script, dialAddress)

	driver.MoveToTailnet("100.64.0.2")
	if err := driver.ClosePublicSSH(context.Background()); err != nil {
		t.Fatalf("ClosePublicSSH() error = %v", err)
	}
	driver.Close(context.Background())

	want := shipped.Call{Sub: "close-public-ssh", Over: address("100.64.0.2")}
	if len(script.Calls) != 1 || !sameCall(script.Calls[0], want) {
		t.Errorf("calls = %+v, want %+v", script.Calls, want)
	}
	if script.ClosedOver != address("100.64.0.2") {
		t.Errorf("closed over %v, want the shipped script removed over the tailnet", script.ClosedOver)
	}
}

func TestBoxDriverCleansUpOverThePublicHostBeforeAMove(t *testing.T) {
	script := bootstrapAt(nil)

	NewBox(script, dialAddress).Close(context.Background())

	if script.ClosedOver != address("203.0.113.10") {
		t.Errorf("closed over %v, want the shipped script removed over the public host", script.ClosedOver)
	}
}

// sameCall reports whether got records the subcommand, arguments, input and
// connection that want does.
func sameCall(got, want shipped.Call) bool {
	return got.Sub == want.Sub && slices.Equal(got.Args, want.Args) && got.Input == want.Input && got.Over == want.Over && got.Final == want.Final
}
