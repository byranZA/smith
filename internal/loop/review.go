package loop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/preset"
	"github.com/byranZA/smith/internal/review"
	"github.com/byranZA/smith/internal/tracker"
)

// Review turns the review role on for a Loop: once every task of an
// unattended run's execution is closed, one agent reads the whole work branch
// and writes its findings to a file the loop reads.
type Review struct {
	// Preset is the preset the review agent runs on.
	Preset preset.Resolved
	// Prompt is the prompt the review agent is handed.
	Prompt ReviewPrompt
}

// reviewable reports whether execution is one a review may be run for: a spec
// or a standalone task, not yet reviewed. A child task run on its own never
// reviews; its spec's run reviews the whole branch.
func reviewable(execution tracker.Execution) bool {
	return !execution.Reviewed && (!execution.Single || execution.Standalone())
}

// reviewDue reports whether the run owes execution, whose tasks are sorted as
// remaining, a review it has not run yet.
func (l Loop) reviewDue(execution tracker.Execution, remaining Remaining, done bool) bool {
	return l.Review != nil && !done && reviewable(execution) && remaining.Complete()
}

// reviewed is how a review went: the worktree it ran in, and why it failed
// when it did.
type reviewed struct {
	dir     string
	failure error
	// broken reports that failure is of the review's agent on every attempt,
	// or that it could not be started.
	broken bool
	capped bool
	filed  []Filed
	// incomplete is set when a tracker failure stopped the filing.
	incomplete *Incomplete
	// note says what the report should note about the review.
	note string
}

// Incomplete is a review whose filing a tracker failure stopped: the ledger
// is posted, Filed of its Total findings have a task, and Err is the failure.
type Incomplete struct {
	Filed, Total int
	Err          error
}

// review runs the review agent on execution's work branch, up to MaxAttempts
// times while the run's cap of agent runs, counted in runs, allows, and when
// the agent leaves a valid findings file posts the review on the execution's
// issue and marks it reviewed. The issue carries the reviewing mark while it
// runs, removed on every exit. An error is the run failing; a review that
// fails is reported in the result.
func (l Loop) review(ctx context.Context, execution tracker.Execution, runs *int) (result reviewed, err error) {
	if *runs >= l.Limits.MaxIterations {
		return reviewed{capped: true}, nil
	}
	number, title := execution.Issue()
	adapter, err := agent.Lookup(l.Review.Preset.Agent.Value)
	if err != nil {
		return reviewed{}, fmt.Errorf("choose the agent of the review: %w", err)
	}
	mergeBase, tip, err := l.Bench.ReviewBase(ctx)
	if err != nil {
		return reviewed{}, fmt.Errorf("find the work under review: %w", err)
	}
	if err := l.warnIf(l.Tracker.StartReview(ctx, number), "could not mark #%d as under review", number); err != nil {
		return reviewed{}, err
	}
	defer func() { err = errors.Join(err, l.stopReviewDetached(ctx, number)) }()
	file := l.Bench.FindingsFile(number)
	prompt := l.Review.Prompt.Render(ReviewTarget{Number: number, Title: title, MergeBase: mergeBase, FindingsFile: file})
	agentFailures := 0
	for attempt := 1; attempt <= l.Limits.MaxAttempts; attempt++ {
		if *runs >= l.Limits.MaxIterations {
			return reviewed{dir: result.dir, capped: true}, nil
		}
		var findings review.File
		var agentFailed bool
		if findings, result.dir, result.failure, agentFailed, err = l.reviewAttempt(ctx, number, file, tip, adapter.Unattended(prompt, l.Review.Preset.Options())); err != nil {
			return reviewed{}, err
		}
		if errors.Is(result.failure, ErrNotStarted) {
			result.broken = true
			return result, l.report("smith: the review of #%d failed: %v\n", number, result.failure)
		}
		*runs++
		if result.failure == nil {
			result.filed, result.incomplete, err = l.postReview(ctx, number, findings, tip)
			return result, err
		}
		if err := l.report("smith: the review of #%d failed: %v\n", number, result.failure); err != nil {
			return reviewed{}, err
		}
		if errors.Is(result.failure, errStrayWork) {
			return result, nil
		}
		if agentFailed {
			agentFailures++
		}
	}
	result.broken = agentFailures > 0 && agentFailures == l.Limits.MaxAttempts
	return result, nil
}

// errStrayWork is what a review that changed its worktree fails with.
var errStrayWork = errors.New("the review changed the work branch it was to leave alone")

// reviewAttempt makes one attempt at the review: it opens and prepares the
// worktree, removes the findings file an earlier attempt left, runs cmd there
// and reads the file it leaves. A failed attempt is returned as failure; err
// is the run failing. agentFailed reports that failure is the agent's own
// exit, not a file it left invalid. A review that commits or changes files
// fails with errStrayWork, naming what it left, whatever else it did.
func (l Loop) reviewAttempt(ctx context.Context, number int, file, tip string, cmd agent.Command) (findings review.File, dir string, failure error, agentFailed bool, err error) {
	dir, err = l.Bench.OpenReview(ctx, number)
	if err != nil {
		return review.File{}, "", nil, false, fmt.Errorf("open a worktree for the review of #%d: %w", number, err)
	}
	cmd.Dir = dir
	if err := l.Placements.into(ctx, dir); err != nil {
		return review.File{}, dir, nil, false, fmt.Errorf("place files into the review worktree of #%d in %s: %w", number, dir, err)
	}
	if err := l.prepare(ctx, tracker.Task{Number: number}, dir); err != nil {
		return review.File{}, dir, nil, false, err
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return review.File{}, dir, nil, false, fmt.Errorf("remove the findings file %s: %w", file, err)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return review.File{}, dir, nil, false, fmt.Errorf("make room for the findings file %s: %w", file, err)
	}
	if err := l.report("smith: handing the review of #%d to %s on %s in %s\n", number, l.Review.Preset.Agent.Value, presetPhrase(l.Review.Preset), dir); err != nil {
		return review.File{}, dir, nil, false, err
	}
	failure = l.Launcher.Launch(ctx, cmd)
	if ctx.Err() != nil {
		return review.File{}, dir, nil, false, fmt.Errorf("review of #%d interrupted: %w", number, ctx.Err())
	}
	strays, serr := l.Bench.ReviewStrays(ctx, dir, tip)
	if serr != nil {
		return review.File{}, dir, nil, false, fmt.Errorf("check the review worktree %s for changes: %w", dir, serr)
	}
	if strays.Any() {
		return review.File{}, dir, fmt.Errorf("%w in %s: %s", errStrayWork, dir, strays), false, nil
	}
	if failure != nil {
		return review.File{}, dir, fmt.Errorf("run the review agent: %w", failure), !errors.Is(failure, ErrKilled), nil
	}
	data, rerr := os.ReadFile(file)
	if rerr != nil {
		return review.File{}, dir, fmt.Errorf("the agent left no findings file at %s: %w", file, rerr), false, nil
	}
	findings, perr := review.Parse(data)
	if perr != nil {
		return review.File{}, dir, fmt.Errorf("read the findings file %s: %w", file, perr), false, nil
	}
	return findings, dir, nil, false, nil
}

// postReview posts findings, of the work branch at tip, as the review of
// issue number, then files them. It returns the tasks it filed in the order it
// filed them; a tracker failure after the post is returned as incomplete.
func (l Loop) postReview(ctx context.Context, number int, findings review.File, tip string) (filed []Filed, incomplete *Incomplete, err error) {
	id, err := l.Tracker.Post(ctx, number, review.Comment(findings, number, tip, nil))
	if err != nil {
		return nil, nil, fmt.Errorf("post the review on #%d: %w", number, err)
	}
	ledger := review.Ledger{Sha: tip, File: findings}
	return l.fileFindings(ctx, number, id, ledger, map[int]int{}, review.Order(findings.Findings))
}

// fileFindings files the findings of ledger at the positions in todo, blockers
// first, as tasks under issue number, whose review is the comment id. tasks
// maps the position of each finding already filed to its task, and gains each
// one filed here. It then edits the comment to name every task and marks the
// issue reviewed. A tracker failure stops it at once, without retrying, and is
// returned as incomplete.
func (l Loop) fileFindings(ctx context.Context, number int, id int64, ledger review.Ledger, tasks map[int]int, todo []int) (filed []Filed, incomplete *Incomplete, err error) {
	findings := ledger.File.Findings
	ids := review.IDs(findings)
	stopped := func(err error) ([]Filed, *Incomplete, error) {
		return filed, &Incomplete{Filed: len(tasks), Total: len(findings), Err: err}, nil
	}
	for _, i := range todo {
		f := findings[i]
		task := tracker.NewTask{Title: f.Title, Body: review.TaskBody(f, ids[i], number), For: tracker.Agent}
		if f.Who == "human" {
			task.For = tracker.Human
		}
		for _, b := range f.BlockedBy {
			task.BlockedBy = append(task.BlockedBy, tasks[b-1])
		}
		n, err := l.Tracker.File(ctx, number, task)
		if err != nil {
			return stopped(fmt.Errorf("file finding %s of the review of #%d: %w", ids[i], number, err))
		}
		tasks[i] = n
		filed = append(filed, Filed{Number: n, ID: ids[i]})
	}
	if len(tasks) > 0 {
		if err := l.Tracker.EditComment(ctx, id, review.Comment(ledger.File, number, ledger.Sha, tasks)); err != nil {
			return stopped(fmt.Errorf("name the filed tasks in the review of #%d: %w", number, err))
		}
	}
	if err := l.Tracker.MarkReviewed(ctx, number); err != nil {
		return stopped(fmt.Errorf("mark #%d as reviewed: %w", number, err))
	}
	// Only now is the review finished: a ledger marked done is history, and
	// removing the reviewed mark afterwards asks for a fresh review.
	if err := l.warnIf(l.Tracker.EditComment(ctx, id, review.CommentDone(ledger.File, number, ledger.Sha, tasks)), "could not mark the review of #%d as done", number); err != nil {
		return filed, nil, err
	}
	return filed, nil, nil
}

// ledgerOf finds the ledger a review left among the comments of execution,
// when the run owes the execution a review: the latest comment that holds one.
// A ledger marked done is a finished review, so it is not found. A comment
// that holds a ledger it cannot read is an error.
func (l Loop) ledgerOf(execution tracker.Execution) (ledger review.Ledger, id int64, found bool, err error) {
	if l.Review == nil {
		return review.Ledger{}, 0, false, nil
	}
	return openLedger(execution)
}

// NeedsReviewAgent reports whether a review enabled for execution would run
// a fresh review agent: it is reviewable and carries no unfinished ledger
// whose findings a run files without one.
func NeedsReviewAgent(execution tracker.Execution) (bool, error) {
	if !reviewable(execution) {
		return false, nil
	}
	_, _, found, err := openLedger(execution)
	return !found, err
}

// openLedger finds the unfinished ledger among the comments of a reviewable
// execution, or none.
func openLedger(execution tracker.Execution) (ledger review.Ledger, id int64, found bool, err error) {
	if !reviewable(execution) {
		return review.Ledger{}, 0, false, nil
	}
	number, _ := execution.Issue()
	for _, c := range execution.Comments {
		got, ok, err := review.ParseLedger(c.Body)
		if err != nil {
			return review.Ledger{}, 0, false, fmt.Errorf("read the review ledger in comment %d on #%d: %w", c.ID, number, err)
		}
		if ok {
			ledger, id, found = got, c.ID, true
		}
	}
	if ledger.Done {
		return review.Ledger{}, 0, false, nil
	}
	return ledger, id, found, nil
}

// resume finishes the filing of the review whose ledger, in comment id,
// execution carries, without running an agent: it files the findings no
// sub-issue names, edits the comment to name every task and marks the issue
// reviewed. The issue carries the reviewing mark meanwhile.
func (l Loop) resume(ctx context.Context, execution tracker.Execution, ledger review.Ledger, id int64) (result reviewed, err error) {
	number, _ := execution.Issue()
	_, tip, err := l.Bench.ReviewBase(ctx)
	if err != nil {
		return reviewed{}, fmt.Errorf("find the work under review: %w", err)
	}
	if err := l.warnIf(l.Tracker.StartReview(ctx, number), "could not mark #%d as under review", number); err != nil {
		return reviewed{}, err
	}
	defer func() { err = errors.Join(err, l.stopReviewDetached(ctx, number)) }()
	var subs []review.Sub
	for _, t := range execution.Tasks {
		if t.Number != number {
			subs = append(subs, review.Sub{Number: t.Number, Body: t.Body})
		}
	}
	tasks, unfiled := review.Pending(ledger, number, subs)
	if err := l.report("smith: resuming the review of #%d from its ledger: %d of %d findings filed\n", number, len(tasks), len(ledger.File.Findings)); err != nil {
		return reviewed{}, err
	}
	if ledger.Sha != tip {
		result.note = fmt.Sprintf("the work branch moved since the review of #%d: it reviewed %s and the branch is now at %s; its findings were filed anyway", number, ledger.Sha, tip)
		if err := l.report("smith: %s\n", result.note); err != nil {
			return reviewed{}, err
		}
	}
	result.filed, result.incomplete, err = l.fileFindings(ctx, number, id, ledger, tasks, unfiled)
	return result, err
}

// stopReviewDetached removes the reviewing mark from issue number as a
// review ends, on a context detached from ctx so it runs even once ctx is
// cancelled, and bounded by detachedStopTimeout, warning when it cannot.
func (l Loop) stopReviewDetached(ctx context.Context, number int) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedStopTimeout)
	defer cancel()
	return l.warnIf(l.Tracker.StopReview(ctx, number), "could not remove the review mark from #%d", number)
}
