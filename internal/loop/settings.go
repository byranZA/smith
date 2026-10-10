package loop

import (
	"fmt"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/repofile"
)

// Settings are the resolved loop settings, each carrying where it came from.
// An unset model or effort resolves to the empty value from the built-in
// default, which leaves the agent to its own default.
type Settings struct {
	// Agent names the agent adapter each task is handed to.
	Agent config.Value
	// Model is the model the agent runs, passed to it verbatim.
	Model config.Value
	// Effort is how hard the agent thinks, on smith's own scale.
	Effort config.Value
	// Push is "on" or "off": whether the loop pushes the branch after each
	// task the agent closes.
	Push config.Value
}

// ResolveSettings resolves each setting field by field as flag, then repo
// file, then built-in default, refusing an unknown agent or an effort off the
// scale and naming where it came from.
func ResolveSettings(flags, file repofile.File) (Settings, error) {
	s := Settings{
		Agent:  resolveSetting(flags.Agent, file.Agent, agent.Default),
		Model:  resolveSetting(flags.Model, file.Model, ""),
		Effort: resolveSetting(flags.Effort, file.Effort, ""),
		Push:   resolveSwitch(flags.Push, file.Push, true),
	}
	if _, err := agent.Lookup(s.Agent.Value); err != nil {
		return Settings{}, fmt.Errorf("agent (%s): %w", s.Agent.Origin, err)
	}
	if _, err := agent.ParseEffort(s.Effort.Value); err != nil {
		return Settings{}, fmt.Errorf("effort (%s): %w", s.Effort.Origin, err)
	}
	return s, nil
}

// Options are the model and effort the settings ask every agent run for.
func (s Settings) Options() agent.Options {
	return agent.Options{Model: s.Model.Value, Effort: agent.Effort(s.Effort.Value)}
}

// Pushes reports whether the settings have the loop push after each closed task.
func (s Settings) Pushes() bool { return s.Push.Value == switchValue(true) }

// resolveSwitch picks the most specific of a flag and a repo file on/off
// setting, falling back to the built-in default when neither is set.
func resolveSwitch(flag, file *bool, fallback bool) config.Value {
	switch {
	case flag != nil:
		return config.Value{Value: switchValue(*flag), Origin: config.FromFlag}
	case file != nil:
		return config.Value{Value: switchValue(*file), Origin: config.FromRepoFile}
	default:
		return config.Value{Value: switchValue(fallback), Origin: config.FromDefault}
	}
}

// switchValue names an on/off setting's state as the settings report shows it.
func switchValue(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// resolveSetting picks the most specific of a flag and a repo file value,
// falling back to the built-in default when neither is set.
func resolveSetting(flag, file, fallback string) config.Value {
	switch {
	case flag != "":
		return config.Value{Value: flag, Origin: config.FromFlag}
	case file != "":
		return config.Value{Value: file, Origin: config.FromRepoFile}
	default:
		return config.Value{Value: fallback, Origin: config.FromDefault}
	}
}
