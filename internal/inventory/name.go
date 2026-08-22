package inventory

// Origin is where the name a box was registered under came from. The operator
// is told when smith derived it rather than taking one they chose, because a
// derived name is one they may want to replace — and a name smith invented
// without saying so would be a name they never agreed to.
type Origin int

const (
	// OriginNone means nothing named the box: smith knew no name at all.
	OriginNone Origin = iota
	// OriginFlag means the operator named the box with --name.
	OriginFlag
	// OriginMarker means the name came off the box's own marker: what the box
	// records it is already called.
	OriginMarker
	// OriginBlueprint means the name came from the blueprint the box was built
	// from.
	OriginBlueprint
	// OriginHost means the name came from the host part of the box's target,
	// the last resort when nothing else named it.
	OriginHost
)

// Naming is everything smith knows that could name a box, one field per source
// of a name. Empty means that source named none.
type Naming struct {
	// Flag is the name the operator passed with --name.
	Flag string
	// Marker is the name the box's own marker records.
	Marker string
	// Blueprint is the name of the blueprint the box was built from.
	Blueprint string
	// Host is the host part of the target smith reaches the box over.
	Host string
}

// Name resolves what a box is called, most specific first: the operator's
// --name, then the name already on the box's marker, then the blueprint it was
// built from, then its host. It returns the name and where it came from.
//
// The marker's name sitting above the blueprint and the host is what makes
// re-setup idempotent however the operator addressed the box: without it,
// setting up a box already registered as "dev" by typing its address would
// register a second entry pointing at the same machine.
//
// Nothing here invents a name. A name that collides is refused by Register,
// never resolved to a "dev-2" the operator did not choose and would not think
// to type.
func Name(n Naming) (string, Origin) {
	for _, candidate := range []struct {
		name   string
		origin Origin
	}{
		{n.Flag, OriginFlag},
		{n.Marker, OriginMarker},
		{n.Blueprint, OriginBlueprint},
		{n.Host, OriginHost},
	} {
		if candidate.name != "" {
			return candidate.name, candidate.origin
		}
	}
	return "", OriginNone
}
