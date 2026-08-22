// Package extractpath reads one value out of a decoded JSON document by the
// extractor path an operator writes in a provider adapter.
//
// It is package provider's own grammar rather than shared infrastructure —
// nothing outside an adapter reads a path — so it lives under provider.
//
// It exists so that grammar has one implementation and one meaning. It is the
// part of the adapter most likely to grow, and the part where a silently wrong answer is
// most damaging: an extractor that quietly yields a private address instead of
// a public one leaves smith holding a box it cannot reach, for a reason that
// looks like anything but a path bug. So a path that matches nothing is an
// error naming the path, never a zero value, and a filter smith does not
// understand is refused rather than quietly matching nothing.
//
// The grammar is exactly what two real provider CLIs proved necessary:
//
//	id                                  a key
//	public_net.ipv4.ip                  dotted descent into nested objects
//	[*]                                 every element of an array
//	networks.v4[type=public].ip_address elements whose field equals a value
//
// The predicate filter is load-bearing rather than a convenience: a provider's
// address list has no stable ordering — in one DigitalOcean account the public
// address came second on one droplet and first on another — so selecting by
// position would hand smith a private address roughly half the time.
//
// A JSON null is a value, not a miss. An unset collection decodes as null
// rather than as an empty list, and reporting that as a missing field would
// refuse a perfectly good response; so a null carries through descent and
// through filters as an absent value.
//
// Documents are expected to have been decoded with json.Decoder.UseNumber, so
// a provider's integer id keeps its exact value instead of round-tripping
// through float64 and rendering in exponent notation.
package extractpath

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Lookup returns the first value doc holds at path, or an error naming the
// path when the path matches nothing or is not a path smith understands. The
// document is the result of decoding a provider's JSON into any.
//
// A path may match several values — that is what "[*]" is for — and the first
// in document order is the answer. Extraction wants one value: the record
// inside an envelope, the box's id, its address.
func Lookup(doc any, path string) (any, error) {
	matches, err := LookupAll(doc, path)
	if err != nil {
		return nil, err
	}
	return matches[0], nil
}

// LookupAll returns every value doc holds at path, in document order, or an
// error naming the path when the path matches nothing or is not a path smith
// understands.
//
// It is the answer for a path over a collection rather than over one value: a
// provider's list response holds every box in the account, and "[*]" over it
// has to reach all of them, not only the first.
func LookupAll(doc any, path string) ([]any, error) {
	segments, err := parse(path)
	if err != nil {
		return nil, err
	}
	matches := []any{doc}
	for _, segment := range segments {
		next := segment.apply(matches)
		if len(next) == 0 {
			return nil, fmt.Errorf("path %q matched nothing: %q selected no value", path, segment.src)
		}
		matches = next
	}
	return matches, nil
}

// segment is one dot-separated step of a path: a key to descend into, then the
// filters applied to whatever that key held.
type segment struct {
	src     string
	key     string
	filters []filter
}

// filter selects elements of an array. A filter with no field is "[*]" and
// selects every element.
type filter struct {
	field string
	value string
}

// apply returns every value this segment reaches from the values matched so
// far.
func (s segment) apply(matches []any) []any {
	var out []any
	for _, match := range matches {
		value, ok := s.descend(match)
		if !ok {
			continue
		}
		reached := []any{value}
		for _, f := range s.filters {
			reached = f.apply(reached)
		}
		out = append(out, reached...)
	}
	return out
}

// descend reads the segment's key from match, reporting whether it was there
// at all. A null holds no keys but is an absent value rather than a wrong one,
// so it carries through.
func (s segment) descend(match any) (any, bool) {
	if s.key == "" {
		return match, true
	}
	if match == nil {
		return nil, true
	}
	object, ok := match.(map[string]any)
	if !ok {
		return nil, false
	}
	value, ok := object[s.key]
	return value, ok
}

// apply returns the elements this filter selects out of every array among
// vals. A null carries through, because a provider reporting an unset
// collection as null has answered the question rather than failed it.
func (f filter) apply(vals []any) []any {
	var out []any
	for _, value := range vals {
		if value == nil {
			out = append(out, nil)
			continue
		}
		array, ok := value.([]any)
		if !ok {
			continue
		}
		for _, element := range array {
			if f.selects(element) {
				out = append(out, element)
			}
		}
	}
	return out
}

// selects reports whether an array element satisfies the filter.
func (f filter) selects(element any) bool {
	if f.field == "" {
		return true
	}
	object, ok := element.(map[string]any)
	if !ok {
		return false
	}
	value, ok := object[f.field]
	if !ok {
		return false
	}
	return Text(value) == f.value
}

// Text renders a looked-up JSON scalar as the string smith holds for it — the
// same rendering a predicate compares against, so a box whose id matches
// "id=593069736" is stored under those same digits. Numbers keep their literal
// form rather than a float's rendering of them, and anything that is not a
// scalar — a null, an object, an array — is the empty string, since a provider
// that has not assigned an address yet reports null, which is an absent value
// rather than a bad one.
func Text(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return fmt.Sprint(v)
	default:
		return ""
	}
}

// parse reads a path into the segments Lookup walks. Every error names the
// whole path, because that is what the operator wrote in their adapter.
func parse(path string) ([]segment, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("empty path: want a key such as %q", "id")
	}
	var segments []segment
	for _, src := range split(path) {
		s, err := parseSegment(src)
		if err != nil {
			return nil, fmt.Errorf("path %q: %w", path, err)
		}
		segments = append(segments, s)
	}
	return segments, nil
}

// split breaks a path on the dots between segments, leaving the dots inside a
// filter alone.
func split(path string) []string {
	var (
		segments []string
		start    int
		depth    int
	)
	for i, r := range path {
		switch r {
		case '[':
			depth++
		case ']':
			depth--
		case '.':
			if depth == 0 {
				segments = append(segments, path[start:i])
				start = i + 1
			}
		}
	}
	return append(segments, path[start:])
}

// parseSegment reads a key and its filters out of one segment's source text.
func parseSegment(src string) (segment, error) {
	s := segment{src: src}
	rest := src
	if open := strings.IndexByte(rest, '['); open >= 0 {
		s.key, rest = rest[:open], rest[open:]
	} else {
		s.key, rest = rest, ""
	}
	for rest != "" {
		end := strings.IndexByte(rest, ']')
		if rest[0] != '[' || end < 0 {
			return segment{}, fmt.Errorf("unclosed filter %q: want %q or %q", rest, "[*]", "[field=value]")
		}
		f, err := parseFilter(rest[1:end])
		if err != nil {
			return segment{}, err
		}
		s.filters = append(s.filters, f)
		rest = rest[end+1:]
	}
	if s.key == "" && len(s.filters) == 0 {
		return segment{}, fmt.Errorf("empty segment: want a key such as %q between the dots", "id")
	}
	return s, nil
}

// parseFilter reads the text between one filter's brackets. Anything else —
// a positional index, say — is refused, because the ordering a position would
// select by is exactly what providers do not guarantee.
func parseFilter(body string) (filter, error) {
	if body == "*" {
		return filter{}, nil
	}
	field, value, ok := strings.Cut(body, "=")
	if !ok || field == "" {
		return filter{}, fmt.Errorf("unsupported filter %q: want %q or %q", "["+body+"]", "[*]", "[field=value]")
	}
	return filter{field: field, value: value}, nil
}
