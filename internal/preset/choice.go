// Package preset chooses the preset each piece of a loop run's work runs on,
// from the repo file's presets and roles and the run's flags. It is the one
// place that knows ADR-0022's precedence, and it has no side effects.
package preset

import (
	"errors"
	"fmt"
	"strings"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/repofile"
	"github.com/byranZA/smith/internal/tracker"
)

// DefaultName names the built-in default preset: the default agent, with the
// model and effort left to the agent.
const DefaultName = "default"

// Flags are the run flags that choose a preset. A field left empty is unset.
type Flags struct {
	// Preset pins the run to one of the repo file's presets by name.
	Preset string
	// Agent builds a one-off preset on this agent, inheriting nothing.
	Agent string
	// Model replaces the model of the preset the run would otherwise use, or
	// sets the one-off preset's model alongside Agent.
	Model string
	// Effort replaces the effort of whatever preset the run resolves.
	Effort string
}

// Resolved is a preset as a run resolved it: its name, empty for a one-off
// preset built from the flags, where it came from, and each setting with
// where that came from. An empty model or effort leaves it to the agent.
type Resolved struct {
	// Name is the preset's name, empty for a one-off preset.
	Name string
	// Origin is where the preset came from.
	Origin config.Origin
	// Agent names the agent adapter the work is handed to.
	Agent config.Value
	// Model is the model the agent runs, passed to it verbatim.
	Model config.Value
	// Effort is how hard the agent thinks, on smith's own scale.
	Effort config.Value
}

// Options are the model and effort the preset asks of its agent.
func (r Resolved) Options() agent.Options {
	return agent.Options{Model: r.Model.Value, Effort: agent.Effort(r.Effort.Value)}
}

// UndefinedError reports a preset named by something, such as a role, that
// the repo file does not define.
type UndefinedError struct {
	// Name is the preset named.
	Name string
	// By is what named it, such as "role work".
	By string
}

// Error implements error.
func (e *UndefinedError) Error() string {
	return fmt.Sprintf("%s names preset %q, which the repo file does not define", e.By, e.Name)
}

// LabelsError reports a task carrying more than one preset: label, since a
// task runs on one preset.
type LabelsError struct {
	// Task is the task's number.
	Task int
	// Presets names the preset each of its labels chooses.
	Presets []string
}

// Error implements error.
func (e *LabelsError) Error() string {
	labels := make([]string, len(e.Presets))
	for i, name := range e.Presets {
		labels[i] = label(name)
	}
	last := len(labels) - 1
	return fmt.Sprintf("#%d carries the labels %s and %s: a task runs on one preset", e.Task, strings.Join(labels[:last], ", "), labels[last])
}

// ErrPresetWithAgent refuses the preset flag alongside the agent or model
// flag, since a preset already names its agent and model.
var ErrPresetWithAgent = errors.New("--preset cannot be combined with --agent or --model: a preset already names its agent and model")

// Choice is the preset choice for one run, built by Choose.
type Choice struct {
	file repofile.File
	work Resolved
	// pinned reports whether a run flag chose the preset of every task, over
	// its labels.
	pinned bool
	effort string
}

// Choose builds the run's preset choice from the repo file and the run's
// flags, refusing an agent flag smith has no adapter for, an effort flag off
// smith's scale, a preset flag alongside the agent or model flag, and a work
// role or preset flag naming a preset the repo file lacks.
func Choose(file repofile.File, flags Flags) (Choice, error) {
	if flags.Preset != "" && (flags.Agent != "" || flags.Model != "") {
		return Choice{}, ErrPresetWithAgent
	}
	if flags.Agent != "" {
		if _, err := agent.Lookup(flags.Agent); err != nil {
			return Choice{}, fmt.Errorf("--agent: %w", err)
		}
	}
	if _, err := agent.ParseEffort(flags.Effort); err != nil {
		return Choice{}, fmt.Errorf("--effort: %w", err)
	}
	work, err := role(file, repofile.WorkRole)
	if err != nil {
		return Choice{}, err
	}
	switch {
	case flags.Preset != "":
		work, err = named(file, flags.Preset, "--preset", config.FromFlag)
		if err != nil {
			return Choice{}, err
		}
	case flags.Agent != "":
		work = Resolved{Origin: config.FromFlag, Agent: flagged(flags.Agent), Model: flagged(flags.Model), Effort: flagged("")}
	case flags.Model != "":
		work.Model = flagged(flags.Model)
	}
	if flags.Effort != "" {
		work.Effort = flagged(flags.Effort)
	}
	pinned := flags.Preset != "" || flags.Agent != "" || flags.Model != ""
	return Choice{file: file, work: work, pinned: pinned, effort: flags.Effort}, nil
}

// Work returns the preset the work role runs on in this run.
func (c Choice) Work() Resolved { return c.work }

// Task returns the preset task runs on in this run: the work preset when a
// run flag pins it or task carries no preset: label, and otherwise the
// preset its label names, under the effort flag when one is set. It refuses
// a task carrying two preset: labels, or one naming a preset the repo file
// does not define, unless the run is pinned.
func (c Choice) Task(task tracker.Task) (Resolved, error) {
	if c.pinned || len(task.Presets) == 0 {
		return c.work, nil
	}
	if len(task.Presets) > 1 {
		return Resolved{}, &LabelsError{Task: task.Number, Presets: task.Presets}
	}
	name := task.Presets[0]
	r, err := named(c.file, name, fmt.Sprintf("#%d's label %s", task.Number, label(name)), config.FromTaskLabel)
	if err != nil {
		return Resolved{}, err
	}
	if c.effort != "" {
		r.Effort = flagged(c.effort)
	}
	return r, nil
}

// Check refuses execution when one of its open tasks has a preset Task
// refuses, so a run can stop before any agent runs. Closed tasks are not
// checked.
func (c Choice) Check(execution tracker.Execution) error {
	for _, task := range execution.Tasks {
		if !task.Open {
			continue
		}
		if _, err := c.Task(task); err != nil {
			return err
		}
	}
	return nil
}

// role resolves the preset the repo file points name at, or the built-in
// default when it points it at none.
func role(file repofile.File, name string) (Resolved, error) {
	preset, ok := file.Roles[name]
	if !ok {
		return builtin(), nil
	}
	return named(file, preset, "role "+name, config.FromRepoFile)
}

// named resolves the repo file's preset name, as by chose it, with each
// setting carrying origin, or refuses a name the repo file does not define.
func named(file repofile.File, name, by string, origin config.Origin) (Resolved, error) {
	p, ok := file.Presets[name]
	if !ok {
		return Resolved{}, &UndefinedError{Name: name, By: by}
	}
	chosen := func(value string) config.Value { return config.Value{Value: value, Origin: origin} }
	return Resolved{Name: name, Origin: origin, Agent: chosen(p.Agent), Model: chosen(p.Model), Effort: chosen(p.Effort)}, nil
}

// builtin is the built-in default preset: the default agent, with its model
// and effort left to the agent.
func builtin() Resolved {
	fallback := func(value string) config.Value { return config.Value{Value: value, Origin: config.FromDefault} }
	return Resolved{Name: DefaultName, Origin: config.FromDefault, Agent: fallback(agent.Default), Model: fallback(""), Effort: fallback("")}
}

// label is the preset: label that chooses the preset called name.
func label(name string) string { return tracker.PresetPrefix + name }

// flagged is value as a run flag set it.
func flagged(value string) config.Value {
	return config.Value{Value: value, Origin: config.FromFlag}
}
