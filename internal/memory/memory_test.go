package memory

import "testing"

func TestAdvisory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		total Total
		want  string
	}{
		{"just under 1.75 GB warns", FromKiB(1835007), "memory: 1.7 GB\nwarning: under the recommended 2 GB of memory to run an agent"},
		{"exactly 1.75 GB does not warn", FromKiB(1835008), "memory: 1.8 GB"},
		{"nominal 2 GB box reporting about 1.9 GB does not warn", FromKiB(2014000), "memory: 1.9 GB"},
		{"well over 2 GB does not warn", FromKiB(8148000), "memory: 7.8 GB"},
		{"512 MB droplet warns in MB", FromKiB(469000), "memory: 458 MB\nwarning: under the recommended 2 GB of memory to run an agent"},
		{"unknown memory does not warn", Total{}, "memory: unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.total.Advisory(); got != tt.want {
				t.Errorf("%+v.Advisory() = %q, want %q", tt.total, got, tt.want)
			}
		})
	}
}
