package staging

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

func TestPlanStagesTheDocumentVerbatim(t *testing.T) {
	document := []byte("access: tailscale\nprovider:\n  create: [hcloud, server, create]\n")
	tree := Plan(document, blueprint.Blueprint{})
	if got := string(tree.Document.Bytes); got != string(document) {
		t.Errorf("Document.Bytes = %q, want %q", got, document)
	}
}

func TestPlanFiltersNoFieldOutOfTheDocument(t *testing.T) {
	document := []byte("access: tailscale\nterminal: tmux\nprovider:\n  create: [hcloud]\n")
	tree := Plan(document, blueprint.Blueprint{})
	for _, want := range []string{"provider:", "access: tailscale"} {
		if !strings.Contains(string(tree.Document.Bytes), want) {
			t.Errorf("staged document lost %q:\n%s", want, tree.Document.Bytes)
		}
	}
}

func TestPlanPlacesTheDocumentRootOwnedAt0644(t *testing.T) {
	tree := Plan([]byte("access: public\n"), blueprint.Blueprint{})
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"path", tree.Document.Path, "/etc/smith/blueprint.yaml"},
		{"mode", tree.Document.Mode, "0644"},
		{"owner", tree.Document.Owner, "root:root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Document %s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestPlanStagesUnderTheBoxConfigDirectoryOnly(t *testing.T) {
	tree := Plan([]byte("access: public\n"), blueprint.Blueprint{})
	if !strings.HasPrefix(tree.Document.Path, "/etc/smith/") {
		t.Errorf("Document.Path = %q, want a path under /etc/smith/", tree.Document.Path)
	}
	if strings.Contains(tree.Document.Path, ".smith") {
		t.Errorf("Document.Path = %q, want no operator config home on the box", tree.Document.Path)
	}
}
