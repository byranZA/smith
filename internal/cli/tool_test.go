package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/smithtool"
)

// runTool runs `smith tool args` as an agent whose SMITH_TOOLS is socket, and returns its stdout, stderr and exit code.
func runTool(t *testing.T, socket string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newToolCmd(func(string) string { return socket })
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	var exit *exitError
	if errors.As(err, &exit) {
		code = exit.code
	} else if err != nil {
		t.Fatalf("tool %v: %v", args, err)
	}
	return out.String(), errb.String(), code
}

// serveTools serves tools on a fresh socket for the test and returns its path.
func serveTools(t *testing.T, tools ...smithtool.Tool) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "ct")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove temp dir: %v", err)
		}
	})
	dir := filepath.Join(root, "tools")
	if err := smithtool.PrepareDir(dir); err != nil {
		t.Fatal(err)
	}
	s, err := smithtool.Serve(dir, smithtool.NewRegistry(tools...), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close the tool server: %v", err)
		}
	})
	return s.Path()
}

func TestToolListPrintsEachToolOnOneLine(t *testing.T) {
	socket := serveTools(t, smithtool.Tool{Name: "stack-up", Summary: "re-run the stack's up", Run: func(context.Context, []string, io.Writer) error { return nil }})

	stdout, _, code := runTool(t, socket, "list")

	if want := "stack-up  re-run the stack's up\n"; stdout != want || code != 0 {
		t.Errorf("tool list = %q, exit %d; want %q, exit 0", stdout, code, want)
	}
}

func TestToolListJSONOfNoToolsIsAnEmptyList(t *testing.T) {
	stdout, _, code := runTool(t, serveTools(t), "list", "--json")

	if want := `{"schema_version":1,"tools":[]}` + "\n"; stdout != want || code != 0 {
		t.Errorf("tool list --json = %q, exit %d; want %q, exit 0", stdout, code, want)
	}
}

func TestToolListJSONNamesEachToolAndItsSummary(t *testing.T) {
	socket := serveTools(t, smithtool.Tool{Name: "stack-up", Summary: "re-run up", Run: func(context.Context, []string, io.Writer) error { return nil }})

	stdout, _, _ := runTool(t, socket, "list", "--json")

	if want := `{"schema_version":1,"tools":[{"name":"stack-up","summary":"re-run up"}]}` + "\n"; stdout != want {
		t.Errorf("tool list --json = %q, want %q", stdout, want)
	}
}

func TestToolWithNoSmithToolsExitsTwoSayingNoLoopIsRunning(t *testing.T) {
	for _, args := range [][]string{{"stack-up"}, {"list"}} {
		_, stderr, code := runTool(t, "", args...)
		if code != 2 || !strings.Contains(stderr, "smith is not running a loop for this shell") {
			t.Errorf("tool %v with no SMITH_TOOLS = exit %d, stderr %q; want exit 2 saying no loop is running", args, code, stderr)
		}
	}
}

func TestToolOnADeadSocketExitsTwo(t *testing.T) {
	_, stderr, code := runTool(t, filepath.Join(t.TempDir(), "gone.sock"), "list")

	if code != 2 || !strings.Contains(stderr, "smith is not running a loop for this shell") {
		t.Errorf("tool list on a dead socket = exit %d, stderr %q; want exit 2", code, stderr)
	}
}

func TestToolUnknownExitsTwoNamingTheAvailableTools(t *testing.T) {
	socket := serveTools(t, smithtool.Tool{Name: "stack-up", Run: func(context.Context, []string, io.Writer) error { return nil }})

	_, stderr, code := runTool(t, socket, "frobnicate")

	if want := `unknown smith tool "frobnicate"; available in this run: stack-up`; code != 2 || !strings.Contains(stderr, want) {
		t.Errorf("tool frobnicate = exit %d, stderr %q; want exit 2 with %q", code, stderr, want)
	}
}

func TestToolStreamsOutputAndExitsZeroOnSuccess(t *testing.T) {
	socket := serveTools(t, smithtool.Tool{Name: "say", Run: func(_ context.Context, args []string, out io.Writer) error {
		_, err := io.WriteString(out, strings.Join(args, " ")+"\n")
		return err
	}})

	stdout, _, code := runTool(t, socket, "say", "--loud", "hi")

	if stdout != "--loud hi\n" || code != 0 {
		t.Errorf("tool say --loud hi = %q, exit %d; want %q, exit 0", stdout, code, "--loud hi\n")
	}
}

func TestToolThatFailsExitsOne(t *testing.T) {
	socket := serveTools(t, smithtool.Tool{Name: "bad", Run: func(context.Context, []string, io.Writer) error { return errors.New("up exited 3") }})

	_, stderr, code := runTool(t, socket, "bad")

	if code != 1 || !strings.Contains(stderr, "up exited 3") {
		t.Errorf("tool bad = exit %d, stderr %q; want exit 1 saying why", code, stderr)
	}
}
