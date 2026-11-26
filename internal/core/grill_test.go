package core

import (
	"fmt"
	"testing"
)

// roundIDs is the rounds as the last byte of each Question's ULID, with a
// "+" after an answered one.
func roundIDs(rounds []GrillRound) string {
	var out []string
	for _, r := range rounds {
		s := ""
		for _, q := range r {
			s += fmt.Sprint(q.Question.ID[15])
			if q.Answered {
				s += "+"
			}
			s += " "
		}
		out = append(out, s)
	}
	return fmt.Sprint(out)
}

// bfc-121s AC-11: rounds are derived from the order of Grill Questions and
// their answers.
func TestGrillRounds(t *testing.T) {
	ask := func(n byte) Message { return msg(n, ada, "claude-code", []string{"ADA@example.com"}, 0) }
	answer := func(n, q byte) Message { return msg(n, ada, "", nil, q) }
	for _, c := range []struct {
		name    string
		msgs    []Message
		want    string
		current int
	}{
		{"no Grill yet", nil, "[]", 1},
		{"one round asked", []Message{ask(1), ask(2), ask(3)}, "[1 2 3 ]", 1},
		{
			"three answered, then two more",
			[]Message{ask(1), ask(2), ask(3), answer(4, 1), answer(5, 2), answer(6, 3), ask(7), ask(8)},
			"[1+ 2+ 3+  7 8 ]", 2,
		},
		{
			"a late answer stays in its round",
			[]Message{ask(1), ask(2), answer(3, 1), ask(4), answer(5, 2)},
			"[1+ 2+  4 ]", 2,
		},
		{
			"other Messages are ignored",
			[]Message{
				ask(1),
				msg(2, ben, "", []string{"ada@example.com"}, 0), // a person's Question
				msg(3, ada, "claude-code", []string{"ai"}, 0),   // to the AI, not the author
				msg(4, ben, "", nil, 1),                         // discussion, not the answer
				msg(5, ada, "claude-code", nil, 1),              // the agent itself
				ask(6),
				answer(7, 2), // answers a Question outside the Grill
				ask(8),
			},
			"[1 6 8 ]", 1,
		},
		{
			"every round answered",
			[]Message{ask(1), answer(2, 1), ask(3), answer(4, 3), ask(5), answer(6, 5)},
			"[1+  3+  5+ ]", 4,
		},
		{
			"a second answer does not start a round",
			[]Message{ask(1), answer(2, 1), ask(3), answer(4, 1), ask(5)},
			"[1+  3 5 ]", 2,
		},
	} {
		rounds := GrillRounds(c.msgs, "ada@example.com")
		if got := roundIDs(rounds); got != c.want {
			t.Errorf("%s: rounds %s, want %s", c.name, got, c.want)
		}
		if got := CurrentGrillRound(rounds); got != c.current {
			t.Errorf("%s: current round %d, want %d", c.name, got, c.current)
		}
	}
}
