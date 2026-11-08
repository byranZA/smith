package loop

import (
	"fmt"
	"strconv"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/preset"
	"github.com/byranZA/smith/internal/repofile"
	"github.com/byranZA/smith/internal/tracker"
)

// Flags are the loop settings a run's flags set, over the repo file. A field
// left empty or nil is unset.
type Flags struct {
	// Preset are the flags that choose the preset the run works on.
	Preset preset.Flags
	// Push turns pushing after each closed task on or off.
	Push *bool
	// NoReview skips the review for this run.
	NoReview bool
	// MaxIterations is the cap on agent runs the flag sets, nil when unset.
	MaxIterations *int
	// MaxAttempts is the attempts the flag sets, nil when unset.
	MaxAttempts *int
	// Stack says the run brings the stack its repo file declares up and
	// down, as a run on a box does.
	Stack bool
}

// Settings are the resolved loop settings, each carrying where it came from.
type Settings struct {
	// Preset is the preset the work role runs on, with where it came from.
	Preset preset.Resolved
	// Push is "on" or "off": whether the loop pushes the branch after each
	// task the agent closes.
	Push config.Value
	// Cap is the cap on agent runs, "none" or a number, with where it came from.
	Cap config.Value
	// MaxIterations is the cap as a number of agent runs, zero for none.
	MaxIterations int
	// Attempts is the attempts at each task and the review, a number, with
	// where it came from.
	Attempts config.Value
	// MaxAttempts is Attempts as a number.
	MaxAttempts int
	// AgentLabel, HumanLabel and SpecLabel name the tracker's triage labels.
	// They are set in the repo file alone, never by a flag.
	AgentLabel config.Value
	HumanLabel config.Value
	SpecLabel  config.Value
	// Stack is "on" with where it came from when the run brings the repo's
	// stack up and down, and zero when it runs none: the repo file declares
	// none, or the run is not set to run it.
	Stack config.Value
	// StackDeclared is whether the repo file declares a stack, on or not.
	StackDeclared bool
	// StackRuns is whether the run executes stacks at all, on a box or with
	// --stack, whether or not the invocation checkout declares one: a work
	// branch or a task can add a stack the run must then bring up.
	StackRuns bool
}

// ResolveSettings resolves the settings from flags over the repo file over the
// built-in defaults, naming each one's origin, and returns the preset choice
// that picks each task's preset. It refuses what preset.Choose refuses and
// triage labels that clash with each other or with the loop's own marks.
func ResolveSettings(flags Flags, file repofile.File) (Settings, preset.Choice, error) {
	choice, err := preset.Choose(file, flags.Preset)
	if err != nil {
		return Settings{}, preset.Choice{}, fmt.Errorf("choose the preset: %w", err)
	}
	defaults := tracker.DefaultLabels()
	maxIterations, capOrigin := resolveCap(flags.MaxIterations, file.Limits.AgentRuns)
	maxAttempts, attemptsOrigin := resolveAttempts(flags.MaxAttempts, file.Limits.TaskAttempts)
	s := Settings{
		Preset:        choice.Work(),
		Push:          resolveSwitch(flags.Push, file.Push, true),
		Cap:           capReport(maxIterations, capOrigin),
		Attempts:      config.Value{Value: strconv.Itoa(maxAttempts), Origin: attemptsOrigin},
		MaxIterations: maxIterations,
		MaxAttempts:   maxAttempts,
		AgentLabel:    resolveSetting("", file.Labels.Agent, defaults.Agent),
		HumanLabel:    resolveSetting("", file.Labels.Human, defaults.Human),
		SpecLabel:     resolveSetting("", file.Labels.Spec, defaults.Spec),
	}
	if err := s.distinctLabels(); err != nil {
		return Settings{}, preset.Choice{}, err
	}
	s.StackDeclared = file.Stack.Declared()
	s.StackRuns = flags.Stack
	if flags.Stack && s.StackDeclared {
		s.Stack = config.Value{Value: switchValue(true), Origin: config.FromRepoFile}
	}
	return s, choice, nil
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

// Stacks reports whether the settings have the run bring the repo's stack up
// and down.
func (s Settings) Stacks() bool { return s.Stack.Value == switchValue(true) }

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

// The origins of the two limits a flag sets, worded as the flag itself so the
// settings report names which flag set them.
const (
	originMaxIterations config.Origin = "--max-iterations"
	originMaxAttempts   config.Origin = "--max-attempts"
)

// resolveCap resolves the cap on agent runs: the flag, zero of it meaning
// none, then the repo file's, then no cap from smith's default. It returns the
// number of runs, zero for none, and where it came from.
func resolveCap(flag *int, file int) (int, config.Origin) {
	switch {
	case flag != nil:
		return *flag, originMaxIterations
	case file > 0:
		return file, config.FromRepoFile
	default:
		return DefaultLimits().MaxIterations, config.FromDefault
	}
}

// capReport formats the cap on agent runs for the settings report. A cap the
// flag sets reads as a count of agent runs; the repo file's reads as a bare
// number.
func capReport(n int, origin config.Origin) config.Value {
	switch {
	case n == 0:
		return config.Value{Value: "none", Origin: origin}
	case origin == originMaxIterations:
		return config.Value{Value: strconv.Itoa(n) + " agent runs", Origin: origin}
	default:
		return config.Value{Value: strconv.Itoa(n), Origin: origin}
	}
}

// resolveAttempts resolves the attempts at each task: the flag, then the repo
// file's, then smith's default. It returns the number and where it came from.
func resolveAttempts(flag *int, file int) (int, config.Origin) {
	switch {
	case flag != nil:
		return *flag, originMaxAttempts
	case file > 0:
		return file, config.FromRepoFile
	default:
		return DefaultLimits().MaxAttempts, config.FromDefault
	}
}
