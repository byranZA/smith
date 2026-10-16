package loop

import (
	"fmt"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/repofile"
	"github.com/byranZA/smith/internal/tracker"
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
	// AgentLabel, HumanLabel and SpecLabel name the tracker's triage labels.
	// They are set in the repo file alone, never by a flag.
	AgentLabel config.Value
	HumanLabel config.Value
	SpecLabel  config.Value
}

// ResolveSettings resolves each setting field by field as flag, then repo
// file, then built-in default, refusing an unknown agent, an effort off the
// scale, a triage label named for one of the loop's own marks, or two triage
// labels sharing a name, and naming where each came from.
// The triage labels are read from the repo file alone, whatever flags holds.
func ResolveSettings(flags, file repofile.File) (Settings, error) {
	defaults := tracker.DefaultLabels()
	s := Settings{
		Agent:      resolveSetting(flags.Agent, file.Agent, agent.Default),
		Model:      resolveSetting(flags.Model, file.Model, ""),
		Effort:     resolveSetting(flags.Effort, file.Effort, ""),
		Push:       resolveSwitch(flags.Push, file.Push, true),
		AgentLabel: resolveSetting("", file.Labels.Agent, defaults.Agent),
		HumanLabel: resolveSetting("", file.Labels.Human, defaults.Human),
		SpecLabel:  resolveSetting("", file.Labels.Spec, defaults.Spec),
	}
	if _, err := agent.Lookup(s.Agent.Value); err != nil {
		return Settings{}, fmt.Errorf("agent (%s): %w", s.Agent.Origin, err)
	}
	if _, err := agent.ParseEffort(s.Effort.Value); err != nil {
		return Settings{}, fmt.Errorf("effort (%s): %w", s.Effort.Origin, err)
	}
	if err := s.distinctLabels(); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// Labels are the triage labels the settings have the tracker read and write.
func (s Settings) Labels() tracker.Labels {
	return tracker.Labels{Agent: s.AgentLabel.Value, Human: s.HumanLabel.Value, Spec: s.SpecLabel.Value}
}

// The repo file keys the triage labels are set under.
const (
	agentKey = "labels.agent"
	humanKey = "labels.human"
	specKey  = "labels.spec"
)

// namedLabel is a resolved triage label with the repo file key that sets it.
type namedLabel struct {
	key   string
	label config.Value
}

// namedLabels returns the resolved triage labels, each with its repo file key,
// in the order the repo file documents them.
func (s Settings) namedLabels() []namedLabel {
	return []namedLabel{{agentKey, s.AgentLabel}, {humanKey, s.HumanLabel}, {specKey, s.SpecLabel}}
}

// distinctLabels refuses a triage label named for one of the loop's own marks,
// and two triage labels resolved to the same name, naming the one set in the
// repo file first: a task carrying both the agent and the human label goes to
// the human, so a shared name would quietly misroute work.
func (s Settings) distinctLabels() error {
	labels := s.namedLabels()
	for _, l := range labels {
		if tracker.IsMark(l.label.Value) {
			return fmt.Errorf("%s (%s) is %q, the label smith marks its own work with: name another", l.key, l.label.Origin, l.label.Value)
		}
	}
	for j, later := range labels {
		for _, earlier := range labels[:j] {
			if earlier.label.Value != later.label.Value {
				continue
			}
			set, other := later, earlier
			if later.label.Origin != config.FromRepoFile {
				set, other = earlier, later
			}
			return fmt.Errorf("%s (%s) is %q, the name %s (%s) has: each label needs a name of its own",
				set.key, set.label.Origin, set.label.Value, other.key, other.label.Origin)
		}
	}
	return nil
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
