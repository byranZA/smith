package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// defaultDockerConfig is where the docker daemon reads its config on a real box.
const defaultDockerConfig = "/etc/docker/daemon.json"

// Keys of the two settings that make published ports default to loopback.
const (
	ipKey       = "ip"
	networkOpts = "default-network-opts"
	bridgeKey   = "bridge"
	bindingKey  = "com.docker.network.bridge.host_binding_ipv4"
	loopback    = "127.0.0.1"
)

// convergeDocker makes published ports default to loopback in the daemon's
// config, restarting the daemon only when the file changed. A box without
// docker changes nothing and says so.
func convergeDocker(ctx context.Context, env Env, progress io.Writer) (string, error) {
	if err := env.Command.Run(ctx, "sudo", []string{"dockerd", "--version"}, nil, io.Discard, io.Discard); err != nil {
		return "docker is not installed; nothing to change", nil
	}
	path := env.dockerConfig()
	existing, err := readDockerConfig(ctx, env.Command, path)
	if err != nil {
		return "", err
	}
	merged, notes, changed, err := mergeLoopbackDefaults(existing)
	if err != nil {
		return "", fmt.Errorf("merge %s: %w", path, err)
	}
	suffix := noteSuffix(notes)
	if !changed {
		return "nothing to change; published ports already default to loopback" + suffix, nil
	}
	if err := replaceDockerConfig(ctx, env.Command, path, merged, progress); err != nil {
		return "", err
	}
	if err := env.Command.Run(ctx, "sudo", []string{"systemctl", "restart", "docker"}, nil, progress, progress); err != nil {
		return "", fmt.Errorf("restart docker to apply %s: %w", path, err)
	}
	return "published ports now default to loopback in " + path + "; restarted docker" + suffix, nil
}

// noteSuffix renders the notes to append to a summary, none when there are none.
func noteSuffix(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	return "; note: " + strings.Join(notes, "; ")
}

// readDockerConfig returns the daemon's config at path, nil when the file does not exist.
func readDockerConfig(ctx context.Context, run Runner, path string) ([]byte, error) {
	if err := run.Run(ctx, "sudo", []string{"test", "-e", path}, nil, io.Discard, io.Discard); err != nil {
		return nil, nil
	}
	var out bytes.Buffer
	if err := run.Run(ctx, "sudo", []string{"cat", path}, nil, &out, io.Discard); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out.Bytes(), nil
}

// replaceDockerConfig validates data as a daemon config and only then replaces
// the live file at path with it, so a bad merge never reaches the daemon.
func replaceDockerConfig(ctx context.Context, run Runner, path string, data []byte, progress io.Writer) error {
	tmp, err := os.CreateTemp("", "daemon-*.json")
	if err != nil {
		return fmt.Errorf("stage the merged %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // removal is best effort
	if _, err := tmp.Write(data); err != nil {
		return errors.Join(fmt.Errorf("stage the merged %s: %w", path, err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("stage the merged %s: %w", path, err)
	}
	if err := run.Run(ctx, "sudo", []string{"dockerd", "--validate", "--config-file", tmp.Name()}, nil, progress, progress); err != nil {
		return fmt.Errorf("the merged %s failed dockerd validation, so the live file is unchanged: %w", path, err)
	}
	if err := run.Run(ctx, "sudo", []string{"install", "-D", "-m", "0644", tmp.Name(), path}, nil, progress, progress); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// mergeLoopbackDefaults adds the two loopback settings to a daemon config,
// keeping every other key and any value the operator already set for either
// setting, which it notes. It reports whether the result differs from the
// input; empty input is an empty config.
func mergeLoopbackDefaults(existing []byte) (merged []byte, notes []string, changed bool, err error) {
	top := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if top, err = objectAt(existing, "the daemon config"); err != nil {
			return nil, nil, false, err
		}
	}
	var ipChanged, bindingChanged bool
	top[ipKey], notes, ipChanged = defaultTo(top[ipKey], ipKey, notes)
	opts, err := objectAt(top[networkOpts], networkOpts)
	if err != nil {
		return nil, nil, false, err
	}
	bridge, err := objectAt(opts[bridgeKey], networkOpts+"."+bridgeKey)
	if err != nil {
		return nil, nil, false, err
	}
	bridge[bindingKey], notes, bindingChanged = defaultTo(bridge[bindingKey], bindingKey, notes)
	if opts[bridgeKey], err = json.Marshal(bridge); err != nil {
		return nil, nil, false, fmt.Errorf("encode %s: %w", bridgeKey, err)
	}
	if top[networkOpts], err = json.Marshal(opts); err != nil {
		return nil, nil, false, fmt.Errorf("encode %s: %w", networkOpts, err)
	}
	merged, err = json.MarshalIndent(top, "", "  ")
	if err != nil {
		return nil, nil, false, fmt.Errorf("encode the daemon config: %w", err)
	}
	return append(merged, '\n'), notes, ipChanged || bindingChanged, nil
}

// defaultTo is the value a setting should hold: loopback when the operator set
// none, else theirs, with a note when theirs is not loopback. It reports
// whether it filled the setting in.
func defaultTo(current json.RawMessage, name string, notes []string) (json.RawMessage, []string, bool) {
	if current == nil {
		return json.RawMessage(`"` + loopback + `"`), notes, true
	}
	var value string
	if err := json.Unmarshal(current, &value); err == nil && value == loopback {
		return current, notes, false
	}
	return current, append(notes, fmt.Sprintf("kept your %s of %s, so ports published without an address are not forced to loopback", name, current)), false
}

// objectAt decodes raw as a JSON object, empty when raw is absent.
func objectAt(raw json.RawMessage, name string) (map[string]json.RawMessage, error) {
	if raw == nil {
		return map[string]json.RawMessage{}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("%s is not a JSON object: %w", name, err)
	}
	if obj == nil {
		return nil, fmt.Errorf("%s is null, not a JSON object", name)
	}
	return obj, nil
}
