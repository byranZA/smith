package shipped_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/shipped"
)

func TestFirstRunShipsTheScriptAndRunsTheSubcommand(t *testing.T) {
	b := newBox()
	s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")

	if err := s.Run(context.Background(), io.Discard, io.Discard, "probe"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(b.runs) != 1 || b.runs[0].script != "echo hi" || !slices.Equal(b.runs[0].args, []string{"probe"}) {
		t.Errorf("box ran %+v, want the shipped script's probe subcommand once", b.runs)
	}
}

func TestLaterRunsReuseTheSameCopy(t *testing.T) {
	b := newBox()
	s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")

	for _, sub := range []string{"tailscale-status", "enroll"} {
		if err := s.Run(context.Background(), io.Discard, io.Discard, sub); err != nil {
			t.Fatalf("Run(%s) error = %v", sub, err)
		}
	}

	if b.made != 1 {
		t.Errorf("box made %d private directories, want the script shipped once", b.made)
	}
	if len(b.runs) != 2 || b.runs[0].dir != b.runs[1].dir {
		t.Errorf("box ran %+v, want both subcommands from the same directory", b.runs)
	}
}

func TestTwoHandlesNeverShareADirectory(t *testing.T) {
	b := newBox()
	for range 2 {
		s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")
		if err := s.Run(context.Background(), io.Discard, io.Discard, "probe"); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	}

	if len(b.runs) != 2 || b.runs[0].dir == b.runs[1].dir {
		t.Errorf("box ran %+v, want each handle's copy in its own directory", b.runs)
	}
}

func TestAHandleThatNeverRunsShipsNothing(t *testing.T) {
	b := newBox()
	s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")

	s.Close(context.Background())

	if b.sent != 0 {
		t.Errorf("box received %d operations, want none", b.sent)
	}
}

func TestArgumentsReachTheScriptVerbatim(t *testing.T) {
	for _, arg := range []string{
		"https://example.com/smith.tar.gz",
		"a value with spaces",
		"it's quoted; $(not run) `not run`",
	} {
		t.Run(arg, func(t *testing.T) {
			b := newBox()
			s := shipped.New(b.login("smith"), "install.sh", "echo hi")

			if err := s.Run(context.Background(), io.Discard, io.Discard, "install", arg); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			if len(b.runs) != 1 || !slices.Equal(b.runs[0].args, []string{"install", arg}) {
				t.Errorf("box ran %+v, want install with %q as one argument", b.runs, arg)
			}
		})
	}
}

func TestTheDirectoryPathMayHoldShellMetacharacters(t *testing.T) {
	b := newBox()
	b.tmpdir = "/home/smith/my tmp $HOME"
	s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")

	if err := s.Run(context.Background(), io.Discard, io.Discard, "probe"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(b.runs) != 1 || b.runs[0].dir != "/home/smith/my tmp $HOME/smith.00000001" {
		t.Errorf("box ran %+v, want probe from the copy under the metacharacter TMPDIR", b.runs)
	}
}

func TestOutputStreamsBackToTheStage(t *testing.T) {
	b := newBox()
	b.scriptStdout, b.scriptStderr = "fact=yes\n", "warning: slow mirror\n"
	s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")

	var stdout, stderr bytes.Buffer
	if err := s.Run(context.Background(), &stdout, &stderr, "probe"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if stdout.String() != "fact=yes\n" || stderr.String() != "warning: slow mirror\n" {
		t.Errorf("streams = %q / %q, want the script's stdout and stderr", stdout.String(), stderr.String())
	}
}

func TestASubcommandThatFailsIsReportedAsHavingRun(t *testing.T) {
	b := newBox()
	b.scriptErr = errors.New("exit status 3")
	s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")

	err := s.Run(context.Background(), io.Discard, io.Discard, "probe")

	if err == nil || errors.Is(err, shipped.ErrNotShipped) || errors.Is(err, connection.ErrConnect) {
		t.Errorf("Run() error = %v, want a run failure that is neither a ship nor a connect failure", err)
	}
}

func TestAnUnreachableBoxIsAConnectFailure(t *testing.T) {
	b := newBox()
	b.unreachable = true
	s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")

	err := s.Run(context.Background(), io.Discard, io.Discard, "probe")

	if !errors.Is(err, connection.ErrConnect) || errors.Is(err, shipped.ErrNotShipped) {
		t.Errorf("Run() error = %v, want a connect failure", err)
	}
}

func TestTheBoxRejectingTheShipIsAShipFailure(t *testing.T) {
	tests := []struct {
		name    string
		fails   func(*box)
		boxText string
	}{
		{
			"cannot make a private directory",
			func(b *box) {
				b.mktempStderr = "mktemp: failed to create directory via template: No space left on device\n"
			},
			"No space left on device",
		},
		{
			"refuses the copy into the directory",
			func(b *box) { b.copyErr = errors.New("scp smith@box: exit status 1: scp: write: Disk quota exceeded") },
			"Disk quota exceeded",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBox()
			tt.fails(b)
			s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")

			err := s.Run(context.Background(), io.Discard, io.Discard, "probe")

			if !errors.Is(err, shipped.ErrNotShipped) {
				t.Fatalf("Run() error = %v, want ErrNotShipped", err)
			}
			if msg := err.Error(); !strings.Contains(msg, "bootstrap.sh") || !strings.Contains(msg, tt.boxText) {
				t.Errorf("Run() error = %q, want it to name bootstrap.sh and carry %q", msg, tt.boxText)
			}
			if len(b.runs) != 0 {
				t.Errorf("box ran %+v, want nothing run", b.runs)
			}
		})
	}
}

func TestAFailedCopyLeavesNoDirectoryBehind(t *testing.T) {
	b := newBox()
	b.copyErr = errors.New("scp: exit status 1")
	s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")

	if err := s.Run(context.Background(), io.Discard, io.Discard, "probe"); err == nil {
		t.Fatal("Run() error = nil, want the copy failure")
	}

	if left := b.leftovers(); len(left) != 0 {
		t.Errorf("box still holds %q, want the directory removed", left)
	}
}

// shippedOn returns a handle that has shipped and run once on b.
func shippedOn(t *testing.T, b *box) *shipped.Script {
	t.Helper()
	s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")
	if err := s.Run(context.Background(), io.Discard, io.Discard, "probe"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return s
}

func TestCloseRemovesThePrivateDirectory(t *testing.T) {
	b := newBox()
	s := shippedOn(t, b)

	s.Close(context.Background())

	if left := b.leftovers(); len(left) != 0 {
		t.Errorf("box still holds %q, want the directory removed", left)
	}
}

func TestCloseAfterAnInterruptedCommandStillCleansUp(t *testing.T) {
	b := newBox()
	s := shippedOn(t, b)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s.Close(ctx)

	if len(b.removed) != 1 || b.removed[0].ctxErr != nil {
		t.Errorf("removals = %+v, want one attempted on a live context", b.removed)
	}
}

func TestAnUnresponsiveBoxCannotHangClose(t *testing.T) {
	t.Parallel()
	b := newBox()
	s := shippedOn(t, b)
	b.unresponsive = true

	start := time.Now()
	s.Close(context.Background())

	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("Close() took %v, want it to give up within a few seconds", elapsed)
	}
}

func TestAFailedRemovalIsAttemptedOnceAndSwallowed(t *testing.T) {
	b := newBox()
	s := shippedOn(t, b)
	b.rmErr = errors.New("rm: Permission denied")

	s.Close(context.Background())
	s.Close(context.Background())

	if len(b.removed) != 1 {
		t.Errorf("removals = %+v, want exactly one attempt", b.removed)
	}
}

func TestClosingTwiceIsHarmless(t *testing.T) {
	b := newBox()
	s := shippedOn(t, b)
	s.Close(context.Background())
	sent := b.sent

	s.Close(context.Background())

	if b.sent != sent {
		t.Errorf("second Close sent %d operations, want none", b.sent-sent)
	}
}

func TestInputReachesTheSubcommandOnStdinNotItsArguments(t *testing.T) {
	b := newBox()
	s := shipped.New(b.login("smith"), "bootstrap.sh", "echo hi")

	err := s.RunWithInput(context.Background(), strings.NewReader("tskey-secret"), io.Discard, io.Discard, "enroll", "--hostname", "dev")
	if err != nil {
		t.Fatalf("RunWithInput() error = %v", err)
	}

	if len(b.runs) != 1 || b.runs[0].stdin != "tskey-secret" || !slices.Equal(b.runs[0].args, []string{"enroll", "--hostname", "dev"}) {
		t.Errorf("box ran %+v, want enroll --hostname dev fed tskey-secret on stdin", b.runs)
	}
}

func TestLaterSubcommandsFollowTheMoveFromTheSameCopy(t *testing.T) {
	b := newBox()
	s := shipped.New(b.login("public"), "bootstrap.sh", "echo hi")
	if err := s.Run(context.Background(), io.Discard, io.Discard, "enroll"); err != nil {
		t.Fatalf("Run(enroll) error = %v", err)
	}

	s.Move(b.login("tailnet"))
	if err := s.Run(context.Background(), io.Discard, io.Discard, "close-public-ssh"); err != nil {
		t.Fatalf("Run(close-public-ssh) error = %v", err)
	}

	if b.made != 1 || len(b.runs) != 2 || b.runs[1].over != "tailnet" || b.runs[1].dir != b.runs[0].dir {
		t.Errorf("box made %d directories and ran %+v, want close-public-ssh over the tailnet from the first copy", b.made, b.runs)
	}
}

func TestCloseRemovesTheCopyOverTheAddressItMovedTo(t *testing.T) {
	b := newBox()
	public := b.login("public")
	s := shipped.New(public, "bootstrap.sh", "echo hi")
	if err := s.Run(context.Background(), io.Discard, io.Discard, "enroll"); err != nil {
		t.Fatalf("Run(enroll) error = %v", err)
	}
	s.Move(b.login("tailnet"))
	public.gone = true

	s.Close(context.Background())

	if left := b.leftovers(); len(left) != 0 {
		t.Errorf("box still holds %q, want the copy removed over the tailnet", left)
	}
}

func TestMovingBeforeAnythingShippedShipsOverTheNewAddress(t *testing.T) {
	b := newBox()
	public := b.login("public")
	public.gone = true
	s := shipped.New(public, "bootstrap.sh", "echo hi")

	s.Move(b.login("tailnet"))
	if err := s.Run(context.Background(), io.Discard, io.Discard, "tailscale-status"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(b.runs) != 1 || b.runs[0].over != "tailnet" {
		t.Errorf("box ran %+v, want the script shipped and run over the tailnet", b.runs)
	}
}

func TestTheFinalSubcommandIsToldWhereTheCopyLives(t *testing.T) {
	b := newBox()
	s := shipped.New(b.login("root"), "bootstrap.sh", "echo hi")

	if err := s.RunFinal(context.Background(), io.Discard, io.Discard, "setup", "--access", "public"); err != nil {
		t.Fatalf("RunFinal() error = %v", err)
	}

	if len(b.runs) != 1 || !slices.Equal(b.runs[0].args, []string{"setup", "--access", "public", "--remove-dir", b.runs[0].dir}) {
		t.Errorf("box ran %+v, want setup handed its own directory to remove", b.runs)
	}
}

func TestTheHandleIsSpentAfterTheFinalSubcommand(t *testing.T) {
	for name, scriptErr := range map[string]error{
		"final subcommand succeeds": nil,
		"final subcommand fails":    errors.New("exit status 1"),
	} {
		t.Run(name, func(t *testing.T) {
			b := newBox()
			b.scriptErr = scriptErr
			s := shipped.New(b.login("root"), "bootstrap.sh", "echo hi")
			if err := s.RunFinal(context.Background(), io.Discard, io.Discard, "setup"); (err != nil) != (scriptErr != nil) {
				t.Fatalf("RunFinal() error = %v, want an error only when the subcommand fails", err)
			}

			s.Close(context.Background())

			if len(b.removed) != 0 {
				t.Errorf("removals = %+v, want none once the final subcommand has run", b.removed)
			}
		})
	}
}

func TestRunningAfterTheFinalSubcommandIsRefused(t *testing.T) {
	runs := map[string]func(*shipped.Script) error{
		"Run": func(s *shipped.Script) error {
			return s.Run(context.Background(), io.Discard, io.Discard, "probe")
		},
		"RunWithInput": func(s *shipped.Script) error {
			return s.RunWithInput(context.Background(), strings.NewReader("x"), io.Discard, io.Discard, "enroll")
		},
		"RunFinal": func(s *shipped.Script) error {
			return s.RunFinal(context.Background(), io.Discard, io.Discard, "setup")
		},
	}
	for name, run := range runs {
		t.Run(name, func(t *testing.T) {
			b := newBox()
			s := shipped.New(b.login("root"), "bootstrap.sh", "echo hi")
			if err := s.RunFinal(context.Background(), io.Discard, io.Discard, "setup"); err != nil {
				t.Fatalf("RunFinal() error = %v", err)
			}
			sent := b.sent

			err := run(s)

			if err == nil || b.sent != sent {
				t.Errorf("%s after the final subcommand = %v and sent %d operations, want a refusal that sends none", name, err, b.sent-sent)
			}
		})
	}
}
