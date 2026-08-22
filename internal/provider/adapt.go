package provider

import "github.com/byranZA/smith/internal/blueprint"

// Adapt returns the adapter a blueprint's provider block describes. The two
// shapes are kept apart deliberately: the blueprint package owns what an
// operator may write in a document, and this package owns what smith runs, so
// the document format can change without reaching into the execution path.
func Adapt(p blueprint.Provider) Adapter {
	return Adapter{
		Create:   p.Create,
		List:     p.List,
		Destroy:  p.Destroy,
		Requires: p.Requires,
		SSHKey:   p.SSHKey,
		Marker:   Marker{Arg: p.Marker.Arg, Read: p.Marker.Read},
		Extract:  Extract{ID: p.Extract.ID, IP: p.Extract.IP},
	}
}
