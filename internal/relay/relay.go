// Package relay decides where a session verb runs. One rule: a box target
// means relay it over ssh to the smith installed on that box, no target means
// run it here. There is no flag and no mode, which is what keeps the operator
// typing `smith session list dev` on their laptop and the operator typing
// `smith session list` after SSHing in on one implementation of the verb.
//
// It is the only code that knows the relay is ssh-shaped, and it composes no
// git and no tmux command line: what a session is called on the box is
// internal/session's to know, on the box. It reaches ssh through
// internal/connection, the one exec boundary smith has, rather than growing a
// second one.
package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/byranZA/smith/internal/connection"
)

// BoxSmith is the absolute path the relay invokes on the box. It is absolute
// deliberately: a non-interactive `ssh box smith …` gets a stripped PATH, and
// "works when I SSH in" is exactly the bug not to have in the relay.
const BoxSmith = "/usr/local/bin/smith"

// Verb is one smith invocation and where it is to happen: the box target it
// relays to, empty to run it locally, plus the arguments the box is to run and
// the version of the smith asking for them.
type Verb struct {
	// Target is the ssh destination the verb relays to — a box name already
	// resolved through the inventory, or a literal target the operator wrote
	// out. Empty means the verb runs on this machine.
	Target string
	// Box is the box as the operator named it, kept beside the address it
	// resolved to because a failure the operator is asked to act on has to
	// come back in their own words: `dev` is what they typed and what the
	// command they are pointed at takes, where the target it resolved to is a
	// string they never wrote. Empty means there was no name to keep — a
	// literal target, which is already the operator's own spelling.
	Box string
	// Version is local smith's version, passed to the box as --relayed-from
	// so the box refuses a command line it may not mean the same thing by.
	Version string
	// Args are the smith arguments the box is to run, as the operator's own
	// command line spelled them minus the box target — "session", "list",
	// "--names".
	Args []string
}

// Local is a verb's local implementation, run when no box was named. It is
// passed in rather than reached for so the rule is applied in one place and a
// relayed verb provably never touches the local machine's worktrees.
type Local func() error

// Run runs the verb: relayed to the box when one is named, streaming the box's
// own output to stdout and stderr as it arrives, and through local otherwise.
//
// A box that answers the relay with exit 127 has no smith installed, which is
// reported as a NotInstalledError; any other non-zero exit is the box's own
// refusal — its message has already reached the operator, so it comes back as
// an ExitError carrying only the code to exit with.
func Run(ctx context.Context, exec connection.Exec, v Verb, local Local, stdout, stderr io.Writer) error {
	if v.Target == "" {
		return local()
	}
	return Send(ctx, connection.New(v.Target, exec), v, stdout, stderr)
}

// Conn is the narrow slice of an open connection the relay sends over: run a
// remote command with its output streamed back. It is accepted rather than
// dialled so a caller already holding a connection to the box relays over that
// one — the install stage confirming the binary it just installed does exactly
// that, on the connection it installed over.
type Conn interface {
	// Run runs remoteCmd on the box, streaming its output as it arrives.
	Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error
}

// Send relays a verb over an already-open connection and classifies what came
// back, streaming the box's own output to stdout and stderr as it arrives.
//
// It is the sending half by itself, where Run is the verb-level entry that
// first decides whether there is a box to send to at all. Nothing here talks to
// a terminal: the outcome comes back typed, and what to do about it — prompt,
// print, or converge — belongs to the caller.
//
// Anything that grows a second way to run a verb re-opens ADR-0008: a local
// fast path, or a laptop-side reimplementation "just this once", each recreates
// the drift between the two sides that this relay exists to prevent. One
// implementation, on the box, reached through here.
func Send(ctx context.Context, conn Conn, v Verb, stdout, stderr io.Writer) error {
	watched := &strings.Builder{}
	if err := conn.Run(ctx, v.remoteCmd(), stdout, io.MultiWriter(stderr, watched)); err != nil {
		return classify(v, watched.String(), err)
	}
	return nil
}

// Execer replaces smith's own process with another command. It is the system
// boundary a connecting verb crosses, injected because a real exec never
// returns: a test asserts on the argv smith would have handed the kernel.
type Execer interface {
	// Exec replaces the calling process with name run with args. It returns
	// only when the replacement did not happen.
	Exec(name string, args []string) error
}

// Connect runs a verb that hands the operator a terminal — attach, and the
// start that connects. Relayed, it replaces smith with `ssh -t`, so the
// terminal talks to the box's tmux with no smith process in the middle; with
// no box named it runs local, which execs into tmux itself.
//
// The box is asked whether it would accept the command before the terminal is
// handed over, on an ordinary round trip classified exactly as a streamed
// verb's outcome is. That check cannot be folded into the connecting
// invocation: replacing smith with ssh gives up ever seeing the box's exit
// code, so a refusal reached after the exec has nobody left to react to it,
// and the version-skew decision the operator is owed would never be made. The
// box's own words travel to stderr as they arrive, so what it said reaches the
// operator ahead of whatever the caller does about it.
//
// A box with no smith installed is named as such by the check and by the
// connecting invocation both: the guard on the box covers the binary going
// missing between the two round trips, and costs nothing when it does not.
//
// What that tmux session is called never travels: the box is handed the same
// session verb the operator typed, and the mapping from a session name to a
// tmux session stays in internal/session, on the box.
func Connect(ctx context.Context, exec connection.Exec, replace Execer, v Verb, local Local, stderr io.Writer) error {
	if v.Target == "" {
		return local()
	}
	if err := check(ctx, connection.New(v.Target, exec), v, stderr); err != nil {
		return err
	}
	if err := replace.Exec("ssh", connection.TerminalArgs(v.Target, v.connectCmd())); err != nil {
		return fmt.Errorf("hand the terminal to box %s: %w", v.Target, err)
	}
	return nil
}

// check asks the box whether it would accept a command relayed by this smith,
// and answers with the same typed outcomes a relayed verb's own failure comes
// back as — a version mismatch, a box with no smith, or a box that could not
// be reached.
//
// It relays `version`, which is the one verb every smith has and the one that
// changes nothing on the box: what is being read is not its answer but whether
// the box let it run at all, since the declaration the box refuses on is
// checked before any verb of its own does anything. Its stdout is the box's
// version banner, which the operator did not ask for and never sees.
func check(ctx context.Context, conn Conn, v Verb, stderr io.Writer) error {
	probe := Verb{Target: v.Target, Box: v.Box, Version: v.Version, Args: []string{"version"}}
	return Send(ctx, conn, probe, io.Discard, stderr)
}

// NotInstalledError reports that the box answered the relay with no smith to
// run. It names `smith machine setup` rather than `machine upgrade`: a box in
// this state was provisioned before smith installed itself, so it likely wants
// the rest of the pipeline too.
type NotInstalledError struct {
	// Box is the box as the operator named it, which is the spelling the
	// setup command it points at takes back.
	Box string
}

// Error implements error.
func (e *NotInstalledError) Error() string {
	return fmt.Sprintf("box %s has no smith installed: `smith machine setup %s` installs it, along with the rest of what the box is missing", e.Box, e.Box)
}

// ExitError reports that smith on the box ran and exited non-zero — a refused
// relay, or a verb the box itself refused. The box has already said why on the
// operator's terminal, so this carries the exit code and nothing else, and the
// caller exits with it rather than restating the failure.
type ExitError struct {
	// Code is the exit status the box's smith answered with.
	Code int
	// Target is the ssh destination that ran the verb.
	Target string
}

// Error implements error.
func (e *ExitError) Error() string {
	return fmt.Sprintf("smith on box %s exited %d", e.Target, e.Code)
}

// notInstalled is what a box with no smith at BoxSmith answers with: the
// shell's "command not found" status, or its message when a login shell
// reports the missing file some other way.
const notInstalled = 127

// classify turns a failed relay into the error its cause deserves: a
// provisioning gap that names the setup command, a version mismatch the caller
// can act on, a connection that could not be made, or the box's own non-zero
// exit carried back as a code.
func classify(v Verb, stderr string, err error) error {
	var coder interface{ ExitCode() int }
	if !errors.As(err, &coder) {
		return err
	}
	switch code := coder.ExitCode(); {
	case code == notInstalled || strings.Contains(stderr, BoxSmith+": No such file or directory"):
		return &NotInstalledError{Box: v.named()}
	case code == RefusalExitCode:
		return &MismatchError{Target: v.Target, Box: boxVersion(stderr), Local: v.Version}
	default:
		return &ExitError{Code: code, Target: v.Target}
	}
}

// RefusalExitCode is the status on-box smith exits with when it refuses a
// command relayed by a smith of another version. It is a wire contract between
// the two sides rather than either side's own detail, which is why it lives
// here: the box exits with it, and the relay reads it back as a version
// mismatch instead of as the verb's own failure.
//
// It sits outside the setup family's codes and outside the 127 a box with no
// smith answers with, so no other outcome can be mistaken for it.
const RefusalExitCode = 6

// Refusal renders the message on-box smith prints when it refuses a relayed
// command, naming both versions because the operator reading it cannot
// otherwise tell which side is which.
//
// The wording lives here, with the relay, because both sides depend on it: the
// box prints it, and the relaying side reads the box's version back out of it
// to report the mismatch. local is the version of the smith refusing — the one
// on the box — and relayedFrom the version the relaying smith declared.
func Refusal(local, relayedFrom string) string {
	return fmt.Sprintf("refusing a command relayed from smith %s: this smith is %s, and only an identical version may relay to it", relayedFrom, local)
}

// refusedVersion pulls the refusing smith's own version out of the message
// Refusal renders, and is the only reader of that wording.
var refusedVersion = regexp.MustCompile(`this smith is (\S+),`)

// boxVersion reads the version the box reported in its refusal, empty when
// what came back on stderr was not a refusal this smith knows how to read — an
// older box's wording, say, whose exit code still says what happened.
func boxVersion(stderr string) string {
	match := refusedVersion.FindStringSubmatch(stderr)
	if match == nil {
		return ""
	}
	return match[1]
}

// MismatchError reports that the box refused the relayed command because the
// two smiths are different versions. It is the outcome, not the reaction: the
// box has already printed its own refusal on the operator's terminal, and
// whether to prompt, print an upgrade command or converge the box is the
// caller's to decide.
//
// This is the smith binary on the box against the smith binary on the
// operator's machine. It is not the marker's schema_version against the
// constant a build understands — a different axis entirely, and neither
// message borrows the other's words.
type MismatchError struct {
	// Target is the ssh destination that refused the command.
	Target string
	// Box is the version the box runs, empty when its refusal named none.
	Box string
	// Local is the version this smith declared when it relayed.
	Local string
}

// Error implements error.
func (e *MismatchError) Error() string {
	if e.Box == "" {
		return fmt.Sprintf("box %s refused a command relayed from smith %s: it runs a different version of smith", e.Target, e.Local)
	}
	return fmt.Sprintf("box %s runs smith %s and you run %s: only an identical version may relay to it", e.Target, e.Box, e.Local)
}

// connectCmd renders the command line a connecting verb hands the box: the
// same invocation, but exec'd behind a test for the smith that is to run it.
// Replacing smith with ssh gives up ever seeing the box's exit code, so the
// judgement classify makes after the fact is made on the box before it, and
// the box answers a missing smith with the words and the exit code the other
// verbs answer it with. On a box that has smith the shell is exec'd away, so
// the guard leaves nothing between the operator's terminal and tmux.
func (v Verb) connectCmd() string {
	absent := &NotInstalledError{Box: v.named()}
	return fmt.Sprintf("if [ -x %s ]; then exec %s; fi; printf '%%s\\n' %s >&2; exit 1",
		BoxSmith, v.remoteCmd(), connection.ShellArg(absent.Error()))
}

// named is the box in the operator's own words: the name they typed when
// there was one, and otherwise the target, which they wrote out themselves.
func (v Verb) named() string {
	if v.Box != "" {
		return v.Box
	}
	return v.Target
}

// remoteCmd renders the command line the box runs: the absolute path, the
// version the relay is calling from, and the operator's own arguments, each
// quoted so the box's shell hands them over as written.
func (v Verb) remoteCmd() string {
	parts := []string{BoxSmith, "--relayed-from", connection.ShellArg(v.Version)}
	for _, arg := range v.Args {
		parts = append(parts, connection.ShellArg(arg))
	}
	return strings.Join(parts, " ")
}
