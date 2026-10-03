// Package bootstrap is the Go side of the on-box provisioning harness: it
// drives the shipped bootstrap.sh's preflight and setup subcommands and turns
// their output into a decision plus a process exit code.
package bootstrap

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/osgate"
	"github.com/byranZA/smith/internal/shipped"
)

// Script is the embedded bootstrap.sh, scp'd to the box and run there.
//
//go:embed bootstrap.sh
var Script string

// ShippedScript is the shipped bootstrap.sh a Runner drives by subcommand:
// preflight runs as an ordinary subcommand and setup as the final one, which
// removes the shipped copy itself as it exits.
type ShippedScript interface {
	Run(ctx context.Context, stdout, stderr io.Writer, sub string, args ...string) error
	RunFinal(ctx context.Context, stdout, stderr io.Writer, sub string, args ...string) error
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

// Runner drives bootstrap.sh's preflight and setup subcommands through one
// shipped copy of the script, and probes the bootstrap login's connection.
type Runner struct {
	conn   connection.Runner
	script ShippedScript
}

// NewRunner returns a Runner that probes the box over conn and drives script,
// the shipped bootstrap.sh reaching the box over that same login. Removing
// script when the run is done is the caller's job.
func NewRunner(conn connection.Runner, script ShippedScript) *Runner {
	return &Runner{conn: conn, script: script}
}

// Preflight runs the non-recorded preflight gate: it probes reachability, runs
// bootstrap.sh's preflight subcommand, and applies the OS support gate to the
// reported facts. A connect failure is reported as an Outcome (not a Go error);
// a Go error is returned for a script that could not be shipped and for
// unexpected infrastructure failures.
func (r *Runner) Preflight(ctx context.Context) (Result, error) {
	reachable, err := connection.Reachable(ctx, r.conn)
	if err != nil {
		return Result{}, fmt.Errorf("probe reachability: %w", err)
	}
	if !reachable {
		return Result{Outcome: OutcomeConnectFailed, Reason: connection.ErrConnect.Error()}, nil
	}

	var out bytes.Buffer
	if err := r.script.Run(ctx, &out, io.Discard, "preflight"); err != nil {
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

// Setup runs bootstrap.sh's setup as the final subcommand, streaming each
// phase's progress and reporting a connect or phase failure in the SetupResult,
// while an unshipped script is a Go error wrapping shipped.ErrNotShipped.
// The script removes its own shipped copy as it exits, and Setup assumes
// preflight has passed.
func (r *Runner) Setup(ctx context.Context, opts SetupOptions, stdout, stderr io.Writer) (SetupResult, error) {
	var outBuf, errBuf bytes.Buffer
	teeOut := io.MultiWriter(stdout, &outBuf)
	teeErr := io.MultiWriter(stderr, &errBuf)

	err := r.script.RunFinal(ctx, teeOut, teeErr, "setup", setupArgs(opts)...)
	switch {
	case err == nil:
		return SetupResult{Outcome: OutcomePassed}, nil
	case errors.Is(err, connection.ErrConnect):
		return SetupResult{Outcome: OutcomeConnectFailed}, nil
	case errors.Is(err, shipped.ErrNotShipped):
		return SetupResult{}, fmt.Errorf("run setup: %w", err)
	}
	report := newFailureReport(outBuf.String(), errBuf.String(), openDoorForRun(opts))
	return SetupResult{Outcome: OutcomePartial, Failure: &report}, nil
}

// setupArgs renders opts as setup's arguments, defaulting the public-SSH
// target to open.
func setupArgs(opts SetupOptions) []string {
	publicSSH := opts.PublicSSH
	if publicSSH == "" {
		publicSSH = "open"
	}
	args := []string{"--access", opts.AccessMode, "--smith-version", opts.SmithVersion, "--public-ssh", publicSSH}
	args = append(args, optionalFlag("--name", opts.BoxName)...)
	return append(args, optionalFlag("--blueprint", opts.Blueprint)...)
}

// optionalFlag renders flag and value for a value the run has, and nothing for
// one it does not — so a box the run names nothing, or builds from no
// blueprint, is never handed an empty value to record.
func optionalFlag(flag, value string) []string {
	if value == "" {
		return nil
	}
	return []string{flag, value}
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
