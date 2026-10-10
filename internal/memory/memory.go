// Package memory holds the memory advisory: the recommended minimum RAM for a
// box to run an agent, and the lines smith prints about a box's memory and swap.
package memory

import (
	"fmt"
	"strconv"
	"strings"
)

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

// Parse reads a MemTotal figure in kB as the shipped script prints it, yielding
// unknown memory for a figure that is empty or not a number.
func Parse(v string) Total {
	kiB, ok := parseKiB(v)
	if !ok {
		return Total{}
	}
	return FromKiB(kiB)
}

// Known reports whether the box's memory could be read.
func (t Total) Known() bool {
	return t.kiB > 0
}

// String renders the memory as operators read it: whole MB under 1 GB, GB to
// one decimal place above, and "unknown" when it could not be read.
func (t Total) String() string {
	if !t.Known() {
		return "unknown"
	}
	return size(t.kiB)
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

// Swap is a box's total swap as its kernel reports it. Unlike Total, zero swap
// is a known figure: the box has none. The zero Swap is unknown swap.
type Swap struct {
	kiB   uint64
	known bool
}

// SwapFromKiB returns the Swap for kiB kibibytes of reported swap, as
// /proc/meminfo's SwapTotal gives it. Zero kiB is a box without swap.
func SwapFromKiB(kiB uint64) Swap {
	return Swap{kiB: kiB, known: true}
}

// ParseSwap reads a SwapTotal figure in kB as the shipped script prints it,
// yielding unknown swap for a figure that is empty or not a number.
func ParseSwap(v string) Swap {
	kiB, ok := parseKiB(v)
	if !ok {
		return Swap{}
	}
	return SwapFromKiB(kiB)
}

// String renders the swap as Total renders memory, with "none" for a box without
// swap and "unknown" when it could not be read.
func (s Swap) String() string {
	switch {
	case !s.known:
		return "unknown"
	case s.kiB == 0:
		return "none"
	default:
		return size(s.kiB)
	}
}

// size renders kiB as operators read it: whole MB under 1 GB, GB to one decimal
// place above.
func size(kiB uint64) string {
	if kiB < 1<<20 {
		return fmt.Sprintf("%d MB", kiB>>10)
	}
	return fmt.Sprintf("%.1f GB", float64(kiB)/(1<<20))
}

// parseKiB reads a whole number of kB, reporting whether v held one.
func parseKiB(v string) (uint64, bool) {
	kiB, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
	return kiB, err == nil
}
