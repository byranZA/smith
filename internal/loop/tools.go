package loop

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/smithtool"
)

// ToolsEnv names the environment variable that hands an agent the socket of
// the smith tools its run serves.
const ToolsEnv = "SMITH_TOOLS"

// registry returns the smith tools this run serves.
func (l Loop) registry() smithtool.Registry { return smithtool.NewRegistry() }

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

// launch runs cmd through the Launcher with a smith tools socket served for
// its duration, its path in SMITH_TOOLS, and closes the socket when the agent
// exits. With no ToolsDir it launches cmd as it is.
func (l Loop) launch(ctx context.Context, cmd agent.Command) error {
	var server *smithtool.Server
	if l.ToolsDir != "" {
		var err error
		if server, err = smithtool.Serve(l.ToolsDir, l.registry(), l.toolServed); err != nil {
			return fmt.Errorf("serve the smith tools: %w", err)
		}
		cmd.Env = append(cmd.Env[:len(cmd.Env):len(cmd.Env)], ToolsEnv+"="+server.Path())
	}
	failure := l.Launcher.Launch(ctx, cmd)
	var closed error
	if server != nil {
		closed = server.Close()
	}
	return errors.Join(failure, closed)
}

// toolServed shows the operator one smith tool call the agent made.
func (l Loop) toolServed(call smithtool.Served) {
	outcome := "succeeded"
	if call.Err != nil {
		outcome = "failed: " + call.Err.Error()
	}
	_ = l.report("smith: tool %s %s in %s\n", call.Name, outcome, call.Took.Round(100*time.Millisecond))
}
