package loop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/prepare"
	"github.com/byranZA/smith/internal/smithtool"
	"github.com/byranZA/smith/internal/tailbuf"
)

// ToolsEnv names the environment variable that hands an agent the socket of
// the smith tools its run serves.
const ToolsEnv = "SMITH_TOOLS"

// toolRecord is what one agent launch's smith tool calls came to.
type toolRecord struct {
	// reups is how many times the agent re-upped the stack.
	reups int
	// reupFailed reports whether the last re-up failed.
	reupFailed bool
}

// toolLog collects one launch's tool calls: the record for the attempt report
// and the failures to show the operator a call.
type toolLog struct {
	mu     sync.Mutex
	record toolRecord
	// reportErrs are the failures to show the operator a call, kept for launch to return.
	reportErrs []error
}

// noteReport keeps err, when set, so launch returns it: the operator's view of
// a tool call must not be lost silently.
func (t *toolLog) noteReport(err error) {
	if err != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.reportErrs = append(t.reportErrs, err)
	}
}

// refused shows the operator a call turned away before it changed anything.
func (l Loop) refused(log *toolLog, name string, err error) {
	log.noteReport(l.report("smith tool: %s refused (%s)\n", name, err))
}

// reup runs the stack's recorded up for one stack-up call, showing the
// operator how it ended, with the output tail of a failure, and counting it in
// log. It returns the up's own error.
func (l Loop) reup(ctx context.Context, log *toolLog, out io.Writer) error {
	began := time.Now()
	captured := tailbuf.Buffer{Limit: PrepareTailLimit}
	err := l.Stack.Reup(ctx, io.MultiWriter(out, &captured))
	took := time.Since(began).Round(time.Second)
	if err == nil {
		log.noteReport(l.report("smith tool: %s ok (%s)\n", stackUpTool, took))
	} else {
		result := err.Error()
		var step *prepare.StepError
		if errors.As(err, &step) && step.Status != 0 {
			result = "exit " + strconv.Itoa(step.Status)
		}
		log.noteReport(l.report("smith tool: %s failed (%s, %s)\n", stackUpTool, result, took))
		log.noteReport(l.reportTail(&captured))
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	log.record.reups++
	log.record.reupFailed = err != nil
	return err //nolint:wrapcheck // the tool call reports the up's own error as it is
}

// registry returns the smith tools this run serves, noting in log what the
// agent's calls come to. stack-up is there only when smith runs the stack in
// this run.
func (l Loop) registry(log *toolLog) smithtool.Registry {
	if !l.stackRunning() {
		return smithtool.NewRegistry().Withhold(stackUpTool, "smith runs no stack in this run")
	}
	return smithtool.NewRegistry(smithtool.Tool{
		Name:    stackUpTool,
		Summary: "re-run the stack's recorded up in this worktree",
		Run: func(ctx context.Context, args []string, out io.Writer) error {
			if len(args) > 0 {
				err := smithtool.Refuse(fmt.Sprintf("%s takes no arguments, got %q", stackUpTool, strings.Join(args, " ")))
				l.refused(log, stackUpTool, err)
				return err //nolint:wrapcheck // a refusal is returned as it is, for the server to tell apart
			}
			return l.reup(ctx, log, out)
		},
	})
}

// stackRunning reports whether smith runs a stack for the launch now: the
// loop executes stacks and the prepared worktree's stack is up.
func (l Loop) stackRunning() bool { return l.Stack != nil && l.Stack.Running() }

// stackUpTool is the name of the tool that re-runs the stack's recorded up.
const stackUpTool = "stack-up"

// prepareTools readies the tools directory for the run, refusing before any
// task is marked when a socket's path would be too long. It does nothing when
// the loop serves no tools.
func (l Loop) prepareTools() error {
	if l.ToolsDir == "" {
		return nil
	}
	if err := smithtool.PrepareDir(l.ToolsDir); err != nil {
		return fmt.Errorf("prepare the smith tools directory: %w", err)
	}
	return nil
}

// startTools readies the tools directory and notes a prompt without the tools
// placeholder, before a run marks any task.
func (l Loop) startTools() error {
	if err := l.prepareTools(); err != nil {
		return err
	}
	return l.noteToolsPlaceholder()
}

// launch runs cmd through the Launcher with a smith tools socket served for
// its duration (path in SMITH_TOOLS), or as it is with no ToolsDir. It returns
// what the agent's tool calls came to and how the launch ended.
func (l Loop) launch(ctx context.Context, cmd agent.Command) (toolRecord, error) {
	var server *smithtool.Server
	log := &toolLog{}
	if l.ToolsDir != "" {
		var err error
		if server, err = smithtool.Serve(l.ToolsDir, l.registry(log), func(name string, err error) { l.refused(log, name, err) }); err != nil {
			return toolRecord{}, fmt.Errorf("serve the smith tools: %w", err)
		}
		cmd.Env = append(cmd.Env[:len(cmd.Env):len(cmd.Env)], ToolsEnv+"="+server.Path())
	}
	failure := l.Launcher.Launch(ctx, cmd)
	var closed error
	if server != nil {
		closed = server.Close()
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	return log.record, errors.Join(failure, closed, errors.Join(log.reportErrs...))
}

// toolsSection is the prompt text listing the smith tools this run serves,
// empty when it serves none.
func (l Loop) toolsSection() string {
	if l.ToolsDir == "" || !l.stackRunning() {
		return ""
	}
	return smithToolsSection
}

// noteToolsPlaceholder tells the operator at run start when a prompt they
// ejected lacks the tools placeholder, so its agent is not told of the tools
// this run serves. The agent's calls work all the same.
func (l Loop) noteToolsPlaceholder() error {
	if l.ToolsDir == "" || l.Stack == nil {
		return nil
	}
	var lacking []string
	if !strings.Contains(string(l.Prompt), toolsPlaceholder) {
		lacking = append(lacking, "work")
	}
	if l.Review != nil && !strings.Contains(string(l.Review.Prompt), toolsPlaceholder) {
		lacking = append(lacking, "review")
	}
	if len(lacking) == 0 {
		return nil
	}
	return l.report("smith: the ejected %s prompt has no %s placeholder, so its agent is not told of the smith tools; calls still work\n", strings.Join(lacking, " and "), toolsPlaceholder)
}
