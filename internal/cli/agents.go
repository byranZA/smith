package cli

import (
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/agentguide"
)

// The annotation keys a verb declares its traits under.
const (
	traitRelays        = "smith/relays"
	traitRunsLong      = "smith/runs-long"
	traitNeedsTerminal = "smith/needs-terminal"
)

// verbTraits returns the annotations declaring whether a verb relays to a box, runs long and needs a terminal.
func verbTraits(relays, runsLong, needsTerminal bool) map[string]string {
	return map[string]string{
		traitRelays:        strconv.FormatBool(relays),
		traitRunsLong:      strconv.FormatBool(runsLong),
		traitNeedsTerminal: strconv.FormatBool(needsTerminal),
	}
}

// guideVerbs turns the runnable, visible leaves under root into the verb description the agent guide renders, sorted by path.
func guideVerbs(root *cobra.Command) []agentguide.Verb {
	var verbs []agentguide.Verb
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			if child.Hidden || child.Name() == "help" || child.Name() == "completion" {
				continue
			}
			if child.HasSubCommands() {
				walk(child)
				continue
			}
			if !child.Runnable() {
				continue
			}
			verbs = append(verbs, agentguide.Verb{
				Path:          child.CommandPath(),
				Summary:       child.Short,
				Relays:        child.Annotations[traitRelays] == "true",
				RunsLong:      child.Annotations[traitRunsLong] == "true",
				NeedsTerminal: child.Annotations[traitNeedsTerminal] == "true",
			})
		}
	}
	walk(root)
	sort.Slice(verbs, func(i, j int) bool { return verbs[i].Path < verbs[j].Path })
	return verbs
}

// newAgentsTopic builds the `smith help agents` help topic, which prints the agent guide for the command tree it is added to. It has no Run, so cobra lists it under "Additional help topics".
func newAgentsTopic() *cobra.Command {
	topic := &cobra.Command{
		Use:   "agents",
		Short: "How an agent that plans work drives smith",
	}
	topic.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		codes := make([]agentguide.ExitCode, len(exitCodes))
		for i, c := range exitCodes {
			codes[i] = agentguide.ExitCode{Code: c.code, Meaning: c.meaning}
		}
		guide := agentguide.Render(guideVerbs(cmd.Root()), codes)
		_, _ = cmd.OutOrStdout().Write([]byte(strings.TrimRight(guide, "\n") + "\n"))
	})
	return topic
}
