// Package hint renders the smith commands an on-box message suggests, so each
// one runs exactly as printed wherever the operator reads it: on the box itself,
// or on their own machine when the command was relayed there.
//
// It is the one place such a command is spelled. A message that formats a
// `smith …` string inline would name no box when relayed, and the command it
// suggests would run on the operator's machine instead of on the box.
package hint

import "strings"

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

// Rerun renders the command that ran as it reads when it names box, which is
// the command an operator who ran it in the wrong place is pointed at.
func (i Invocation) Rerun(box string) string {
	i.Box = box
	return i.Command(i.Verb, i.Args...)
}

// Command renders the smith command verb with args, as the operator is to type
// it: the box right after the verb when the command was relayed, where every
// relayed verb takes it, and no box at all when it was typed on the box.
func (i Invocation) Command(verb string, args ...string) string {
	parts := []string{"smith", verb}
	if i.Box != "" {
		parts = append(parts, i.Box)
	}
	return strings.Join(append(parts, args...), " ")
}
