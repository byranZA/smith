// Package smithtool carries a smith tool call from a loop's agent to the
// running loop: a registry of named tools, a server the loop serves over a
// Unix socket for each agent launch, and a client the agent reaches it with.
// Its wire protocol is private and generic: a tool name and args in, then
// streamed output and a final status out.
package smithtool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// Tool is one named action the loop performs for its agent.
type Tool struct {
	// Name is what the agent calls the tool by.
	Name string
	// Summary is the tool's one line in a listing.
	Summary string
	// Run performs the tool with args, streaming its output to out. An error
	// made by Refuse reports the tool as refused before anything changed; any
	// other is a tool that ran and failed.
	Run func(ctx context.Context, args []string, out io.Writer) error
}

// Info is a tool as a listing shows it.
type Info struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// Registry is the closed set of tools a run serves.
type Registry struct {
	tools []Tool
	// withheld maps the name of a tool this run does not serve to why.
	withheld map[string]string
}

// Withhold returns the registry with name noted as a tool this run does not
// serve, so a call to it is refused with reason rather than as unknown.
func (r Registry) Withhold(name, reason string) Registry {
	withheld := maps.Clone(r.withheld)
	if withheld == nil {
		withheld = map[string]string{}
	}
	withheld[name] = reason
	r.withheld = withheld
	return r
}

// NewRegistry returns a registry of tools, listed in the order given.
func NewRegistry(tools ...Tool) Registry { return Registry{tools: slices.Clone(tools)} }

// List returns every tool's name and summary, in registration order.
func (r Registry) List() []Info {
	infos := make([]Info, len(r.tools))
	for i, t := range r.tools {
		infos[i] = Info{Name: t.Name, Summary: t.Summary}
	}
	return infos
}

// find returns the tool called name, and false when there is none.
func (r Registry) find(name string) (Tool, bool) {
	i := slices.IndexFunc(r.tools, func(t Tool) bool { return t.Name == name })
	if i < 0 {
		return Tool{}, false
	}
	return r.tools[i], true
}

// unknown is the refusal of a tool the registry does not hold, naming the ones it does.
func (r Registry) unknown(name string) error {
	if reason, ok := r.withheld[name]; ok {
		return &RefusedError{Reason: reason}
	}
	names := make([]string, len(r.tools))
	for i, t := range r.tools {
		names[i] = t.Name
	}
	available := "none"
	if len(names) > 0 {
		available = strings.Join(names, ", ")
	}
	return &RefusedError{Reason: fmt.Sprintf("unknown smith tool %q; available in this run: %s", name, available)}
}

// RefusedError is a call refused before anything changed: no run, a dead
// socket, or an unknown or unavailable tool.
type RefusedError struct {
	Reason string
}

// Error returns the reason.
func (e *RefusedError) Error() string { return e.Reason }

// Refuse returns an error that reports a tool as refused, for a tool's Run to return.
func Refuse(reason string) error { return &RefusedError{Reason: reason} }

// FailedError is a tool that ran and failed.
type FailedError struct {
	Reason string
}

// Error returns the reason.
func (e *FailedError) Error() string { return e.Reason }

var errNoSocket = errors.New("smith is not running a loop for this shell: SMITH_TOOLS is not set")
