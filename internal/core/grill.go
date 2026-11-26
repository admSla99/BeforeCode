package core

import (
	"slices"
	"strings"
)

// MaxGrillRounds is the number of rounds a Grill asks at most (ADR 0017).
const MaxGrillRounds = 3

// IsGrillQuestion reports whether m is a Question of the Grill of a plan
// whose author has this email: asked by an agent and addressed to the
// author (ADR 0012).
func IsGrillQuestion(m Message, author string) bool {
	return m.Agent != "" && author != "" && slices.ContainsFunc(m.To, func(to string) bool { return strings.EqualFold(to, author) })
}

// GrillQuestion is a Question of the Grill with its answer: the first
// direct reply by the author written without an agent, as bfc reply
// --verbatim and the TUI record it.
type GrillQuestion struct {
	Question Message
	Answer   Message
	Answered bool
}

// GrillRound is one round of a Grill: the Questions the agent asked
// together, in the order it asked them (ADR 0017).
type GrillRound []GrillQuestion

// Open returns the Questions of the round that are not answered.
func (r GrillRound) Open() []GrillQuestion {
	var open []GrillQuestion
	for _, q := range r {
		if !q.Answered {
			open = append(open, q)
		}
	}
	return open
}

// GrillRounds returns the Grill Questions among msgs, given in ULID order,
// grouped in rounds. A round is derived, not stored: a Question starts a new
// round when an answer to a Grill Question was written after the Grill
// Question before it, so every Question asked before the next answer is one
// round. A Question answered late stays in the round it was asked in.
// Messages that are not Grill Questions or their answers are ignored.
func GrillRounds(msgs []Message, author string) []GrillRound {
	var rounds []GrillRound
	answers := map[string]Message{} // by the Question's ULID
	for _, m := range msgs {
		if m.ReplyTo == "" || m.Agent != "" || !strings.EqualFold(m.Author.Email, author) {
			continue
		}
		if _, ok := answers[m.ReplyTo]; !ok {
			answers[m.ReplyTo] = m
		}
	}
	answered := false // an answer came after the last Grill Question
	for _, m := range msgs {
		if IsGrillQuestion(m, author) {
			if answered || len(rounds) == 0 {
				rounds = append(rounds, nil)
			}
			answered = false
			a, ok := answers[m.ID.String()]
			last := &rounds[len(rounds)-1]
			*last = append(*last, GrillQuestion{Question: m, Answer: a, Answered: ok})
			continue
		}
		if a, ok := answers[m.ReplyTo]; ok && a.ID == m.ID && isAsked(rounds, m.ReplyTo) {
			answered = true
		}
	}
	return rounds
}

// isAsked reports whether the Question with this ULID is in rounds.
func isAsked(rounds []GrillRound, id string) bool {
	for _, r := range rounds {
		for _, q := range r {
			if q.Question.ID.String() == id {
				return true
			}
		}
	}
	return false
}

// CurrentGrillRound returns the number of the round the Grill is in, from
// 1: the last round while it has an open Question, else the next one. A
// number above MaxGrillRounds means the limit is reached: no new round is
// asked.
func CurrentGrillRound(rounds []GrillRound) int {
	if n := len(rounds); n > 0 && len(rounds[n-1].Open()) > 0 {
		return n
	}
	return len(rounds) + 1
}
