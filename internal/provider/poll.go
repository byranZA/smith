package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/byranZA/smith/internal/provider/extractpath"
)

// How long the address poll waits between attempts, and how long it waits in
// total. A provider that assigns an address asynchronously does it in seconds,
// so the cadence is short; the budget is long enough for a slow region and
// short enough that an operator is not left watching a silent command.
const (
	addressPollInterval = 5 * time.Second
	addressPollTimeout  = 2 * time.Minute
)

// Clock is the passage of time. It is passed in rather than reached for, so a
// test drives the address poll's retries and its timeout without waiting for
// either.
type Clock interface {
	// Now reports the current time.
	Now() time.Time
	// After returns a channel that receives once d has elapsed.
	After(d time.Duration) <-chan time.Time
}

// SystemClock returns the Clock backed by the real passage of time.
func SystemClock() Clock { return systemClock{} }

// systemClock is the real clock, wrapping the time package.
type systemClock struct{}

// Now reports the current time.
func (systemClock) Now() time.Time { return time.Now() }

// After returns a channel that receives once d has elapsed.
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// awaitAddress fills in the address of a box the provider created without one,
// by running the list template until the entry whose id matches carries one.
//
// smith branches on whether the extractor actually yielded an address, rather
// than on an adapter field declaring the provider synchronous or deferred.
// The declaration would be redundant with the response and would have a way to
// be wrong; branching on the response is one field fewer and corrects itself
// when a provider changes when it assigns addresses. A create that reported an
// address never reaches here, so it runs no list command at all.
//
// Giving up names the box id. The box exists and is being billed, and v1 ships
// no destroy, so the operator has to find it at the provider by hand: a box
// lost inside an error message is the worst outcome a create can produce.
func awaitAddress(ctx context.Context, runner Runner, clock Clock, adapter Adapter, box Box) (Box, error) {
	if len(adapter.List) == 0 {
		return Box{}, fmt.Errorf(
			"box %s was created but reported no address, and the adapter declares no list template to poll for one: find the box at the provider by that id",
			box.ID)
	}
	deadline := clock.Now().Add(addressPollTimeout)
	for {
		found, err := listed(ctx, runner, adapter, box.ID)
		if err != nil {
			return Box{}, err
		}
		if found.IP != "" {
			return found, nil
		}
		if !clock.Now().Before(deadline) {
			return Box{}, fmt.Errorf(
				"box %s was created but never reported an address within %s: find it at the provider by that id",
				box.ID, addressPollTimeout)
		}
		select {
		case <-ctx.Done():
			return Box{}, fmt.Errorf(
				"box %s was created but reported no address before the wait was interrupted: find it at the provider by that id: %w",
				box.ID, ctx.Err())
		case <-clock.After(addressPollInterval):
		}
	}
}

// listed runs the list template and returns the entry whose id matches, or the
// zero box when the account holds no such entry. Matching is by the id the
// create reported rather than by position, because a list is the whole account
// and the box smith just made is not reliably first in it.
//
// Everything that is not the matching entry is a reason to poll again rather
// than a reason to fail. An account the provider has not published the box into
// yet answers with an empty list, and another operator's private-only box has
// no address path at all; a create that gave up on either would lose a box that
// exists and is billed. The address is read from the matching entry alone, so a
// path that misses there — and only there — is still the adapter error naming
// the path.
func listed(ctx context.Context, runner Runner, adapter Adapter, id string) (Box, error) {
	doc, err := run(ctx, runner, render(adapter.List, nil))
	if err != nil {
		return Box{}, err
	}
	records := []any{doc}
	if adapter.Record.List != "" {
		records, err = extractpath.LookupAll(doc, adapter.Record.List)
		if err != nil {
			// The account holds no records to walk yet, which is a box still
			// to appear rather than an adapter to complain about.
			return Box{}, nil
		}
	}
	for _, record := range records {
		found, err := extractpath.Lookup(record, adapter.Extract.ID)
		if err != nil {
			// An entry without an id cannot be the box smith is waiting for,
			// and another entry still can be.
			continue
		}
		if extractpath.Text(found) != id {
			continue
		}
		ip, err := extractpath.Lookup(record, adapter.Extract.IP)
		if err != nil {
			return Box{}, fmt.Errorf("provider adapter's ip path: %w", err)
		}
		return Box{ID: id, IP: extractpath.Text(ip)}, nil
	}
	return Box{}, nil
}
