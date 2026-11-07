package status

import (
	"fmt"
	"strings"
)

// DockerPorts is what the probe read of the ports containers publish. The zero
// value reports nothing: no Docker, no exposed binding, or no probe line.
type DockerPorts struct {
	// Exposed are the container bindings on a non-loopback address, each as
	// "container binding".
	Exposed []string
	// Unreadable reports that Docker is present but its bindings could not be
	// read, so nothing can be said of them.
	Unreadable bool
}

// readDocker folds one probe line into the docker facts: the state line, or a
// container's published ports.
func (d *DockerPorts) readDocker(key, value string) {
	switch key {
	case "docker-ports":
		d.Unreadable = value == "?"
	case "docker-container":
		name, ports, _ := strings.Cut(value, " ")
		for _, binding := range strings.Split(ports, ", ") {
			if exposed(binding) {
				d.Exposed = append(d.Exposed, name+" "+binding)
			}
		}
	}
}

// exposed reports whether a `docker ps` port binding is published on an
// address other than loopback. A port with no "->" is not published.
func exposed(binding string) bool {
	host, _, published := strings.Cut(binding, "->")
	if !published {
		return false
	}
	return !strings.HasPrefix(host, "127.") && !strings.HasPrefix(host, "[::1]:")
}

// writeDocker appends the docker advisory: a warning for each exposed binding,
// or that the bindings could not be checked. It never touches the verdict.
func (r Report) writeDocker(b *strings.Builder) {
	if r.Docker.Unreadable {
		b.WriteString("  docker: could not check the published ports\n")
	}
	for _, binding := range r.Docker.Exposed {
		fmt.Fprintf(b, "  warning: docker publishes %s beyond loopback, around the firewall\n", binding)
	}
}
