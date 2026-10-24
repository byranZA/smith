// Package preset chooses the preset each piece of a loop run's work runs on,
// from the repo file's presets and roles and the run's flags. It is the one
// place that knows ADR-0022's precedence, and it has no side effects.
package preset

import (
	"fmt"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/repofile"
)

// DefaultName names the built-in default preset: the default agent, with the
// model and effort left to the agent.
const DefaultName = "default"

// Flags are the run flags that choose a preset. A field left empty is unset.
type Flags struct {
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

// Choice is the preset choice for one run, built by Choose.
type Choice struct {
	work Resolved
}

// Choose builds the run's preset choice from the repo file and the run's
// flags, refusing an agent flag smith has no adapter for, an effort flag off
// smith's scale, and a work role naming a preset the repo file lacks.
func Choose(file repofile.File, flags Flags) (Choice, error) {
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
	case flags.Agent != "":
		work = Resolved{Origin: config.FromFlag, Agent: flagged(flags.Agent), Model: flagged(flags.Model), Effort: flagged("")}
	case flags.Model != "":
		work.Model = flagged(flags.Model)
	}
	if flags.Effort != "" {
		work.Effort = flagged(flags.Effort)
	}
	return Choice{work: work}, nil
}

// Work returns the preset the work role runs on in this run.
func (c Choice) Work() Resolved { return c.work }

// role resolves the preset the repo file points name at, or the built-in
// default when it points it at none.
func role(file repofile.File, name string) (Resolved, error) {
	preset, ok := file.Roles[name]
	if !ok {
		return builtin(), nil
	}
	p, ok := file.Presets[preset]
	if !ok {
		return Resolved{}, &UndefinedError{Name: preset, By: "role " + name}
	}
	declared := func(value string) config.Value { return config.Value{Value: value, Origin: config.FromRepoFile} }
	return Resolved{Name: preset, Origin: config.FromRepoFile, Agent: declared(p.Agent), Model: declared(p.Model), Effort: declared(p.Effort)}, nil
}

// builtin is the built-in default preset: the default agent, with its model
// and effort left to the agent.
func builtin() Resolved {
	fallback := func(value string) config.Value { return config.Value{Value: value, Origin: config.FromDefault} }
	return Resolved{Name: DefaultName, Origin: config.FromDefault, Agent: fallback(agent.Default), Model: fallback(""), Effort: fallback("")}
}

// flagged is value as a run flag set it.
func flagged(value string) config.Value {
	return config.Value{Value: value, Origin: config.FromFlag}
}
