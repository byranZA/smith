package status

import (
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/marker"
)

func TestGatherReadsPublishedDockerBindings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		lines string
		want  DockerPorts
	}{
		{"nothing reported", "", DockerPorts{}},
		{"no docker", "docker-ports=absent\n", DockerPorts{}},
		{"unreadable", "docker-ports=?\n", DockerPorts{Unreadable: true}},
		{"checked with none", "docker-ports=ok\n", DockerPorts{}},
		{
			"container bindings",
			"docker-ports=ok\ndocker-container=web 0.0.0.0:8083->80/tcp, [::]:8083->80/tcp, 127.0.0.1:5432->5432/tcp\ndocker-container=db 127.0.0.1:5433->5432/tcp\n",
			DockerPorts{Exposed: []string{"web 0.0.0.0:8083->80/tcp", "web [::]:8083->80/tcp"}},
		},
		{
			"port range and unpublished port",
			"docker-ports=ok\ndocker-container=app 0.0.0.0:8000-8002->8000-8002/tcp, 9000/tcp\n",
			DockerPorts{Exposed: []string{"app 0.0.0.0:8000-8002->8000-8002/tcp"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g, err := NewProber(&scriptedConn{}, probing(publicProbe+tt.lines), &fakeAdmin{}).Gather(t.Context())
			if err != nil {
				t.Fatalf("Gather() error = %v", err)
			}
			if !reflect.DeepEqual(g.Facts.Docker, tt.want) {
				t.Errorf("Gather() docker = %+v, want %+v", g.Facts.Docker, tt.want)
			}
		})
	}
}

func TestReconcileWarnsOfExposedDockerBindingsWithoutChangingTheVerdict(t *testing.T) {
	t.Parallel()
	facts := cleanPublicFacts()
	facts.Docker = DockerPorts{Exposed: []string{"web 0.0.0.0:8083->80/tcp", "web [::]:8083->80/tcp"}}
	r := Reconcile(cleanPublicMarker(), marker.SkewNone, true, facts)
	if r.Verdict != VerdictMatches || r.ExitCode() != 0 {
		t.Errorf("Reconcile(exposed docker ports) = %v exit %d, want Matches exit 0", r.Verdict, r.ExitCode())
	}
	out := r.String()
	for _, want := range []string{
		"  warning: docker publishes web 0.0.0.0:8083->80/tcp beyond loopback, around the firewall\n",
		"  warning: docker publishes web [::]:8083->80/tcp beyond loopback, around the firewall\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("String() = %q, want it to carry %q", out, want)
		}
	}
}

func TestReconcileSaysWhenDockerPortsCouldNotBeChecked(t *testing.T) {
	t.Parallel()
	facts := cleanPublicFacts()
	facts.Docker = DockerPorts{Unreadable: true}
	r := Reconcile(cleanPublicMarker(), marker.SkewNone, true, facts)
	if r.Verdict != VerdictMatches {
		t.Errorf("Verdict = %v, want Matches", r.Verdict)
	}
	if want := "  docker: could not check the published ports\n"; !strings.Contains(r.String(), want) {
		t.Errorf("String() = %q, want it to carry %q", r.String(), want)
	}
}

func TestReconcileSaysNothingOfDockerWhenNothingIsExposed(t *testing.T) {
	t.Parallel()
	if out := Reconcile(cleanPublicMarker(), marker.SkewNone, true, cleanPublicFacts()).String(); strings.Contains(out, "docker") {
		t.Errorf("String() = %q, want no mention of docker", out)
	}
}
