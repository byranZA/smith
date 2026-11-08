package cli

import (
	"github.com/byranZA/smith/internal/status"
)

// statusDocument is the JSON document `smith machine status --json` prints.
type statusDocument struct {
	SchemaVersion int             `json:"schema_version"`
	Box           string          `json:"box"`
	Verdict       string          `json:"verdict"`
	AccessMode    string          `json:"access_mode,omitempty"`
	Findings      []statusFinding `json:"findings"`
	// MissingPhases is set only for a partial box.
	MissingPhases []string `json:"missing_phases,omitempty"`
	// Error is why the connection failed; set only for an unreachable box.
	Error string `json:"error,omitempty"`
	// Note is the marker's schema-skew note, when there is one.
	Note string `json:"note,omitempty"`
	// MemoryKiB and SwapKiB are absent when the figure could not be read.
	MemoryKiB *uint64 `json:"memory_kib,omitempty"`
	SwapKiB   *uint64 `json:"swap_kib,omitempty"`
	// Docker holds the published ports; absent for an unreachable box.
	Docker *statusDocker `json:"docker,omitempty"`
}

// statusFinding is one reconciled fact in a statusDocument.
type statusFinding struct {
	Phase    string `json:"phase"`
	Fact     string `json:"fact"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Status   string `json:"status"`
}

// statusDocker is what a box's containers publish.
type statusDocker struct {
	Ports      []statusPort `json:"ports"`
	Unreadable bool         `json:"unreadable"`
}

// statusPort is one published container binding and whether it is beyond loopback.
type statusPort struct {
	Binding string `json:"binding"`
	Exposed bool   `json:"exposed"`
}

// newStatusDocument builds the document for box from its report.
func newStatusDocument(box string, r status.Report) statusDocument {
	doc := statusDocument{
		SchemaVersion: 1,
		Box:           box,
		Verdict:       r.Verdict.Token(),
		AccessMode:    r.AccessMode,
		Findings:      []statusFinding{},
		MissingPhases: r.MissingPhases,
		Error:         r.Reason,
		Note:          r.Skew.Note(),
	}
	for _, g := range r.Groups {
		for _, f := range g.Findings {
			doc.Findings = append(doc.Findings, statusFinding{
				Phase: g.Phase, Fact: f.Fact, Expected: f.Expected, Actual: f.Actual, Status: f.Status.Token(),
			})
		}
	}
	if r.Verdict == status.VerdictUnreachable {
		return doc
	}
	if kiB, ok := r.Memory.KiB(); ok {
		doc.MemoryKiB = &kiB
	}
	if kiB, ok := r.Swap.KiB(); ok {
		doc.SwapKiB = &kiB
	}
	docker := &statusDocker{Ports: []statusPort{}, Unreadable: r.Docker.Unreadable}
	for _, b := range r.Docker.Exposed {
		docker.Ports = append(docker.Ports, statusPort{Binding: b, Exposed: true})
	}
	for _, b := range r.Docker.Loopback {
		docker.Ports = append(docker.Ports, statusPort{Binding: b})
	}
	doc.Docker = docker
	return doc
}
