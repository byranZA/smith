package release

import (
	"errors"
	"strings"
	"testing"
)

func TestForNamesTheArchiveAndWhereItIsPublished(t *testing.T) {
	got := For("0.2.0", "amd64")

	if want := "smith_0.2.0_linux_amd64.tar.gz"; got.Name != want {
		t.Errorf("For().Name = %q, want %q", got.Name, want)
	}
	if want := "https://github.com/byranZA/smith/releases/download/v0.2.0/smith_0.2.0_linux_amd64.tar.gz"; got.URL != want {
		t.Errorf("For().URL = %q, want %q", got.URL, want)
	}
	if want := "https://github.com/byranZA/smith/releases/download/v0.2.0/checksums.txt"; got.ChecksumsURL != want {
		t.Errorf("For().ChecksumsURL = %q, want %q", got.ChecksumsURL, want)
	}
}

func TestForCarriesTheArchitectureIntoTheAssetName(t *testing.T) {
	got := For("0.2.0", "arm64")

	if want := "smith_0.2.0_linux_arm64.tar.gz"; got.Name != want {
		t.Errorf("For().Name = %q, want %q", got.Name, want)
	}
}

func TestArchMapsMachineHardwareNames(t *testing.T) {
	tests := []struct {
		machine string
		want    string
	}{
		{"x86_64", "amd64"},
		{"aarch64", "arm64"},
	}
	for _, tt := range tests {
		t.Run(tt.machine, func(t *testing.T) {
			got, err := Arch(tt.machine)
			if err != nil {
				t.Fatalf("Arch(%q) errored: %v", tt.machine, err)
			}
			if got != tt.want {
				t.Errorf("Arch(%q) = %q, want %q", tt.machine, got, tt.want)
			}
		})
	}
}

func TestArchRefusesAnUnsupportedMachineByName(t *testing.T) {
	_, err := Arch("riscv64")

	var unsupported *UnsupportedArchError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Arch(riscv64) error = %v, want an *UnsupportedArchError", err)
	}
	if unsupported.Machine != "riscv64" {
		t.Errorf("Machine = %q, want %q", unsupported.Machine, "riscv64")
	}
	if !strings.Contains(err.Error(), "riscv64") {
		t.Errorf("error %q does not name the machine hardware name", err)
	}
}

func TestInstallable(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"a released tag keeps its version without the prefix", "v0.1.0", "0.1.0"},
		{"a prerelease tag strips the prefix too", "v0.1.0-rc.1", "0.1.0-rc.1"},
		{"a version already without the prefix survives", "0.1.0", "0.1.0"},
		{"the dev fallback has no release", "dev", ""},
		{"an unreported module version has no release", "", ""},
		{"a bare devel build has no release", "(devel)", ""},
		{"a pseudo-version has no release", "v0.0.0-20260728155309-f8e683ead820", ""},
		{"a dirty pseudo-version has no release", "v0.0.0-20260728155309-f8e683ead820+dirty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Installable(tt.version); got != tt.want {
				t.Errorf("Installable(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}
