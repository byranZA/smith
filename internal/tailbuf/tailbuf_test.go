package tailbuf_test

import (
	"testing"

	"github.com/byranZA/smith/internal/tailbuf"
)

func TestBufferKeepsOnlyTheLastLimitBytes(t *testing.T) {
	b := tailbuf.Buffer{Limit: 5}
	if _, err := b.Write([]byte("abcd")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := b.Write([]byte("efgh")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if got := string(b.Bytes()); got != "defgh" {
		t.Errorf("Bytes() = %q, want %q", got, "defgh")
	}
	if !b.Dropped() {
		t.Error("Dropped() = false, want true")
	}
}

func TestBufferWithinTheLimitDropsNothing(t *testing.T) {
	b := tailbuf.Buffer{Limit: 8}
	if _, err := b.Write([]byte("abcd")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if string(b.Bytes()) != "abcd" || b.Dropped() {
		t.Errorf("Bytes() = %q, Dropped() = %v; want %q and false", b.Bytes(), b.Dropped(), "abcd")
	}
}

func TestBufferLinesSkipsTheCutLineAfterADrop(t *testing.T) {
	b := tailbuf.Buffer{Limit: 8}
	if _, err := b.Write([]byte("abc\ndef\nghi\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if got, want := string(b.Lines()), "ghi\n"; got != want {
		t.Errorf("Lines() = %q, want %q", got, want)
	}
}
