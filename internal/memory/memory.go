// Package memory holds the memory advisory: the recommended minimum RAM for a
// box to run an agent, and the line smith prints about a box's memory.
package memory

import "fmt"

// advisoryCutoffKiB is where the warning fires: below 1.75 GB, not the 2 GB
// operators are told, because the kernel keeps part of RAM for itself and a
// nominal 2 GB box reports about 1.9 GB.
const advisoryCutoffKiB = 1835008

// warning is the line printed for a box under the memory advisory.
const warning = "warning: under the recommended 2 GB of memory to run an agent"

// Total is a box's total RAM as its kernel reports it. The zero Total is
// unknown memory.
type Total struct {
	kiB uint64
}

// FromKiB returns the Total for kiB kibibytes of reported RAM, as /proc/meminfo's
// MemTotal gives it. Zero kiB is unknown memory.
func FromKiB(kiB uint64) Total {
	return Total{kiB: kiB}
}

// Known reports whether the box's memory could be read.
func (t Total) Known() bool {
	return t.kiB > 0
}

// String renders the memory as operators read it: whole MB under 1 GB, GB to
// one decimal place above, and "unknown" when it could not be read.
func (t Total) String() string {
	switch {
	case !t.Known():
		return "unknown"
	case t.kiB < 1<<20:
		return fmt.Sprintf("%d MB", t.kiB>>10)
	default:
		return fmt.Sprintf("%.1f GB", float64(t.kiB)/(1<<20))
	}
}

// Advisory returns the lines to print about the box's memory, without a
// trailing newline: the memory figure, plus a warning when the box is known to
// be under the memory advisory.
func (t Total) Advisory() string {
	line := "memory: " + t.String()
	if t.Known() && t.kiB < advisoryCutoffKiB {
		return line + "\n" + warning
	}
	return line
}
