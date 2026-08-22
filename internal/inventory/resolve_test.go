package inventory_test

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/inventory"
)

// registered is the inventory every resolution case is decided against: one
// box under a bare name, and one whose name is itself a literal ssh target.
func registered() inventory.Inventory {
	return inventory.Inventory{
		SchemaVersion: inventory.SchemaVersion,
		Boxes: map[string]inventory.Box{
			"dev":               {Target: "smith@100.92.14.7"},
			"root@203.0.113.10": {Target: "smith@198.51.100.9"},
		},
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		inv  inventory.Inventory
		arg  string
		want string
	}{
		{"a bare name resolves through the inventory", registered(), "dev", "smith@100.92.14.7"},
		{"a value containing @ is never looked up", registered(), "root@203.0.113.10", "root@203.0.113.10"},
		{"an unknown bare name is handed to ssh verbatim", registered(), "myalias", "myalias"},
		{"a name that looks like a host is handed to ssh verbatim", registered(), "203.0.113.10", "203.0.113.10"},
		{"an empty inventory resolves nothing", inventory.Empty(), "dev", "dev"},
		{"an absent box map resolves nothing", inventory.Inventory{}, "dev", "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := inventory.Resolve(tt.inv, tt.arg); got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.arg, got, tt.want)
			}
		})
	}
}

func TestConnectHintNamesBothFixesForAnUnregisteredBareValue(t *testing.T) {
	t.Parallel()

	hint := inventory.ConnectHint(registered(), "203.0.113.10")

	for _, want := range []string{"203.0.113.10", "smith@203.0.113.10", "smith machine add"} {
		if !strings.Contains(hint, want) {
			t.Errorf("ConnectHint = %q, want it to name %q", hint, want)
		}
	}
}

func TestConnectHintStaysSilentWhenThereIsNothingToSuggest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		arg  string
	}{
		{"a registered name resolved to a proven target", "dev"},
		{"a literal target was never a lookup", "root@203.0.113.10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if hint := inventory.ConnectHint(registered(), tt.arg); hint != "" {
				t.Errorf("ConnectHint(%q) = %q, want no hint", tt.arg, hint)
			}
		})
	}
}
