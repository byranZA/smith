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
	capped  bool
	filed   []Filed
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
	for attempt := 1; attempt <= l.Limits.MaxAttempts; attempt++ {
		if *runs >= l.Limits.MaxIterations {
			return reviewed{dir: result.dir, capped: true}, nil
		}
		var findings review.File
		if findings, result.dir, result.failure, err = l.reviewAttempt(ctx, number, file, adapter.Unattended(prompt, l.Review.Preset.Options())); err != nil {
			return reviewed{}, err
		}
		*runs++
		if result.failure != nil {
			if err := l.report("smith: the review of #%d failed: %v\n", number, result.failure); err != nil {
				return reviewed{}, err
			}
			if errors.Is(result.failure, ErrNotStarted) {
				return result, nil
			}
			continue
		}
		result.filed, err = l.postReview(ctx, number, findings, tip)
		return result, err
	}
	return result, nil
}

// reviewAttempt makes one attempt at the review: it opens and prepares the
// worktree, removes the findings file an earlier attempt left, runs cmd there
// and reads the file it leaves. A failed attempt is returned as failure; err
// is the run failing.
func (l Loop) reviewAttempt(ctx context.Context, number int, file string, cmd agent.Command) (findings review.File, dir string, failure, err error) {
	dir, err = l.Bench.OpenReview(ctx, number)
	if err != nil {
		return review.File{}, "", nil, fmt.Errorf("open a worktree for the review of #%d: %w", number, err)
	}
	cmd.Dir = dir
	if err := l.Placements.into(ctx, dir); err != nil {
		return review.File{}, dir, nil, fmt.Errorf("place files into the review worktree of #%d in %s: %w", number, dir, err)
	}
	if err := l.prepare(ctx, tracker.Task{Number: number}, dir); err != nil {
		return review.File{}, dir, nil, err
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return review.File{}, dir, nil, fmt.Errorf("remove the findings file %s: %w", file, err)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return review.File{}, dir, nil, fmt.Errorf("make room for the findings file %s: %w", file, err)
	}
	if err := l.report("smith: handing the review of #%d to %s on %s in %s\n", number, l.Review.Preset.Agent.Value, presetPhrase(l.Review.Preset), dir); err != nil {
		return review.File{}, dir, nil, err
	}
	failure = l.Launcher.Launch(ctx, cmd)
	if ctx.Err() != nil {
		return review.File{}, dir, nil, fmt.Errorf("review of #%d interrupted: %w", number, ctx.Err())
	}
	if failure != nil {
		return review.File{}, dir, fmt.Errorf("run the review agent: %w", failure), nil
	}
	data, rerr := os.ReadFile(file)
	if rerr != nil {
		return review.File{}, dir, fmt.Errorf("the agent left no findings file at %s: %w", file, rerr), nil
	}
	findings, perr := review.Parse(data)
	if perr != nil {
		return review.File{}, dir, fmt.Errorf("read the findings file %s: %w", file, perr), nil
	}
	return findings, dir, nil, nil
}

// postReview posts findings, of the work branch at tip, as the review of
// issue number, files each as a task under it, blockers first, edits the
// review to name the tasks, then marks the issue reviewed. It returns the
// tasks it filed in the order it filed them.
func (l Loop) postReview(ctx context.Context, number int, findings review.File, tip string) ([]Filed, error) {
	id, err := l.Tracker.Post(ctx, number, review.Comment(findings, number, tip, nil))
	if err != nil {
		return nil, fmt.Errorf("post the review on #%d: %w", number, err)
	}
	ids := review.IDs(findings.Findings)
	tasks := map[int]int{}
	var filed []Filed
	for _, i := range review.Order(findings.Findings) {
		f := findings.Findings[i]
		task := tracker.NewTask{Title: f.Title, Body: review.TaskBody(f, ids[i], number), For: tracker.Agent}
		if f.Who == "human" {
			task.For = tracker.Human
		}
		for _, b := range f.BlockedBy {
			task.BlockedBy = append(task.BlockedBy, tasks[b-1])
		}
		if tasks[i], err = l.Tracker.File(ctx, number, task); err != nil {
			return filed, fmt.Errorf("file finding %s of the review of #%d (%d of %d filed): %w", ids[i], number, len(filed), len(findings.Findings), err)
		}
		filed = append(filed, Filed{Number: tasks[i], ID: ids[i]})
	}
	if len(filed) > 0 {
		if err := l.Tracker.EditComment(ctx, id, review.Comment(findings, number, tip, tasks)); err != nil {
			return filed, fmt.Errorf("name the filed tasks in the review of #%d: %w", number, err)
		}
	}
	if err := l.Tracker.MarkReviewed(ctx, number); err != nil {
		return filed, fmt.Errorf("mark #%d as reviewed: %w", number, err)
	}
	return filed, nil
}

// stopReviewDetached removes the reviewing mark from issue number as a
// review ends, on a context detached from ctx so it runs even once ctx is
// cancelled, and bounded by detachedStopTimeout, warning when it cannot.
func (l Loop) stopReviewDetached(ctx context.Context, number int) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedStopTimeout)
	defer cancel()
	return l.warnIf(l.Tracker.StopReview(ctx, number), "could not remove the review mark from #%d", number)
}
