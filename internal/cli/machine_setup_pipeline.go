package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/onbox"
	"github.com/byranZA/smith/internal/tailscale"
)

// pipelineRun is one run of the setup pipeline: what its stages need to reach
// the box, and the tailnet address the access stage settles for the stages
// after it.
//
// The stages run from the operator's machine, after every phase has completed,
// so none of them can revert or damage what the phases established. What they
// share is the reach: the address the box is answering on right now, which the
// access stage is the only thing that moves.
type pipelineRun struct {
	// accessMode is the access layer the run is under: "public" or "tailscale".
	accessMode string
	// host is the host the run reached the box at before any stage ran.
	host string
	// exec is the local process boundary every stage reaches a binary over.
	exec connection.Exec
	// access is the tailscale access orchestrator, nil in public mode.
	access *tailscale.Access
	// acquireKey resolves the tailnet auth key, called only if the box actually
	// needs enrolling.
	acquireKey func() (string, error)
	// box is the name or address the operator typed, named back in a refusal
	// that has a command for them to re-run.
	box string
	// smithVersion is the version the install stage puts on the box: the one
	// the operator named, else the version local smith runs.
	smithVersion string
	// localVersion is the version local smith runs, which every relay declares.
	localVersion string
	// staged is the operator's blueprint, resolved before the box was touched.
	// Nil when the run names no blueprint, which stages and converges nothing.
	staged *stagedConfig

	// tailnetIP is the tailnet address the access stage established, empty
	// until it has and in public mode always.
	tailnetIP string
}

// stages returns the pipeline's ordered stages.
//
// The order is forced rather than stylistic. Access runs first because it
// settles the address the rest of the pipeline reaches the box by: a tailscale
// run closes public SSH, so every later stage travels over the tailnet. Install
// runs second because the stages after it run on the box's own smith, and a box
// cannot install the binary that would have to be there to install it (see
// docs/adr/0008). Config staging runs before the workspace stage because that
// stage reads the document and the placement bytes staging writes.
func (r *pipelineRun) stages(stdout, stderr io.Writer) []bootstrap.Stage {
	return []bootstrap.Stage{
		{Name: "access", Run: func(ctx context.Context) error {
			return r.establishAccess(ctx, stdout)
		}},
		{Name: "install", Run: func(ctx context.Context) error {
			return installSmith(ctx, connection.New(r.reach(), r.exec), r.smithVersion, r.box, stdout)
		}},
		{Name: "config", Run: func(ctx context.Context) error {
			return stageConfig(ctx, connection.New(r.reach(), r.exec), r.staged, stdout)
		}},
		{Name: "workspace", Run: func(ctx context.Context) error {
			return convergeWorkspace(ctx, r.exec, r.reach(), r.localVersion, r.staged, stdout, stderr)
		}},
	}
}

// reach is the ssh target the pipeline's stages reach the box at: the smith
// user, on the tailnet address once the access stage established one and on the
// host the run came in over until then. The bootstrap-in login is dead by this
// point — hardening closed it — so every stage travels as the smith user, whose
// passwordless sudo is what lets them write outside its home.
func (r *pipelineRun) reach() string {
	if r.tailnetIP != "" {
		return smithTarget(r.tailnetIP)
	}
	return smithTarget(r.host)
}

// establishAccess runs the access stage: in tailscale mode it enrolls the box,
// proves reach over the tailnet and closes public SSH, and records the address
// the rest of the pipeline reaches the box by. Public mode has no admin-side
// access layer — the phases left hardened SSH open on the public IP — so the
// stage runs and does nothing.
func (r *pipelineRun) establishAccess(ctx context.Context, stdout io.Writer) error {
	if r.accessMode != "tailscale" {
		return nil
	}
	result, err := establishTailscale(ctx, r.access, r.host, r.acquireKey, stdout)
	if err != nil {
		return err
	}
	r.tailnetIP = result.TailnetIP
	return nil
}

// installSmith runs the install stage: it converges the box's smith binary to
// the version the run named, through the one convergence `machine upgrade`
// runs too, and reports what it did to the binary.
//
// It hands that convergence the connection the pipeline already reaches the box
// over rather than resolving the box a second time — this stage is inside a run
// that reached the box several stages ago, and the address it answers on is the
// access stage's to say.
//
// The refusal names `machine setup`, because that is the command the operator
// ran: a build with no published release is refused before the box is reached,
// so a dev build sets a box up all the way through the phases and fails here
// alone — which is what keeps a from-source build useful for the whole setup
// domain.
//
// Nothing about the installed binary is written down: `smith version` on the
// box is ground truth, and the marker's smith_version keeps its own meaning,
// the smith that provisioned the box.
func installSmith(ctx context.Context, conn onbox.Conn, version, box string, stdout io.Writer) error {
	run := binaryConvergence{box: box, version: version, command: "smith machine setup"}
	converged, err := run.converge(ctx, func() (onbox.Conn, error) { return conn, nil })
	if err != nil {
		return err
	}
	if _, err := fmt.Fprint(stdout, converged.Result.Report()); err != nil {
		return fmt.Errorf("write install report: %w", err)
	}
	return nil
}

// reportStageFailure reports a stage of the pipeline that failed and exits
// partial. Every phase completed, so the box is provisioned and secured and the
// setup is not a failed one: what stopped is a step that runs on top of a box
// that is already there, and the report says which.
func reportStageFailure(stderr io.Writer, err error) error {
	var failure *bootstrap.StageFailure
	if !errors.As(err, &failure) {
		return err
	}
	if _, werr := fmt.Fprint(stderr, failure.Report()); werr != nil {
		return fmt.Errorf("write stage failure: %w", werr)
	}
	return &exitError{code: bootstrap.OutcomePartial.ExitCode()}
}
