package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// dockerBox fakes a box's docker daemon, its config file and the commands the docker step reaches them by.
type dockerBox struct {
	// present reports whether the box holds dockerd.
	present bool
	// invalid makes the validation of any config fail.
	invalid bool
	// restartErr is what restarting the daemon fails with.
	restartErr error
	// path is the live config file.
	path string
	// validated are the contents handed to validation, in order.
	validated []string
	// restarts counts the daemon restarts.
	restarts int
}

func (d *dockerBox) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	line := strings.Join(append([]string{name}, args...), " ")
	switch {
	case line == "sudo dockerd --version":
		if !d.present {
			return exitStatus(127)
		}
		return nil
	case line == "sudo test -e "+d.path:
		if _, err := os.Stat(d.path); err != nil {
			return exitStatus(1)
		}
		return nil
	case line == "sudo cat "+d.path:
		data, err := os.ReadFile(d.path)
		if err != nil {
			return fmt.Errorf("read the canned config: %w", err)
		}
		return write(stdout, string(data))
	case strings.HasPrefix(line, "sudo dockerd --validate --config-file "):
		data, err := os.ReadFile(args[len(args)-1])
		if err != nil {
			return fmt.Errorf("read the config handed to validation: %w", err)
		}
		d.validated = append(d.validated, string(data))
		if d.invalid {
			return exitStatus(1)
		}
		return nil
	case strings.HasPrefix(line, "sudo install -D -m 0644 ") && strings.HasSuffix(line, " "+d.path):
		data, err := os.ReadFile(args[len(args)-2])
		if err != nil {
			return fmt.Errorf("read the config handed to install: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
			return fmt.Errorf("make the config directory: %w", err)
		}
		return os.WriteFile(d.path, data, 0o644)
	case line == "sudo systemctl restart docker":
		d.restarts++
		return d.restartErr
	}
	return fmt.Errorf("unexpected command %q", line)
}

// newDockerBox is a box with docker and, when config is not empty, a daemon.json holding it.
func newDockerBox(t *testing.T, config string) *dockerBox {
	t.Helper()
	box := &dockerBox{present: true, path: filepath.Join(t.TempDir(), "docker", "daemon.json")}
	if config != "" {
		if err := os.MkdirAll(filepath.Dir(box.path), 0o755); err != nil {
			t.Fatalf("make the config directory: %v", err)
		}
		if err := os.WriteFile(box.path, []byte(config), 0o644); err != nil {
			t.Fatalf("write the daemon.json: %v", err)
		}
	}
	return box
}

// convergeDocker runs the docker step against the box.
func (d *dockerBox) converge(t *testing.T) (string, error) {
	t.Helper()
	return convergeDocker(t.Context(), Env{Command: d, DockerConfig: d.path}, io.Discard)
}

// live is the daemon.json on the box, parsed to a generic value.
func (d *dockerBox) live(t *testing.T) any {
	t.Helper()
	return parseJSON(t, d.path)
}

func parseJSON(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return v
}

func loopbackDefaults() map[string]any {
	return map[string]any{
		"ip":                   "127.0.0.1",
		"default-network-opts": map[string]any{"bridge": map[string]any{"com.docker.network.bridge.host_binding_ipv4": "127.0.0.1"}},
	}
}

func TestDockerStepWritesLoopbackDefaultsAndRestarts(t *testing.T) {
	t.Parallel()
	box := newDockerBox(t, "")
	summary, err := box.converge(t)
	if err != nil {
		t.Fatalf("convergeDocker() error = %v, want nil", err)
	}
	if got := box.live(t); !reflect.DeepEqual(got, loopbackDefaults()) {
		t.Errorf("daemon.json = %v, want %v", got, loopbackDefaults())
	}
	if box.restarts != 1 || !strings.Contains(summary, "restarted docker") {
		t.Errorf("convergeDocker() = %q after %d restarts, want one restart said in the summary", summary, box.restarts)
	}
}

func TestDockerStepSecondRunChangesNothingAndDoesNotRestart(t *testing.T) {
	t.Parallel()
	box := newDockerBox(t, "")
	if _, err := box.converge(t); err != nil {
		t.Fatalf("first convergeDocker() error = %v", err)
	}
	before, err := os.ReadFile(box.path)
	if err != nil {
		t.Fatalf("read daemon.json: %v", err)
	}
	summary, err := box.converge(t)
	if err != nil {
		t.Fatalf("second convergeDocker() error = %v", err)
	}
	after, err := os.ReadFile(box.path)
	if err != nil {
		t.Fatalf("read daemon.json: %v", err)
	}
	if string(before) != string(after) || box.restarts != 1 || len(box.validated) != 1 {
		t.Errorf("second run: file changed %v, restarts %d, validations %d, want unchanged, 1 and 1 (summary %q)", string(before) != string(after), box.restarts, len(box.validated), summary)
	}
}

func TestDockerStepKeepsOtherKeysIncludingOtherBridgeOptions(t *testing.T) {
	t.Parallel()
	box := newDockerBox(t, `{"log-driver":"json-file","default-network-opts":{"bridge":{"com.docker.network.driver.mtu":"1400"},"overlay":{"x":"y"}}}`)
	if _, err := box.converge(t); err != nil {
		t.Fatalf("convergeDocker() error = %v, want nil", err)
	}
	want := map[string]any{
		"log-driver": "json-file",
		"ip":         "127.0.0.1",
		"default-network-opts": map[string]any{
			"bridge": map[string]any{
				"com.docker.network.driver.mtu":               "1400",
				"com.docker.network.bridge.host_binding_ipv4": "127.0.0.1",
			},
			"overlay": map[string]any{"x": "y"},
		},
	}
	if got := box.live(t); !reflect.DeepEqual(got, want) {
		t.Errorf("daemon.json = %v, want %v", got, want)
	}
}

func TestDockerStepKeepsAnOperatorsValueWithANote(t *testing.T) {
	t.Parallel()
	box := newDockerBox(t, `{"ip":"10.0.0.5","default-network-opts":{"bridge":{"com.docker.network.bridge.host_binding_ipv4":"0.0.0.0"}}}`)
	summary, err := box.converge(t)
	if err != nil {
		t.Fatalf("convergeDocker() error = %v, want nil", err)
	}
	want := map[string]any{
		"ip":                   "10.0.0.5",
		"default-network-opts": map[string]any{"bridge": map[string]any{"com.docker.network.bridge.host_binding_ipv4": "0.0.0.0"}},
	}
	if got := box.live(t); !reflect.DeepEqual(got, want) {
		t.Errorf("daemon.json = %v, want %v", got, want)
	}
	for _, note := range []string{"ip", "10.0.0.5", "host_binding_ipv4", "0.0.0.0"} {
		if !strings.Contains(summary, note) {
			t.Errorf("convergeDocker() = %q, want a note naming %q", summary, note)
		}
	}
	if box.restarts != 0 {
		t.Errorf("restarts = %d, want none when the operator's values leave nothing to change", box.restarts)
	}
}

func TestDockerStepFailsOnUnparseableConfigAndLeavesItUntouched(t *testing.T) {
	t.Parallel()
	const broken = `{"log-driver": `
	box := newDockerBox(t, broken)
	if _, err := box.converge(t); err == nil {
		t.Fatal("convergeDocker() error = nil, want a failure on an unparseable daemon.json")
	}
	got, err := os.ReadFile(box.path)
	if err != nil || string(got) != broken || box.restarts != 0 {
		t.Errorf("daemon.json = %q (%v) after %d restarts, want it untouched and no restart", got, err, box.restarts)
	}
}

func TestDockerStepKeepsTheLiveConfigWhenValidationFails(t *testing.T) {
	t.Parallel()
	const live = `{"log-driver":"json-file"}`
	box := newDockerBox(t, live)
	box.invalid = true
	if _, err := box.converge(t); err == nil {
		t.Fatal("convergeDocker() error = nil, want a failure when validation rejects the merge")
	}
	got, err := os.ReadFile(box.path)
	if err != nil || string(got) != live || box.restarts != 0 {
		t.Errorf("daemon.json = %q (%v) after %d restarts, want it unchanged and no restart", got, err, box.restarts)
	}
}

func TestDockerStepReportsAFailedRestart(t *testing.T) {
	t.Parallel()
	box := newDockerBox(t, "")
	box.restartErr = errors.New("unit not found")
	if _, err := box.converge(t); !errors.Is(err, box.restartErr) {
		t.Errorf("convergeDocker() error = %v, want it to wrap %v", err, box.restartErr)
	}
}

func TestDockerStepOnABoxWithoutDockerChangesNothing(t *testing.T) {
	t.Parallel()
	box := newDockerBox(t, "")
	box.present = false
	summary, err := box.converge(t)
	if err != nil {
		t.Fatalf("convergeDocker() error = %v, want nil", err)
	}
	if _, statErr := os.Stat(box.path); !os.IsNotExist(statErr) || box.restarts != 0 {
		t.Errorf("a box without docker got a daemon.json (stat %v) or %d restarts, want neither", statErr, box.restarts)
	}
	if want := "docker is not installed; nothing to change"; summary != want {
		t.Errorf("convergeDocker() = %q, want %q", summary, want)
	}
}
