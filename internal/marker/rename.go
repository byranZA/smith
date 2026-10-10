package marker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

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
// object data holds, and whether the object has a name field at all. An
// object with no fields is an error, since nothing smith writes is empty.
func nameSpan(data []byte) (start, end int, found bool, err error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return 0, 0, false, errors.New("decode marker: not a JSON object")
	}
	if !dec.More() {
		return 0, 0, false, errors.New("decode marker: the marker records nothing")
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return 0, 0, false, fmt.Errorf("decode marker: %w", err)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return 0, 0, false, fmt.Errorf("decode marker: %w", err)
		}
		if key == nameKey {
			end := int(dec.InputOffset())
			return end - len(raw), end, true, nil
		}
	}
	return 0, 0, false, nil
}

// splice returns a copy of data with data[start:end] replaced by insert.
func splice(data []byte, start, end int, insert []byte) []byte {
	out := make([]byte, 0, len(data)-(end-start)+len(insert))
	out = append(out, data[:start]...)
	out = append(out, insert...)
	return append(out, data[end:]...)
}
