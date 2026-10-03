package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// shippedCopy places the embedded script in its own directory inside the
// fixture, as the shipped package does on a box, and returns that directory and
// the script's path within it.
func shippedCopy(t *testing.T, fixtureDir string) (shippedDir, scriptPath string) {
	t.Helper()
	shippedDir = filepath.Join(fixtureDir, "smith.shipped")
	if err := os.Mkdir(shippedDir, 0o700); err != nil {
		t.Fatalf("mkdir shipped dir: %v", err)
	}
	scriptPath = filepath.Join(shippedDir, "script.sh")
	if err := os.WriteFile(scriptPath, []byte(Script), 0o755); err != nil {
		t.Fatalf("write shipped script: %v", err)
	}
	return shippedDir, scriptPath
}

// TestScriptSetupRemovesTheDirectoryItWasGiven proves setup removes the
// directory named by --remove-dir however the phases end, so no shipped copy
// outlives the run — even on a root login that hardening closes before anyone
// else could remove it.
func TestScriptSetupRemovesTheDirectoryItWasGiven(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	tests := []struct {
		name     string
		ssh      string
		wantFail bool
	}{
		{name: "all phases complete", ssh: "#!/usr/bin/env bash\nexit 0\n"},
		{
			name:     "a phase fails mid-run",
			ssh:      "#!/usr/bin/env bash\necho 'smith@localhost: Permission denied (publickey).' >&2\nexit 255\n",
			wantFail: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, _, env := scriptFixture(t)
			writeFakeBin(t, filepath.Join(dir, "bin"), "ssh", tt.ssh)
			shippedDir, scriptPath := shippedCopy(t, dir)

			cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test",
				"--remove-dir", shippedDir)
			cmd.Env = append(os.Environ(), env...)
			out, err := cmd.CombinedOutput()
			if failed := err != nil; failed != tt.wantFail {
				t.Fatalf("setup error = %v, want failed=%v\n%s", err, tt.wantFail, out)
			}
			if _, err := os.Stat(shippedDir); !os.IsNotExist(err) {
				t.Errorf("shipped directory %q still exists after setup (stat err = %v)\n%s", shippedDir, err, out)
			}
		})
	}
}

// TestScriptSetupWithoutRemoveDirRemovesNothing proves a setup run that names
// no directory to remove leaves the directory it sits in alone, so a script an
// operator runs by hand never deletes their files.
func TestScriptSetupWithoutRemoveDirRemovesNothing(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, _, env := scriptFixture(t)
	shippedDir, scriptPath := shippedCopy(t, dir)

	cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test")
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup run failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Errorf("script in %q gone after setup without --remove-dir: %v", shippedDir, err)
	}
}

// TestScriptPreflightKeepsTheShippedCopyForSetup proves preflight leaves its
// shipped copy on the box, so setup runs against that same copy and only setup
// removes it.
func TestScriptPreflightKeepsTheShippedCopyForSetup(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir, _, env := scriptFixture(t)
	shippedDir, scriptPath := shippedCopy(t, dir)

	// Preflight's exit status is not under test: it reads the host's
	// /etc/os-release, which a development machine may lack.
	preflight := exec.Command(bash, scriptPath, "preflight")
	preflight.Env = append(os.Environ(), env...)
	if out, err := preflight.CombinedOutput(); err != nil {
		t.Logf("preflight exited %v (host has no /etc/os-release?)\n%s", err, out)
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("shipped script gone after preflight: %v", err)
	}

	setup := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test",
		"--remove-dir", shippedDir)
	setup.Env = append(os.Environ(), env...)
	if out, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("setup against preflight's copy failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(shippedDir); !os.IsNotExist(err) {
		t.Errorf("shipped directory %q still exists after setup (stat err = %v)", shippedDir, err)
	}
}

// TestScriptSetupKeepsItsExitStatusWhenRemovalFails proves a shipped-directory
// removal that fails never changes setup's result: a completed setup still
// exits zero, and a failed one keeps the status its phase failed with.
func TestScriptSetupKeepsItsExitStatusWhenRemovalFails(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	tests := []struct {
		name string
		ssh  string
	}{
		{name: "all phases complete", ssh: "#!/usr/bin/env bash\nexit 0\n"},
		{
			name: "a phase fails mid-run",
			ssh:  "#!/usr/bin/env bash\necho 'smith@localhost: Permission denied (publickey).' >&2\nexit 255\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// exitCode runs setup against a fresh shipped copy and returns its
			// exit status, with the shipped directory's removal failing when
			// removalFails is set.
			exitCode := func(removalFails bool) int {
				t.Helper()
				dir, _, env := scriptFixture(t)
				binDir := filepath.Join(dir, "bin")
				writeFakeBin(t, binDir, "ssh", tt.ssh)
				shippedDir, scriptPath := shippedCopy(t, dir)
				if removalFails {
					// The fake rm refuses only the shipped directory, so every
					// other removal the phases make still works. Its status, 7,
					// differs from any phase failure, so a replaced status shows.
					rmPath, err := exec.LookPath("rm")
					if err != nil {
						t.Fatalf("look up rm: %v", err)
					}
					writeFakeBin(t, binDir, "rm", "#!/usr/bin/env bash\n"+
						"for arg in \"$@\"; do\n"+
						"  if [ \"$arg\" = '"+shippedDir+"' ]; then echo 'rm: permission denied' >&2; exit 7; fi\n"+
						"done\n"+
						"exec '"+rmPath+"' \"$@\"\n")
				}
				cmd := exec.Command(bash, scriptPath, "setup", "--access", "public", "--smith-version", "9.9.9-test",
					"--remove-dir", shippedDir)
				cmd.Env = append(os.Environ(), env...)
				out, err := cmd.CombinedOutput()
				code := cmd.ProcessState.ExitCode()
				if err != nil && code < 0 {
					t.Fatalf("setup did not run: %v\n%s", err, out)
				}
				if removalFails {
					if _, err := os.Stat(scriptPath); err != nil {
						t.Fatalf("fake rm did not refuse the shipped directory: %v\n%s", err, out)
					}
				}
				return code
			}

			want := exitCode(false)
			if got := exitCode(true); got != want {
				t.Errorf("setup exit status with failing removal = %d, want %d (status without the failure)", got, want)
			}
		})
	}
}
