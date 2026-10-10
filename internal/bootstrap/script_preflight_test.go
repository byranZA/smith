package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptPreflightPrintsTotalMemory(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	tests := []struct {
		name    string
		meminfo string
		want    string
	}{
		{"readable meminfo", "MemTotal:         469000 kB\nMemFree:           13000 kB\n", "mem-total-kb=469000\n"},
		{"missing meminfo", "", "mem-total-kb=\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir, scriptPath, env := scriptFixture(t)
			meminfo := filepath.Join(dir, "meminfo")
			if tt.meminfo != "" {
				if err := os.WriteFile(meminfo, []byte(tt.meminfo), 0o644); err != nil {
					t.Fatalf("write meminfo: %v", err)
				}
			}
			cmd := exec.Command(bash, scriptPath, "preflight")
			cmd.Env = append(env, "SMITH_MEMINFO="+meminfo)
			out, err := cmd.Output()
			if err != nil {
				t.Logf("preflight exited %v (host has no /etc/os-release?)", err)
			}
			if !strings.Contains(string(out), "\n"+tt.want) {
				t.Errorf("preflight output = %q, want it to carry %q", out, tt.want)
			}
		})
	}
}
