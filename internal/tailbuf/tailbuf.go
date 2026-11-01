// Package tailbuf keeps the end of a stream of output within a fixed size.
package tailbuf

// Buffer is an io.Writer that keeps only the last Limit bytes written to it.
// The zero value keeps nothing.
type Buffer struct {
	// Limit is the most bytes the buffer keeps.
	Limit int

	buf     []byte
	dropped bool
}

// Write appends p, dropping the oldest bytes beyond Limit. It never fails.
func (b *Buffer) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	if over := len(b.buf) - b.Limit; over > 0 {
		b.buf = b.buf[over:]
		b.dropped = true
	}
	return len(p), nil
}

// Bytes returns the kept bytes.
func (b *Buffer) Bytes() []byte { return b.buf }

// Dropped reports whether earlier output was cut to stay within Limit.
func (b *Buffer) Dropped() bool { return b.dropped }
