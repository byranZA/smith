package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/relay"
)

// exitCodes is every code smith can exit with, paired with what it means. It
// is the one place a code's meaning is written: `smith help exit-codes` prints
// it, and each verb's help names the codes it returns from it.
var exitCodes = []struct {
	code    int
	meaning string
}{
	{bootstrap.OutcomePassed.ExitCode(), "The verb did what was asked. For machine status, the box matches what setup established."},
	{bootstrap.OutcomePartial.ExitCode(), "A general failure, said on stderr: the verb was refused or could not finish. For setup, the pipeline ran only in part; for machine status, the box has drifted. A relayed verb whose box has no smith installed exits 1 too, naming the setup command."},
	{bootstrap.OutcomeRejected.ExitCode(), "A gate rejected the box, or refused the request, before anything changed. For machine status, the box was never set up or its setup is partial."},
	{bootstrap.OutcomeConnectFailed.ExitCode(), "smith could not connect to the box."},
	{exitStagedBlueprintAbsent, "The box has no staged blueprint or resolution: it was never set up with one, or by a smith that predates it. `smith machine setup` stages one."},
	{exitStagedBlueprintMalformed, "The staged blueprint or resolution on the box cannot be trusted: it was truncated or hand-edited after setup staged it."},
	{relay.RefusalExitCode, "The box runs another version of smith and refused the relayed verb. `smith machine upgrade <box>` converges it."},
	{relay.NotInstalledExitCode, "Never smith's own exit: the shell's \"command not found\", which a box with no smith installed answers a relayed verb with. smith reports it as 1."},
}

// newExitCodesTopic builds the `smith help exit-codes` help topic. It has no
// Run, so cobra lists it under "Additional help topics" rather than as a verb.
func newExitCodesTopic() *cobra.Command {
	var b strings.Builder
	b.WriteString("Every code smith can exit with. Each verb's help names the ones it returns.\n")
	for _, c := range exitCodes {
		fmt.Fprintf(&b, "\n  %-4d %s", c.code, c.meaning)
	}
	return &cobra.Command{
		Use:   "exit-codes",
		Short: "What each code smith exits with means",
		Long:  b.String(),
	}
}

// relayedVerb is the paragraph every verb taking an optional leading box shares
// in its help: what naming a box does, and the exits the staged config and the
// relay add.
const relayedVerb = `A leading box relays the verb over ssh to the smith installed on that box,
which runs it there and exits with its own code; with no box it runs on this
machine. Either way it reads the blueprint staged where it runs: none staged
exits 4, one that cannot be trusted exits 5. A box running another version of
smith refuses the relay and exits 6.`

// exits renders the last line of a verb's Long: the codes it can return, and a
// pointer to the topic that says what they mean.
func exits(codes ...int) string {
	names := make([]string, len(codes))
	for i, c := range codes {
		names[i] = strconv.Itoa(c)
	}
	last := len(names) - 1
	listed := names[last]
	if last > 0 {
		listed = strings.Join(names[:last], ", ") + " or " + names[last]
	}
	return fmt.Sprintf(`Exits %s; see "smith help exit-codes".`, listed)
}
