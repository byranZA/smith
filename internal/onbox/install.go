// Package onbox puts smith on a box and keeps the binary there converged to the
// version the operator's own smith runs.
//
// The box fetches its own binary: the operator is likely darwin/arm64 and the
// box linux/amd64, and smith is a binary rather than a toolchain, so it cannot
// cross-compile itself onto a box. What travels over the connection is a small
// shell artifact and a URL; the download, the verification and the install all
// happen on the box, which can reach GitHub for the same reason it can reach
// its apt mirrors.
//
// Install and upgrade are one operation. Converge probes what the box has,
// leaves a box already at the version alone, and otherwise downloads to a temp
// path, verifies the archive against the release's published checksums, and
// only then replaces the binary — check-before-change, as everywhere else in
// the setup domain. Every failure before that last step leaves the existing
// binary untouched, so a box whose upgrade fails is still a working box at its
// old version.
//
// It converges the binary and nothing else: no phase runs, no configuration is
// staged, and the marker is not written. The marker records the smith that
// provisioned the box, and `smith version` on the box is ground truth for what
// is installed — a stored copy of that could drift, and a half-failed upgrade
// would leave it lying.
package onbox

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/relay"
	"github.com/byranZA/smith/internal/release"
)

// Script is the embedded install.sh, scp'd to the box and run there. It is a
// separate artifact from bootstrap.sh deliberately: the base layer provisions
// any box, and how smith distributes itself does not belong in it.
//
//go:embed install.sh
var Script string

// RemoteScriptPath is where install.sh is placed on the box before it runs,
// beside — not inside — the bootstrap script's own path.
const RemoteScriptPath = "/tmp/smith-install.sh"

// InstallPath is where smith lives on a box: an absolute path on the default
// PATH, so the relay can invoke it without depending on a login shell's
// environment and an operator who SSHes in can just type `smith`.
//
// It is the path the relay invokes, spelled once: what the installer writes and
// what the relay runs are the same file by construction rather than by two
// constants agreeing.
const InstallPath = relay.BoxSmith

// Conn is the narrow slice of a connection the installer needs: ship a file and
// run a remote command with streamed output. It mirrors bootstrap.Conn — a box
// is reached the same way here as everywhere else in the setup domain.
type Conn interface {
	Copy(ctx context.Context, localPath, remotePath string) error
	Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error
}

// Result is what converging the binary did: the version the box runs
// afterwards, the version it ran before (empty when it carried no smith), the
// release architecture its assets were fetched for, and whether anything was
// downloaded at all.
type Result struct {
	// Version is the version the box runs once the run finished.
	Version string
	// Previous is the version it ran before, empty when it had no smith.
	Previous string
	// Arch is the release architecture the box's assets are built for.
	Arch string
	// Changed reports whether the binary was replaced. False means the box
	// already matched and nothing was downloaded.
	Changed bool
}

// Report renders what the operator is told the run did to the binary. A move is
// always reported as matching local smith, in either direction: the
// post-condition is no version skew rather than "the box is newest", so a box
// ahead of the operator is moved backwards and is owed the reason.
func (r Result) Report() string {
	switch {
	case !r.Changed:
		return fmt.Sprintf("smith %s already matches local smith\n", r.Version)
	case r.Previous == "":
		return fmt.Sprintf("installed smith %s\n", r.Version)
	default:
		return fmt.Sprintf("smith %s → %s (matching local smith)\n", r.Previous, r.Version)
	}
}

// Installer converges the smith binary on one box over a connection.
type Installer struct {
	conn Conn
	box  string
}

// NewInstaller returns an Installer that reaches the box over conn. The box is
// the name or address the operator typed, named back in what the run reports so
// a failure is theirs to act on rather than an ssh destination to decode.
func NewInstaller(conn Conn, box string) *Installer {
	return &Installer{conn: conn, box: box}
}

// Converge brings the box's smith binary to version, and reports what it did.
//
// It probes first: a box already at the version downloads nothing, and a box
// whose machine hardware name smith publishes no asset for is refused by that
// name with nothing fetched. Otherwise the box downloads the release asset for
// its architecture to a temp path, verifies it against the release's published
// checksums, and installs it root-owned and 0755 — one step, so a failed
// download or a checksum mismatch aborts with the box's existing binary
// untouched.
//
// The version is the caller's: it is local smith's own, so the two sides match
// by construction rather than by policy. Whether that version has a release to
// fetch at all is settled before a connection is opened, by release.Installable.
func (i *Installer) Converge(ctx context.Context, version string) (Result, error) {
	if err := bootstrap.Ship(ctx, i.conn, Script, RemoteScriptPath); err != nil {
		return Result{}, fmt.Errorf("ship the install script: %w", err)
	}

	state, err := i.probe(ctx)
	if err != nil {
		return Result{}, err
	}
	arch, err := release.Arch(state.machine)
	if err != nil {
		return Result{}, fmt.Errorf("install smith on the box: %w", err)
	}
	if state.version == version {
		return Result{Version: version, Previous: state.version, Arch: arch}, nil
	}

	asset := release.For(version, arch)
	cmd := fmt.Sprintf("bash %s install --url %s --checksums-url %s",
		RemoteScriptPath, connection.ShellArg(asset.URL), connection.ShellArg(asset.ChecksumsURL))
	if err := i.conn.Run(ctx, cmd, io.Discard, io.Discard); err != nil {
		return Result{}, fmt.Errorf("install smith %s on the box: %w", version, err)
	}
	if err := i.confirm(ctx, version); err != nil {
		return Result{}, err
	}
	return Result{Version: version, Previous: state.version, Arch: arch, Changed: true}, nil
}

// confirm proves what the install just did rather than assuming it: the binary
// now on the box is asked its version through the relay, declaring the version
// that installed it. It is the one step of the run that goes through the same
// channel every on-box verb will, so a box that came out of this stage is a box
// the relay is known to work against.
//
// It travels with --relayed-from deliberately, and the probe above deliberately
// does not: a box whose smith disagrees refuses a declared command, which is
// exactly the answer wanted here — but a probe that declared a version would be
// refused by the very skew it exists to detect, and the stage could then never
// converge anything. Only the confirmation declares.
func (i *Installer) confirm(ctx context.Context, version string) error {
	var out bytes.Buffer
	verb := relay.Verb{Target: i.box, Version: version, Args: []string{"version"}}
	if err := relay.Send(ctx, i.conn, verb, &out, io.Discard); err != nil {
		return fmt.Errorf("confirm the smith %s installed on the box: %w", version, err)
	}
	if reported := parseVersionReport(out.String()); reported != version {
		return fmt.Errorf("the smith installed on box %s reports version %q, not the %s that was installed", i.box, reported, version)
	}
	return nil
}

// parseVersionReport reads the version out of what `smith version` prints,
// whose first line is the version and whose later lines are build provenance.
func parseVersionReport(output string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	_, version, ok := strings.Cut(strings.TrimSpace(first), " ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(version)
}

// boxState is what the box reports about itself before anything is downloaded:
// its machine hardware name, and the smith version it already runs (empty when
// it carries none).
type boxState struct {
	machine string
	version string
}

// probe reads the box's machine hardware name and installed smith version. It
// mutates nothing: it is the check half of check-before-change, and a box that
// already matches never gets past it.
//
// It declares no relaying version — see confirm, which explains why the two
// halves of this stage differ on that.
func (i *Installer) probe(ctx context.Context) (boxState, error) {
	var out bytes.Buffer
	cmd := fmt.Sprintf("bash %s probe", RemoteScriptPath)
	if err := i.conn.Run(ctx, cmd, &out, io.Discard); err != nil {
		return boxState{}, fmt.Errorf("probe the box's smith: %w", err)
	}
	return parseProbe(out.String()), nil
}

// The lines install.sh's probe subcommand reports, read back here. A box with
// no smith reports no version line at all, which is how "never installed" is
// told from "installed and reporting".
const (
	archPrefix    = "arch="
	versionPrefix = "smith-version="
)

// parseProbe reads the probe subcommand's output into the facts it reports.
func parseProbe(output string) boxState {
	var state boxState
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, archPrefix); ok {
			state.machine = v
		}
		if v, ok := strings.CutPrefix(line, versionPrefix); ok {
			state.version = v
		}
	}
	return state
}
