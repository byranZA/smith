package loop

import (
	"fmt"
	"slices"

	"github.com/byranZA/smith/internal/preset"
	"github.com/byranZA/smith/internal/tracker"
)

// Agents names, once each and in task order, the agents a run of execution
// may hand a task to under presets: interactive, the next available task's
// alone, and otherwise every open task's that is meant for an agent. It
// refuses a task whose preset presets cannot resolve.
func Agents(presets preset.Choice, execution tracker.Execution, interactive bool) ([]string, error) {
	var names []string
	for _, task := range handable(execution, interactive) {
		resolved, err := presets.Task(task)
		if err != nil {
			return nil, fmt.Errorf("choose a preset: %w", err)
		}
		if !slices.Contains(names, resolved.Agent.Value) {
			names = append(names, resolved.Agent.Value)
		}
	}
	return names, nil
}

// handable is the tasks of execution a run may hand to an agent: interactive,
// the next available task alone, and otherwise every open task meant for an
// agent, blocked or not.
func handable(execution tracker.Execution, interactive bool) []tracker.Task {
	if interactive {
		next, ok := Survey(execution).Next()
		if !ok {
			return nil
		}
		return []tracker.Task{next}
	}
	var tasks []tracker.Task
	for _, task := range execution.Tasks {
		if task.Open && task.For == tracker.Agent {
			tasks = append(tasks, task)
		}
	}
	return tasks
}
