package inventory

import (
	"fmt"
	"strings"
)

// smithUser is the login every box smith provisions is reached as after setup.
// It is only ever used to suggest an address, never to build one behind the
// operator's back: the resolution rule hands a bare value to ssh as written.
const smithUser = "smith"

// Resolve turns a value the operator typed into the ssh target smith connects
// over. It is the one rule every verb shares: the "@" decides.
//
// A value containing "@" is a literal ssh target and is never looked up, so a
// box smith has never seen is addressed exactly as it was written. A value
// without "@" is looked up in the inventory, and on a miss is handed back
// verbatim — ssh resolves it against ssh_config and DNS, so an unregistered
// alias keeps working with no smith code and no flag. An inventory name
// therefore shadows an identically-named ssh_config alias, which is what makes
// the name the operator chose the one that wins.
//
// It always returns a usable target, and never reaches a box or a file.
func Resolve(inv Inventory, arg string) string {
	if target, ok := Lookup(inv, arg); ok {
		return target
	}
	return arg
}

// Lookup returns the target arg names in the inventory and whether it named
// one. A value containing "@" is a literal target, so it is never looked up
// even when a box is registered under that exact name.
func Lookup(inv Inventory, arg string) (string, bool) {
	if literal(arg) {
		return "", false
	}
	box, ok := inv.Boxes[arg]
	if !ok {
		return "", false
	}
	return box.Target, true
}

// literal reports whether arg is an ssh target the operator wrote out in full.
// It is the "@" of "the \"@\" decides", written once so every half of the rule
// asks the same question.
func literal(arg string) bool { return strings.Contains(arg, "@") }

// ConnectHint returns what to tell an operator whose connection to arg failed,
// or "" when there is nothing worth saying — a literal target and a registered
// name both mean smith connected to what the operator asked for.
//
// A bare value smith did not recognise is the case the rule costs something:
// it went to ssh as written, so a bare address no longer implies the smith
// user. The hint names both fixes — the address that would have worked, and
// the command that registers the box so the name works from now on.
func ConnectHint(inv Inventory, arg string) string {
	if _, ok := Lookup(inv, arg); ok || literal(arg) {
		return ""
	}
	return fmt.Sprintf(`%s is not a registered box, so smith handed it to ssh as written.

  Try the address:  %s@%s
  Or register it:   smith machine add %s@%s
`, arg, smithUser, arg, smithUser, arg)
}
