// Package bootstrap is the Go side of the on-box provisioning harness. It ships
// the embedded bootstrap.sh to a box, invokes a subcommand, and turns the
// script's output into a decision plus a process exit code.
//
// Preflight runs the non-recorded gate (privilege + OS support, mutating
// nothing). Setup ships and drives the ordered mutating phases, streaming live
// progress and mapping a mid-run failure to a recovery report. In tailscale
// mode the caller derives the access-aware public-SSH firewall target from
// SSHConnection and drives the admin-side access layer (see the tailscale
// package) around Setup.
package bootstrap

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/osgate"
)

// Script is the embedded bootstrap.sh, scp'd to the box and run there.
//
//go:embed bootstrap.sh
var Script string

// RemoteScriptPath is where bootstrap.sh is placed on the box before it runs.
// The tailscale access layer drives the same shipped script's enroll and
// close-public-ssh subcommands, so it reads this path too.
const RemoteScriptPath = "/tmp/smith-bootstrap.sh"

// Conn is the narrow slice of a connection the runner needs: ship a file and
// run a remote command with streamed output.
type Conn interface {
	Copy(ctx context.Context, localPath, remotePath string) error
	Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error
}

// Outcome is the terminal result of a preflight run. Its ExitCode is the
// process exit code smith returns for that outcome.
type Outcome int

const (
	// OutcomePassed means the box cleared the preflight gate; setup may proceed.
	OutcomePassed Outcome = iota
	// OutcomeRejected means the preflight gate rejected the box (unsupported OS
	// or missing passwordless sudo) before anything was mutated.
	OutcomeRejected
	// OutcomeConnectFailed means smith could not reach the box.
	OutcomeConnectFailed
	// OutcomePartial means a mutating phase failed after setup began: the box is
	// left partial but reachable over the door setup connected on.
	OutcomePartial
)

// ExitCode maps an outcome to smith's process exit code: 0 pass, 1
// partial-but-reachable, 2 gate rejection, 3 connect failure.
func (o Outcome) ExitCode() int {
	switch o {
	case OutcomePassed:
		return 0
	case OutcomeRejected:
		return 2
	case OutcomeConnectFailed:
		return 3
	case OutcomePartial:
		return 1
	default:
		return 1
	}
}

// Result is what a preflight run reports: its outcome, a human-readable reason
// when the box was rejected or unreachable, and the probed facts.
type Result struct {
	Outcome   Outcome
	Reason    string
	Privilege string
	Release   osgate.Release
}

// Report renders a human-readable summary of the preflight result.
func (r Result) Report() string {
	switch r.Outcome {
	case OutcomePassed:
		return "preflight passed\n"
	case OutcomeConnectFailed:
		return fmt.Sprintf("connect failed: %s\n", r.Reason)
	default:
		return fmt.Sprintf("preflight rejected: %s\n", r.Reason)
	}
}

// Runner ships and drives bootstrap.sh over a connection.
type Runner struct {
	conn Conn
}

// NewRunner returns a Runner that reaches the box over conn.
func NewRunner(conn Conn) *Runner {
	return &Runner{conn: conn}
}

// Preflight runs the non-recorded preflight gate: it probes reachability,
// ships bootstrap.sh, runs its preflight subcommand, and applies the OS
// support gate to the reported facts. A connect failure is reported as an
// Outcome (not a Go error); a Go error is returned only for unexpected
// infrastructure failures.
func (r *Runner) Preflight(ctx context.Context) (Result, error) {
	if err := r.conn.Run(ctx, "true", io.Discard, io.Discard); err != nil {
		if errors.Is(err, connection.ErrConnect) {
			return Result{Outcome: OutcomeConnectFailed, Reason: err.Error()}, nil
		}
		return Result{}, fmt.Errorf("reachability probe: %w", err)
	}

	if err := r.ship(ctx); err != nil {
		if errors.Is(err, connection.ErrConnect) {
			return Result{Outcome: OutcomeConnectFailed, Reason: err.Error()}, nil
		}
		return Result{}, fmt.Errorf("ship bootstrap script: %w", err)
	}

	var out bytes.Buffer
	cmd := fmt.Sprintf("bash %s preflight", RemoteScriptPath)
	if err := r.conn.Run(ctx, cmd, &out, io.Discard); err != nil {
		if errors.Is(err, connection.ErrConnect) {
			return Result{Outcome: OutcomeConnectFailed, Reason: err.Error()}, nil
		}
		return Result{}, fmt.Errorf("run preflight: %w", err)
	}

	facts, err := parsePreflight(out.String())
	if err != nil {
		return Result{}, fmt.Errorf("parse preflight output: %w", err)
	}
	return decide(facts), nil
}

// SetupOptions carries the run parameters bootstrap.sh's setup needs: how the
// box is reached, which smith version, box name and blueprint pointer to stamp
// into the marker, and the access-aware public-SSH firewall target.
type SetupOptions struct {
	// AccessMode is the access layer to record and drive: "public" or "tailscale".
	AccessMode string
	// SmithVersion is the smith build recorded in the marker.
	SmithVersion string
	// BoxName is the box's name: the operator-chosen name recorded in the
	// marker. Empty when the run names the box nothing, which is how a box the
	// operator has not named records no name.
	BoxName string
	// Blueprint is the blueprint pointer: the name of the blueprint the box is
	// built from, recorded in the marker. Empty when the run names none, which
	// is how a box built from no blueprint records none.
	Blueprint string
	// PublicSSH is the firewall target for public port 22 ("open" or "closed"),
	// derived by the caller from the access mode and the door smith connected
	// over. Empty defaults to "open".
	PublicSSH string
}

// SetupResult is the terminal result of a setup run: its Outcome (which maps to
// the process exit code) and, when a phase failed mid-run, the FailureReport
// telling the operator what happened and how to recover. Failure is non-nil only
// when Outcome is OutcomePartial.
type SetupResult struct {
	Outcome Outcome
	Failure *FailureReport
}

// Setup runs the ordered mutating phases on the box: it ships bootstrap.sh,
// invokes its setup subcommand, and streams each phase's live progress to
// stdout and stderr as it happens. A connect failure and a phase failure are
// reported in the SetupResult rather than as Go errors, so the caller can map
// them to an exit code; a phase failure also carries a FailureReport built from
// the captured stream. A Go error is returned only for unexpected infrastructure
// failures. Setup assumes the preflight gate has already passed.
func (r *Runner) Setup(ctx context.Context, opts SetupOptions, stdout, stderr io.Writer) (SetupResult, error) {
	if err := r.ship(ctx); err != nil {
		if errors.Is(err, connection.ErrConnect) {
			return SetupResult{Outcome: OutcomeConnectFailed}, nil
		}
		return SetupResult{}, fmt.Errorf("ship bootstrap script: %w", err)
	}

	// Tee the live streams into buffers: the operator still sees progress as it
	// happens, and a phase failure can be reconstructed into a report afterward.
	var outBuf, errBuf bytes.Buffer
	teeOut := io.MultiWriter(stdout, &outBuf)
	teeErr := io.MultiWriter(stderr, &errBuf)

	publicSSH := opts.PublicSSH
	if publicSSH == "" {
		publicSSH = "open"
	}
	cmd := fmt.Sprintf("bash %s setup --access %s --smith-version %s --public-ssh %s%s%s",
		RemoteScriptPath, connection.ShellArg(opts.AccessMode), connection.ShellArg(opts.SmithVersion),
		connection.ShellArg(publicSSH), optionalFlag("--name", opts.BoxName),
		optionalFlag("--blueprint", opts.Blueprint))
	if err := r.conn.Run(ctx, cmd, teeOut, teeErr); err != nil {
		if errors.Is(err, connection.ErrConnect) {
			return SetupResult{Outcome: OutcomeConnectFailed}, nil
		}
		// A non-connect error means a phase ran and failed, leaving a partial box.
		// Build the failure report from the streams the phases just emitted,
		// attributing the still-open reach to the door setup connected over.
		report := newFailureReport(outBuf.String(), errBuf.String(), openDoorForRun(opts))
		return SetupResult{Outcome: OutcomePartial, Failure: &report}, nil
	}
	return SetupResult{Outcome: OutcomePassed}, nil
}

// optionalFlag renders " <flag> <value>" for a value the run has, and nothing
// for one it does not — so a box the run names nothing, or builds from no
// blueprint, is never handed an empty value to record.
func optionalFlag(flag, value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf(" %s %s", flag, connection.ShellArg(value))
}

// SSHConnection reports the box's SSH_CONNECTION for the connection smith is
// reached over ("clientip clientport serverip serverport"), so the caller can
// tell whether smith connected over the tailnet and derive the access-aware
// public-SSH firewall target before setup runs.
func (r *Runner) SSHConnection(ctx context.Context) (string, error) {
	var out bytes.Buffer
	if err := r.conn.Run(ctx, `printf '%s' "$SSH_CONNECTION"`, &out, io.Discard); err != nil {
		return "", fmt.Errorf("probe SSH_CONNECTION: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}

// ship writes the embedded script to a local temp file and scp's it to the box.
func (r *Runner) ship(ctx context.Context) error {
	return ShipScript(ctx, r.conn)
}

// ShipScript writes the embedded bootstrap.sh to a local temp file and copies it
// to the box at RemoteScriptPath over conn. Both the setup runner and the
// read-only status prober ship the same script this way, then invoke a
// subcommand on it.
func ShipScript(ctx context.Context, conn Conn) error {
	f, err := os.CreateTemp("", "smith-bootstrap-*.sh")
	if err != nil {
		return fmt.Errorf("create temp script: %w", err)
	}
	// Best-effort cleanup of the OS temp file: a failed remove is unrecoverable
	// here and harmless (the OS reclaims its temp dir), so the error is
	// deliberately not propagated.
	defer func() { _ = os.Remove(f.Name()) }()

	if _, err := f.WriteString(Script); err != nil {
		writeErr := fmt.Errorf("write temp script: %w", err)
		// The write already failed; close best-effort and join any close error
		// so a failed close can't silently mask the underlying write failure.
		if cerr := f.Close(); cerr != nil {
			return errors.Join(writeErr, fmt.Errorf("close temp script: %w", cerr))
		}
		return writeErr
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp script: %w", err)
	}
	if err := conn.Copy(ctx, f.Name(), RemoteScriptPath); err != nil {
		return fmt.Errorf("copy script to box: %w", err)
	}
	return nil
}

// preflightFacts are the fields bootstrap.sh's preflight reports.
type preflightFacts struct {
	privilege string
	release   osgate.Release
}

// parsePreflight extracts the privilege level and /etc/os-release block from
// the preflight output.
func parsePreflight(output string) (preflightFacts, error) {
	var facts preflightFacts
	var osRelease strings.Builder
	inOSRelease := false

	for _, line := range strings.Split(output, "\n") {
		switch {
		case line == "os-release-begin":
			inOSRelease = true
		case line == "os-release-end":
			inOSRelease = false
		case inOSRelease:
			osRelease.WriteString(line)
			osRelease.WriteByte('\n')
		default:
			if v, ok := strings.CutPrefix(line, "privilege="); ok {
				facts.privilege = strings.TrimSpace(v)
			}
		}
	}

	if facts.privilege == "" {
		return preflightFacts{}, errors.New("preflight output missing privilege line")
	}
	facts.release = osgate.Parse(osRelease.String())
	return facts, nil
}

// decide turns probed facts into a Result. Privilege is checked before the OS
// gate: without a way to run privileged commands, setup cannot proceed at all.
func decide(facts preflightFacts) Result {
	if facts.privilege == "none" {
		return Result{
			Outcome:   OutcomeRejected,
			Reason:    "passwordless sudo is required on a non-root bootstrap login",
			Privilege: facts.privilege,
			Release:   facts.release,
		}
	}
	if d := osgate.Evaluate(facts.release); !d.Supported {
		return Result{
			Outcome:   OutcomeRejected,
			Reason:    fmt.Sprintf("unsupported OS: %s", d.Reason),
			Privilege: facts.privilege,
			Release:   facts.release,
		}
	}
	return Result{Outcome: OutcomePassed, Privilege: facts.privilege, Release: facts.release}
}
