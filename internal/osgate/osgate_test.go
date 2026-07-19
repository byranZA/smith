package osgate

import "testing"

func TestParse(t *testing.T) {
	content := `NAME="Ubuntu"
VERSION="24.04.1 LTS (Noble Numbat)"
ID=ubuntu
ID_LIKE=debian
VERSION_ID="24.04"
PRETTY_NAME="Ubuntu 24.04.1 LTS"`

	got := Parse(content)
	want := Release{ID: "ubuntu", Version: "24.04.1 LTS (Noble Numbat)", VersionID: "24.04"}
	if got != want {
		t.Errorf("Parse() = %+v, want %+v", got, want)
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name          string
		release       Release
		wantSupported bool
	}{
		{
			name:          "noble LTS at the floor passes",
			release:       Release{ID: "ubuntu", Version: "24.04.1 LTS (Noble)", VersionID: "24.04"},
			wantSupported: true,
		},
		{
			name:          "future LTS above the floor passes",
			release:       Release{ID: "ubuntu", Version: "26.04 LTS", VersionID: "26.04"},
			wantSupported: true,
		},
		{
			name:          "unseen far-future LTS passes on the floor-plus-LTS rule alone",
			release:       Release{ID: "ubuntu", Version: "40.04 LTS (Imaginary)", VersionID: "40.04"},
			wantSupported: true,
		},
		{
			name:          "interim release without an LTS token rejects",
			release:       Release{ID: "ubuntu", Version: "23.10 (Mantic)", VersionID: "23.10"},
			wantSupported: false,
		},
		{
			name:          "LTS below the 24.04 floor rejects",
			release:       Release{ID: "ubuntu", Version: "22.04.3 LTS (Jammy)", VersionID: "22.04"},
			wantSupported: false,
		},
		{
			name:          "non-ubuntu distro rejects",
			release:       Release{ID: "debian", Version: "12 (Bookworm)", VersionID: "12"},
			wantSupported: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.release)
			if got.Supported != tt.wantSupported {
				t.Errorf("Evaluate(%+v).Supported = %v, want %v (reason: %q)",
					tt.release, got.Supported, tt.wantSupported, got.Reason)
			}
			if !got.Supported && got.Reason == "" {
				t.Errorf("Evaluate(%+v) rejected without a reason", tt.release)
			}
		})
	}
}

func TestEvaluateRejectionReasons(t *testing.T) {
	tests := []struct {
		name    string
		release Release
		want    string
	}{
		{"not ubuntu", Release{ID: "debian", Version: "12", VersionID: "12"}, "not an Ubuntu box"},
		{"not LTS", Release{ID: "ubuntu", Version: "23.10 (Mantic)", VersionID: "23.10"}, "not an LTS release"},
		{"below floor", Release{ID: "ubuntu", Version: "22.04.3 LTS (Jammy)", VersionID: "22.04"}, "below the 24.04 LTS floor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Evaluate(tt.release); got.Reason != tt.want {
				t.Errorf("Evaluate(%+v).Reason = %q, want %q", tt.release, got.Reason, tt.want)
			}
		})
	}
}
