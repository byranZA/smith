package blueprint

import "testing"

func TestSameRepo(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"ssh scp form and https", "git@github.com:acme/api.git", "https://github.com/acme/api", true},
		{"case and git suffix", "https://github.com/Acme/API.git", "git@github.com:acme/api.git", true},
		{"ssh scheme and https git", "ssh://git@github.com/acme/api", "https://github.com/acme/api.git", true},
		{"bare host and path", "github.com/acme/api", "https://github.com/acme/api", true},
		{"trailing slash", "https://github.com/acme/api/", "github.com/acme/api", true},
		{"ssh port is dropped", "ssh://git@github.com:22/acme/api.git", "https://github.com/acme/api", true},
		{"same name other owner", "github.com/other/api", "github.com/acme/api", false},
		{"other host", "https://gitlab.com/acme/api", "https://github.com/acme/api", false},
		{"empty never matches", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameRepo(tc.a, tc.b); got != tc.want {
				t.Errorf("SameRepo(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
