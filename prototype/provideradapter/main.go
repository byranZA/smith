package main

// PROTOTYPE — throwaway. Answers issue #60; not production code.
//
// Drives the full box lifecycle through ONE provider CLI using nothing but the
// three command templates + extractors from the research ticket (#54):
//
//	create -> id (+ maybe ip) -> poll list-by-id for ip -> poll port 22
//	       -> list-by-tag -> teardown-by-IP (forget the id, resolve it back)
//	       -> destroy -> list to confirm gone
//
// The contract claim under test is "command references, not SDKs": this file
// must contain ZERO provider-specific knowledge. Every provider fact lives in
// adapters/*.json. If a step here ever needs an `if adapter.Name == ...`, the
// contract has failed.

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Adapter struct {
	Name string `json:"name"`

	// CreateIP declares whether create returns a reachable IP ("synchronous")
	// or only an id ("deferred"). #54 called this the hardest gap; the harness
	// treats the deferred path as first-class, not a special case.
	CreateIP string `json:"createIP"`

	// TagArg is what create's {{tag}} placeholder expands to.
	// TagForm is what the tag path is expected to yield for that same box.
	// They differ where the provider stores tags as a key/value map.
	TagArg  string `json:"tagArg"`
	TagForm string `json:"tagForm"`

	Fields struct {
		ID  string `json:"id"`
		IP  string `json:"ip"`
		Tag string `json:"tag"`
	} `json:"fields"`

	Create  Command `json:"create"`
	List    Command `json:"list"`
	Destroy Command `json:"destroy"`

	Notes string `json:"notes"`
}

type Command struct {
	Argv   []string `json:"argv"`
	Record string   `json:"record"` // path from raw output to the box record(s)
}

// Box is the canonical record every provider normalizes to.
type Box struct {
	ID  string
	IP  string
	Tag string
}

func (b Box) String() string {
	ip := b.IP
	if ip == "" {
		ip = "<none yet>"
	}
	return fmt.Sprintf("id=%s ip=%s tag=%s", b.ID, ip, b.Tag)
}

var (
	adapterPath = flag.String("adapter", "prototype/provideradapter/adapters/doctl.json", "adapter file")
	runID       = flag.String("runid", "", "run id for the smith tag (default: proto<unix>)")
	image       = flag.String("image", "", "image reference")
	size        = flag.String("size", "", "server type/size reference")
	region      = flag.String("region", "", "region/location reference")
	sshKey      = flag.String("sshkey", "", "pre-registered SSH key reference")
	dryRun      = flag.Bool("dry-run", false, "print the commands, run nothing")
	offline     = flag.Bool("offline", false, "extractor check against fixtures/, no CLI, no spend")
	keep        = flag.Bool("keep", false, "skip destroy (leaves a paid box running)")
	raw         = flag.Bool("raw", false, "dump raw CLI JSON at every step")
	pollFor     = flag.Duration("poll", 3*time.Minute, "how long to poll for IP and for port 22")
)

// leaked is true between a successful create and a confirmed destroy.
var leaked bool

func main() {
	flag.Parse()
	if *runID == "" {
		*runID = fmt.Sprintf("proto%d", time.Now().Unix())
	}

	a, err := loadAdapter(*adapterPath)
	if err != nil {
		die(err)
	}
	fmt.Printf("adapter %s  (create IP: %s)\n", a.Name, a.CreateIP)

	if *offline {
		if err := extractorCheck(a); err != nil {
			die(err)
		}
		return
	}
	if *dryRun {
		showCommands(a)
		return
	}
	if err := lifecycle(a); err != nil {
		die(err)
	}
}

func lifecycle(a *Adapter) error {
	name := "smith-proto-" + *runID
	tag := expand(a.TagArg, map[string]string{"runid": *runID})
	wantTag := expand(a.TagForm, map[string]string{"runid": *runID})
	vars := map[string]string{
		"name": name, "tag": tag, "runid": *runID,
		"image": *image, "size": *size, "region": *region, "sshkey": *sshKey,
	}

	step(1, "create")
	// Always dump create's raw output: from here on a real, billed box exists,
	// and if an extractor path is wrong this is the only record of its id.
	wasRaw := *raw
	*raw = true
	created, err := a.records(a.Create, vars)
	*raw = wasRaw
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	// Everything past this point may have left a running box behind.
	defer func() {
		if leaked {
			fmt.Fprintf(os.Stderr, "\n!! A BILLED BOX MAY STILL BE RUNNING. Check the raw create output above,\n"+
				"   then: %s\n", strings.Join(a.Destroy.Argv, " "))
		}
	}()
	leaked = true
	if len(created) != 1 {
		return fmt.Errorf("create returned %d records, want 1 (record path %q wrong?)", len(created), a.Create.Record)
	}
	box := created[0]
	fmt.Printf("  -> %s\n", box)
	if box.ID == "" {
		return fmt.Errorf("create yielded no id — id is the MUST-extract field")
	}

	// The id is the only thing smith is allowed to persist. Everything after
	// this point either re-derives the IP or resolves it back to an id.
	createdID := box.ID

	step(2, "resolve IP")
	switch {
	case box.IP != "":
		fmt.Printf("  create was IP-synchronous: %s\n", box.IP)
		if a.CreateIP != "synchronous" {
			fmt.Printf("  !! adapter declares %q but an IP arrived at create\n", a.CreateIP)
		}
	default:
		fmt.Printf("  create was IP-deferred; polling list for id=%s\n", createdID)
		box, err = a.pollFor(vars, func(b Box) bool { return b.ID == createdID && b.IP != "" })
		if err != nil {
			return fmt.Errorf("resolve IP: %w", err)
		}
		fmt.Printf("  -> %s\n", box)
		if a.CreateIP == "synchronous" {
			fmt.Printf("  !! adapter declares synchronous but the IP was deferred\n")
		}
	}

	step(3, "wait for sshd on :22 (smith's own poll — no CLI waits for this)")
	banner, waited, err := waitSSH(box.IP, *pollFor)
	if err != nil {
		return fmt.Errorf("port 22: %w", err)
	}
	fmt.Printf("  -> reachable after %s: %s\n", waited.Round(time.Second), banner)

	step(4, "list by tag (client-side filter on the JSON)")
	all, err := a.records(a.List, vars)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	tagged := filterTag(all, wantTag)
	fmt.Printf("  %d box(es) from the provider, %d carrying tag %q\n", len(all), len(tagged), wantTag)
	for _, b := range tagged {
		fmt.Printf("    %s\n", b)
	}
	if len(tagged) != 1 {
		return fmt.Errorf("tag filter matched %d, want 1 — tag path %q or tagForm %q wrong", len(tagged), a.Fields.Tag, a.TagForm)
	}

	step(5, "teardown-by-IP: forget the id, resolve it back from the IP alone")
	onlyIP := box.IP
	resolved := ""
	for _, b := range all {
		if b.IP == onlyIP {
			resolved = b.ID
		}
	}
	if resolved == "" {
		return fmt.Errorf("could not resolve IP %s back to an id via list", onlyIP)
	}
	fmt.Printf("  ip %s -> id %s (matches create-time id: %t)\n", onlyIP, resolved, resolved == createdID)

	if *keep {
		leaked = false
		fmt.Printf("\n-keep set: box %s (%s) LEFT RUNNING and billing. Destroy it with:\n  %s\n",
			resolved, onlyIP, strings.Join(expandAll(a.Destroy.Argv, vars), " "))
		return nil
	}

	step(6, "destroy by resolved id")
	vars["id"] = resolved
	if _, err := a.run(a.Destroy, vars); err != nil {
		return fmt.Errorf("destroy: %w", err)
	}
	leaked = false
	fmt.Printf("  destroyed %s\n", resolved)

	step(7, "list again to confirm it is gone")
	after, err := a.records(a.List, vars)
	if err != nil {
		return fmt.Errorf("list after destroy: %w", err)
	}
	if left := filterTag(after, wantTag); len(left) != 0 {
		fmt.Printf("  !! %d box(es) still carry the tag (provider delete may be async)\n", len(left))
	} else {
		fmt.Printf("  -> no box carries tag %q\n", wantTag)
	}
	return nil
}

// records runs a command and normalizes its JSON to canonical Box records.
func (a *Adapter) records(c Command, vars map[string]string) ([]Box, error) {
	out, err := a.run(c, vars)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("output was not JSON: %w\n%s", err, truncate(out))
	}
	var boxes []Box
	for _, r := range eval(doc, c.Record) {
		boxes = append(boxes, Box{
			ID:  evalString(r, a.Fields.ID),
			IP:  evalString(r, a.Fields.IP),
			Tag: strings.Join(evalStrings(r, a.Fields.Tag), ","),
		})
	}
	return boxes, nil
}

func (a *Adapter) run(c Command, vars map[string]string) ([]byte, error) {
	argv := expandAll(c.Argv, vars)
	fmt.Printf("  $ %s\n", strings.Join(argv, " "))
	cmd := exec.Command(argv[0], argv[1:]...) //nolint // prototype: operator-supplied argv is the point
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if *raw {
		fmt.Printf("  raw: %s\n", truncate(out))
	}
	return out, err
}

// pollFor re-runs list until a record satisfies want, or the budget runs out.
func (a *Adapter) pollFor(vars map[string]string, want func(Box) bool) (Box, error) {
	deadline := time.Now().Add(*pollFor)
	for attempt := 1; time.Now().Before(deadline); attempt++ {
		boxes, err := a.records(a.List, vars)
		if err != nil {
			return Box{}, err
		}
		for _, b := range boxes {
			if want(b) {
				return b, nil
			}
		}
		fmt.Printf("  attempt %d: not ready, sleeping 5s\n", attempt)
		time.Sleep(5 * time.Second)
	}
	return Box{}, fmt.Errorf("gave up after %s", *pollFor)
}

// waitSSH is smith's own readiness poll. It reads the banner rather than just
// dialing, because an open port is not yet a usable sshd.
func waitSSH(ip string, budget time.Duration) (string, time.Duration, error) {
	start := time.Now()
	deadline := start.Add(budget)
	for attempt := 1; time.Now().Before(deadline); attempt++ {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "22"), 5*time.Second)
		if err == nil {
			buf := make([]byte, 256)
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, rerr := conn.Read(buf)
			conn.Close()
			if rerr == nil && n > 0 {
				return strings.TrimSpace(string(buf[:n])), time.Since(start), nil
			}
			fmt.Printf("  attempt %d: port open, no banner yet\n", attempt)
		} else {
			fmt.Printf("  attempt %d: %v\n", attempt, err)
		}
		time.Sleep(5 * time.Second)
	}
	return "", time.Since(start), fmt.Errorf("no sshd banner within %s", budget)
}

func filterTag(boxes []Box, want string) []Box {
	var out []Box
	for _, b := range boxes {
		for _, t := range strings.Split(b.Tag, ",") {
			if t == want {
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// extractorCheck validates the extractor paths against recorded fixtures.
// It costs nothing and needs no token, but it only proves the paths parse the
// shapes we BELIEVE the CLIs emit — the live run is what proves the shapes.
func extractorCheck(a *Adapter) error {
	fmt.Printf("OFFLINE extractor check — fixtures only, proves nothing about the real CLI\n")
	for _, f := range []struct {
		verb string
		cmd  Command
	}{{"create", a.Create}, {"list", a.List}} {
		path := fmt.Sprintf("prototype/provideradapter/fixtures/%s-%s.json", a.Name, f.verb)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		fmt.Printf("\n%s (%s)\n", f.verb, path)
		recs := eval(doc, f.cmd.Record)
		if len(recs) == 0 {
			return fmt.Errorf("record path %q matched nothing in %s", f.cmd.Record, path)
		}
		for _, r := range recs {
			fmt.Printf("  %s\n", Box{
				ID:  evalString(r, a.Fields.ID),
				IP:  evalString(r, a.Fields.IP),
				Tag: strings.Join(evalStrings(r, a.Fields.Tag), ","),
			})
		}
	}
	return nil
}

// showCommands prints the three templates fully expanded. Nothing runs, nothing
// is billed — this is the "read it before you spend money" mode.
func showCommands(a *Adapter) {
	vars := map[string]string{
		"name": "smith-proto-" + *runID, "runid": *runID,
		"tag":   expand(a.TagArg, map[string]string{"runid": *runID}),
		"image": *image, "size": *size, "region": *region, "sshkey": *sshKey,
		"id": "<id-from-create>",
	}
	fmt.Printf("DRY RUN — nothing executes.\n")
	for _, c := range []struct {
		verb string
		cmd  Command
	}{{"create", a.Create}, {"list", a.List}, {"destroy", a.Destroy}} {
		fmt.Printf("\n%s\n  $ %s\n", c.verb, strings.Join(expandAll(c.cmd.Argv, vars), " "))
	}
	fmt.Printf("\nexpected tag on the box: %q\n", expand(a.TagForm, map[string]string{"runid": *runID}))
}

func loadAdapter(path string) (*Adapter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	a := &Adapter{}
	if err := json.Unmarshal(data, a); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return a, nil
}

func expandAll(argv []string, vars map[string]string) []string {
	out := make([]string, len(argv))
	for i, s := range argv {
		out[i] = expand(s, vars)
	}
	return out
}

func expand(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}

func step(n int, what string) { fmt.Printf("\n[%d] %s\n", n, what) }

func truncate(b []byte) string {
	if len(b) > 1200 {
		return string(b[:1200]) + "…"
	}
	return string(b)
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "\nFAILED: %v\n", err)
	os.Exit(1)
}
