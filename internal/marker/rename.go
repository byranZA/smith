package marker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrMalformed reports marker data that is not exactly one JSON object with
// fields and at most one name, so Rename cannot say which name the box records.
var ErrMalformed = errors.New("malformed marker")

// nameKey is the marker field Rename rewrites.
const nameKey = "name"

// Rename returns the marker data with the box's name set to name and every
// other byte left as it was: no field, unknown field, schema version or
// timestamp is touched, because renaming a box is not a setup run. A marker
// that records no name gains one after its opening brace. Malformed JSON, or
// anything but an object with fields, is an error.
func Rename(data []byte, name string) ([]byte, error) {
	value, err := json.Marshal(name)
	if err != nil {
		return nil, fmt.Errorf("encode box name %q: %w", name, err)
	}
	start, end, found, err := nameSpan(data)
	if err != nil {
		return nil, err
	}
	if !found {
		open := bytes.IndexByte(data, '{') + 1
		field := append([]byte("\n  \""+nameKey+"\": "), value...)
		return splice(data, open, open, append(field, ',')), nil
	}
	return splice(data, start, end, value), nil
}

// nameSpan returns the byte range of the name field's value in the top-level
// object data holds, and whether the object has a name field at all. The data
// must be exactly one JSON object with fields and at most one name field, since
// a marker with two would decode as a name other than the one rewritten.
func nameSpan(data []byte) (start, end int, found bool, err error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return 0, 0, false, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if tok != json.Delim('{') {
		return 0, 0, false, fmt.Errorf("%w: not a JSON object", ErrMalformed)
	}
	if !dec.More() {
		return 0, 0, false, fmt.Errorf("%w: it records nothing", ErrMalformed)
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return 0, 0, false, fmt.Errorf("%w: %w", ErrMalformed, err)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return 0, 0, false, fmt.Errorf("%w: %w", ErrMalformed, err)
		}
		if key != nameKey {
			continue
		}
		if found {
			return 0, 0, false, fmt.Errorf("%w: it records more than one name", ErrMalformed)
		}
		end = int(dec.InputOffset())
		start, found = end-len(raw), true
	}
	if err := closeObject(dec); err != nil {
		return 0, 0, false, err
	}
	return start, end, found, nil
}

// closeObject consumes the closing brace of the object dec is inside and
// requires nothing but whitespace after it.
func closeObject(dec *json.Decoder) error {
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: data follows the marker", ErrMalformed)
	}
	return nil
}

// splice returns a copy of data with data[start:end] replaced by insert.
func splice(data []byte, start, end int, insert []byte) []byte {
	out := make([]byte, 0, len(data)-(end-start)+len(insert))
	out = append(out, data[:start]...)
	out = append(out, insert...)
	return append(out, data[end:]...)
}
