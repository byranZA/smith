package staging

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/marker"
)

// pointerBox stands in for a box whose marker the pointer rule reads. It
// answers the marker read with whatever JSON the box is set up to hold, and
// records every command so a test can prove a refusal touched nothing.
type pointerBox struct {
	marker string

	commands []string
}

func (b *pointerBox) Run(_ context.Context, cmd string, stdout, _ io.Writer) error {
	b.commands = append(b.commands, cmd)
	if strings.Contains(cmd, marker.Path) {
		if _, err := io.WriteString(stdout, b.marker); err != nil {
			return err
		}
	}
	return nil
}

func (b *pointerBox) RunWithInput(_ context.Context, cmd string, stdin io.Reader, _, _ io.Writer) error {
	b.commands = append(b.commands, cmd)
	return nil
}

func TestCheckPointerRefusesABlueprintlessRerunNamingTheBlueprint(t *testing.T) {
	t.Parallel()
	box := &pointerBox{marker: `{"schema_version":1,"access_mode":"public","blueprint":"acme"}`}

	err := CheckPointer(context.Background(), box, "")
	if err == nil {
		t.Fatal("CheckPointer() err = nil, want a box built from a blueprint refused a blueprint-less re-run")
	}
	if !strings.Contains(err.Error(), "acme") {
		t.Errorf("CheckPointer() err = %v, want it to name the blueprint the box was built from", err)
	}
	for _, cmd := range box.commands {
		if !strings.Contains(cmd, marker.Path) {
			t.Errorf("CheckPointer() ran %q on the box, want the refusal to read and write nothing", cmd)
		}
	}
}

func TestCheckPointerAllowsARunAgainstABoxThatRecordsNoBlueprint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		marker string
	}{
		{"a marker recording no blueprint", `{"schema_version":1,"access_mode":"public","blueprint":""}`},
		{"a marker written before the pointer existed", `{"schema_version":1,"access_mode":"public"}`},
		{"no marker at all", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := CheckPointer(context.Background(), &pointerBox{marker: tt.marker}, ""); err != nil {
				t.Errorf("CheckPointer() err = %v, want a blueprint-less run to be allowed", err)
			}
		})
	}
}

func TestCheckPointerLeavesTheBoxUnreadWhenTheRunNamesABlueprint(t *testing.T) {
	t.Parallel()
	box := &pointerBox{marker: `{"schema_version":1,"blueprint":"acme"}`}

	if err := CheckPointer(context.Background(), box, "other"); err != nil {
		t.Fatalf("CheckPointer() err = %v, want a run naming a blueprint to be allowed", err)
	}
	if len(box.commands) != 0 {
		t.Errorf("CheckPointer() ran %q on the box, want nothing asked of it when the run names a blueprint", box.commands)
	}
}
