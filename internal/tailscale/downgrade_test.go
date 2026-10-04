package tailscale

import (
	"errors"
	"testing"

	"github.com/byranZA/smith/internal/config"
)

func TestDowngrade(t *testing.T) {
	cases := []struct {
		name     string
		recorded string
		resolved config.Value
		refused  bool
	}{
		{"tailscale to public from the default is refused", "tailscale",
			config.Value{Value: "public", Origin: config.FromDefault}, true},
		{"tailscale to public from a preference is refused", "tailscale",
			config.Value{Value: "public", Origin: config.FromPreferences}, true},
		{"tailscale to public from the blueprint is refused", "tailscale",
			config.Value{Value: "public", Origin: config.FromBlueprint}, true},
		{"tailscale to public from the flag proceeds", "tailscale",
			config.Value{Value: "public", Origin: config.FromFlag}, false},
		{"tailscale to tailscale proceeds", "tailscale",
			config.Value{Value: "tailscale", Origin: config.FromDefault}, false},
		{"public to tailscale proceeds", "public",
			config.Value{Value: "tailscale", Origin: config.FromBlueprint}, false},
		{"public to public proceeds", "public",
			config.Value{Value: "public", Origin: config.FromDefault}, false},
		{"no recorded mode proceeds", "",
			config.Value{Value: "public", Origin: config.FromDefault}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Downgrade("devbox", tc.recorded, tc.resolved)
			if !tc.refused {
				if err != nil {
					t.Fatalf("Downgrade = %v, want nil", err)
				}
				return
			}
			var de *DowngradeError
			if !errors.As(err, &de) {
				t.Fatalf("Downgrade = %v, want a *DowngradeError", err)
			}
			want := DowngradeError{Box: "devbox", Recorded: tc.recorded, Resolved: tc.resolved}
			if *de != want {
				t.Errorf("DowngradeError = %+v, want %+v", *de, want)
			}
		})
	}
}

func TestDowngradeErrorMessage(t *testing.T) {
	err := &DowngradeError{
		Box:      "devbox",
		Recorded: "tailscale",
		Resolved: config.Value{Value: "public", Origin: config.FromBlueprint},
	}
	want := "devbox was provisioned with access tailscale, but this run resolves access: public (blueprint); " +
		"pass --access tailscale to keep it, or --access public to reopen public SSH"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
