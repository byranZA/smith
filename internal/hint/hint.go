// Package hint renders the smith commands an on-box message suggests, so each
// one runs exactly as printed wherever the operator reads it: on the box itself,
// or on their own machine when the command was relayed there.
//
// It is the one place such a command is spelled. A message that formats a
// `smith …` string inline would name no box when relayed, and the command it
// suggests would run on the operator's machine instead of on the box.
package hint

import (
	"strings"

	"github.com/byranZA/smith/internal/connection"
)

// boxPlaceholder stands for a box the operator has yet to name.
const boxPlaceholder = "<box>"

// Invocation is the smith command an on-box message answers, as the operator
// sent it. Its zero value is a command typed on the box itself.
type Invocation struct {
	// Box is the box as the operator named it when the command was relayed —
	// a name from their inventory or a literal login@host — and empty when it
	// was typed on the box.
	Box string
	// Verb is the smith verb that ran — "session start" — empty when no verb
	// is known to have run.
	Verb string
	// Args are the arguments it ran with, the box left out.
	Args []string
}

// Command renders the smith command verb with args, as the operator is to type
// it: the box right after the verb when the command was relayed, and none when
// it was typed on the box. The box and every arg are literal values, quoted
// where a shell would otherwise read them as anything else.
func (i Invocation) Command(verb string, args ...string) string {
	return render(verb, literal(i.Box), words(args))
}

// Usage renders the smith command verb with usage, a line of flags and
// <placeholders> the operator fills in, which is written as given. The box is
// placed and quoted as Command places and quotes it.
func (i Invocation) Usage(verb, usage string) string {
	return render(verb, literal(i.Box), []string{usage})
}

// Rerun renders the command that ran as it reads naming a box the operator has
// yet to choose, which is the command one who ran it in the wrong place is
// pointed at.
func (i Invocation) Rerun() string {
	return render(i.Verb, boxPlaceholder, words(i.Args))
}

// render joins verb, box and the rendered words after it into one command
// line, leaving out a box that is empty.
func render(verb, box string, after []string) string {
	parts := []string{"smith", verb}
	if box != "" {
		parts = append(parts, box)
	}
	return strings.Join(append(parts, after...), " ")
}

// literal renders the box as a shell word, empty when there is none.
func literal(box string) string {
	if box == "" {
		return ""
	}
	return word(box)
}

// words renders each arg as a shell word.
func words(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = word(arg)
	}
	return out
}

// word renders s as one literal shell word: as written when no character in it
// means anything to a shell, and single-quoted otherwise.
func word(s string) string {
	if s != "" && !strings.ContainsFunc(s, special) {
		return s
	}
	return connection.ShellArg(s)
}

// special reports whether a shell reads r as anything but itself in an
// unquoted word.
func special(r rune) bool {
	return !strings.ContainsRune(plain, r)
}

// plain is every character a shell reads as itself in an unquoted word.
const plain = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@%+=:,./_-"
