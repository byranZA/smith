// Package jsonpath reads one value out of a decoded JSON document by path.
//
// It exists so the extractor grammar an operator writes in a provider adapter
// has one implementation and one meaning. The grammar is the part of the
// adapter most likely to grow, and the part where a silently wrong answer is
// most damaging: an extractor that quietly yields a private address instead of
// a public one leaves smith holding a box it cannot reach, for a reason that
// looks like anything but a path bug. So a path that matches nothing is an
// error naming the path, never a zero value.
//
// The grammar is dotted descent: "id" reads a key, "public_net.ipv4.ip" walks
// into nested objects. A JSON null is a value, not a miss — an unset
// collection decodes as null rather than as an empty list, and reporting that
// as a missing field would refuse a perfectly good response.
//
// Documents are expected to have been decoded with json.Decoder.UseNumber, so
// a provider's integer id keeps its exact value instead of round-tripping
// through float64 and rendering in exponent notation.
package jsonpath

import (
	"fmt"
	"strings"
)

// Lookup returns the value doc holds at path, or an error naming the path when
// the path matches nothing. The document is the result of decoding a
// provider's JSON into any.
func Lookup(doc any, path string) (any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("empty path: want a key such as %q", "id")
	}
	current := doc
	for _, key := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("path %q matched nothing: nothing to read %q from", path, key)
		}
		value, ok := object[key]
		if !ok {
			return nil, fmt.Errorf("path %q matched nothing: no key %q", path, key)
		}
		current = value
	}
	return current, nil
}
