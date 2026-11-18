package smithtool_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/smithtool"
)

// shortDir returns a fresh directory under /tmp, short enough for a socket path.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "st")
	if err != nil {
		t.Fatalf("make temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove temp dir: %v", err)
		}
	})
	return dir
}

// serve starts a server for tools in a prepared short dir and closes it with the test.
func serve(t *testing.T, onRefused func(string, error), tools ...smithtool.Tool) *smithtool.Server {
	t.Helper()
	dir := filepath.Join(shortDir(t), "tools")
	if err := smithtool.PrepareDir(dir); err != nil {
		t.Fatalf("PrepareDir(%q) = %v", dir, err)
	}
	s, err := smithtool.Serve(dir, smithtool.NewRegistry(tools...), onRefused)
	if err != nil {
		t.Fatalf("Serve = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close the tool server: %v", err)
		}
	})
	return s
}

// slowCall runs the slow tool with arg, failing the test when the call does.
func slowCall(t *testing.T, s *smithtool.Server, arg string) {
	t.Helper()
	if err := smithtool.Call(t.Context(), s.Path(), "slow", []string{arg}, io.Discard); err != nil {
		t.Errorf("Call(slow %s) = %v, want nil", arg, err)
	}
}

func TestCallStreamsOutputAndSucceeds(t *testing.T) {
	t.Parallel()
	echo := smithtool.Tool{Name: "echo", Summary: "echoes", Run: func(_ context.Context, args []string, out io.Writer) error {
		_, err := io.WriteString(out, "hello "+strings.Join(args, ",")+"\n")
		return err
	}}
	s := serve(t, nil, echo)
	var out bytes.Buffer
	if err := smithtool.Call(t.Context(), s.Path(), "echo", []string{"a", "b"}, &out); err != nil {
		t.Fatalf("Call = %v, want nil", err)
	}
	if got, want := out.String(), "hello a,b\n"; got != want {
		t.Errorf("Call output = %q, want %q", got, want)
	}
}

func TestCallOfFailingToolReturnsFailedErrorAfterItsOutput(t *testing.T) {
	t.Parallel()
	bad := smithtool.Tool{Name: "bad", Run: func(_ context.Context, _ []string, out io.Writer) error {
		if _, err := io.WriteString(out, "partial\n"); err != nil {
			return err
		}
		return errors.New("up exited 3")
	}}
	s := serve(t, nil, bad)
	var out bytes.Buffer
	err := smithtool.Call(t.Context(), s.Path(), "bad", nil, &out)
	var failed *smithtool.FailedError
	if !errors.As(err, &failed) || failed.Reason != "up exited 3" {
		t.Errorf("Call = %v, want FailedError %q", err, "up exited 3")
	}
	if got, want := out.String(), "partial\n"; got != want {
		t.Errorf("Call output = %q, want %q", got, want)
	}
}

func TestCallOfRefusingToolReturnsRefusedError(t *testing.T) {
	t.Parallel()
	no := smithtool.Tool{Name: "no", Run: func(context.Context, []string, io.Writer) error {
		return smithtool.Refuse("smith is not running a stack in this run")
	}}
	s := serve(t, nil, no)
	err := smithtool.Call(t.Context(), s.Path(), "no", nil, io.Discard)
	var refused *smithtool.RefusedError
	if !errors.As(err, &refused) || refused.Reason != "smith is not running a stack in this run" {
		t.Errorf("Call = %v, want RefusedError", err)
	}
}

func TestCallOfUnknownToolIsRefusedNamingTheAvailableOnes(t *testing.T) {
	t.Parallel()
	a := smithtool.Tool{Name: "alpha", Run: func(context.Context, []string, io.Writer) error { return nil }}
	b := smithtool.Tool{Name: "beta", Run: func(context.Context, []string, io.Writer) error { return nil }}
	s := serve(t, nil, a, b)
	err := smithtool.Call(t.Context(), s.Path(), "frobnicate", nil, io.Discard)
	var refused *smithtool.RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("Call = %v, want RefusedError", err)
	}
	want := `unknown smith tool "frobnicate"; available in this run: alpha, beta`
	if refused.Reason != want {
		t.Errorf("reason = %q, want %q", refused.Reason, want)
	}
}

func TestListReturnsNameAndSummaryOfEachTool(t *testing.T) {
	t.Parallel()
	a := smithtool.Tool{Name: "alpha", Summary: "does a", Run: func(context.Context, []string, io.Writer) error { return nil }}
	s := serve(t, nil, a)
	got, err := smithtool.List(t.Context(), s.Path())
	if err != nil {
		t.Fatalf("List = %v", err)
	}
	if want := []smithtool.Info{{Name: "alpha", Summary: "does a"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("List = %+v, want %+v", got, want)
	}
}

func TestListOfEmptyRegistryIsEmptyNotNil(t *testing.T) {
	t.Parallel()
	s := serve(t, nil)
	got, err := smithtool.List(t.Context(), s.Path())
	if err != nil {
		t.Fatalf("List = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("List = %#v, want empty non-nil", got)
	}
}

func TestCallWithNoSocketPathIsRefused(t *testing.T) {
	t.Parallel()
	err := smithtool.Call(t.Context(), "", "echo", nil, io.Discard)
	var refused *smithtool.RefusedError
	if !errors.As(err, &refused) || !strings.Contains(refused.Reason, "SMITH_TOOLS is not set") {
		t.Errorf("Call = %v, want a refusal naming SMITH_TOOLS", err)
	}
}

func TestCallOnSocketOfEndedRunIsRefused(t *testing.T) {
	t.Parallel()
	s := serve(t, nil)
	path := s.Path()
	if err := s.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	_, err := smithtool.List(t.Context(), path)
	var refused *smithtool.RefusedError
	if !errors.As(err, &refused) {
		t.Errorf("List = %v, want RefusedError", err)
	}
}

func TestCallsRunOneAtATime(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var events []string
	release := make(chan struct{})
	started := make(chan struct{})
	slow := smithtool.Tool{Name: "slow", Run: func(_ context.Context, args []string, _ io.Writer) error {
		mu.Lock()
		events = append(events, "start "+args[0])
		mu.Unlock()
		if args[0] == "1" {
			close(started)
			<-release
		}
		mu.Lock()
		events = append(events, "end "+args[0])
		mu.Unlock()
		return nil
	}}
	s := serve(t, nil, slow)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); slowCall(t, s, "1") }()
	<-started
	go func() { defer wg.Done(); slowCall(t, s, "2") }()
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	want := []string{"start 1", "end 1", "start 2", "end 2"}
	if !reflect.DeepEqual(events, want) {
		t.Errorf("events = %v, want %v", events, want)
	}
}

func TestCloseCancelsARunningCall(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	hang := smithtool.Tool{Name: "hang", Run: func(ctx context.Context, _ []string, _ io.Writer) error {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return ctx.Err()
	}}
	s := serve(t, nil, hang)
	done := make(chan error, 1)
	go func() { done <- smithtool.Call(t.Context(), s.Path(), "hang", nil, io.Discard) }()
	<-started
	if err := s.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel the running call")
	}
	if err := <-done; err == nil {
		t.Error("Call = nil, want an error once the loop closed")
	}
}

func TestPrepareDirMakesDirectoryPrivate(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(shortDir(t), "tools")
	if err := smithtool.PrepareDir(dir); err != nil {
		t.Fatalf("PrepareDir = %v", err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o700 {
		t.Errorf("tools directory mode = %o, want 700", got)
	}
}

func TestPrepareDirClearsStaleSocketsAndKeepsLiveOnes(t *testing.T) {
	t.Parallel()
	live := serve(t, nil)
	dir := filepath.Dir(live.Path())
	stale := filepath.Join(dir, "deadbeefdeadbeef.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := smithtool.PrepareDir(dir); err != nil {
		t.Fatalf("PrepareDir = %v", err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale socket stat = %v, want not exist", err)
	}
	if _, err := os.Stat(live.Path()); err != nil {
		t.Errorf("live socket stat = %v, want it kept", err)
	}
}

func TestPrepareDirRefusesPathTooLongForSocketBeforeMakingIt(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(shortDir(t), strings.Repeat("d", 120), "tools")
	err := smithtool.PrepareDir(dir)
	var tooLong *smithtool.TooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("PrepareDir = %v, want TooLongError", err)
	}
	if !strings.Contains(err.Error(), "too long for the tool socket") {
		t.Errorf("error = %q, want it to say the path is too long for the tool socket", err)
	}
	if _, serr := os.Stat(dir); !errors.Is(serr, os.ErrNotExist) {
		t.Errorf("directory stat = %v, want nothing made", serr)
	}
}

func TestServerTellsOnRefusedOfAnUnknownToolCall(t *testing.T) {
	t.Parallel()
	var got []string
	var mu sync.Mutex
	s := serve(t, func(name string, _ error) { mu.Lock(); got = append(got, name); mu.Unlock() })
	var refused *smithtool.RefusedError
	if err := smithtool.Call(t.Context(), s.Path(), "frobnicate", nil, io.Discard); !errors.As(err, &refused) {
		t.Fatalf("Call(frobnicate) = %v, want a RefusedError", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(got, []string{"frobnicate"}) {
		t.Errorf("onRefused saw %q, want [frobnicate]", got)
	}
}
