// PROTOTYPE for issue #115, throwaway: three layouts of the Discussion in
// the plan detail, printed as 80x24 frames with fake data. It answers
// "what should the Discussion look like?" and is not production code.
//
//	go run ./internal/tui/prototype_discussion          # all variants
//	go run ./internal/tui/prototype_discussion tab      # one of: tab, pane, markers
package main

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

const width, height = 80, 24

// The fake Discussion: two Threads, one with a reply chain, an open
// Question to the author, an answered one to the AI and a stale ref.
type msg struct {
	id, by, at, to, ref, status, body string
	depth                              int
}

var discussion = []msg{
	{id: "01JD..C4E", by: "Peter Horváth", at: "11-16 21:42", to: "jana@example.com", ref: "plan.md#AC-2", status: "open", body: "Does the webhook retry? AC-2 says one email."},
	{id: "01JD..C4F", by: "Jana Nováková", at: "11-16 21:50", ref: "", body: "Stripe retries for 3 days; we dedupe by event ID.", depth: 1},
	{id: "01JD..C50", by: "Peter Horváth", at: "11-16 22:01", body: "Then AC-2 should name the event ID.", depth: 2},
	{id: "01JD..C51", by: "Jana Nováková", at: "11-17 08:10", to: "ai", ref: "plan.md#affected-areas", status: "answered", body: "Which packages call payments.Charge today?"},
	{id: "01JD..C52", by: "Jana (through claude-code)", at: "11-17 08:11", body: "checkout/handler.go and billing/renew.go.", depth: 1},
	{id: "01JD..C53", by: "Ben Bitdiddle", at: "11-17 09:30", ref: "plan.md#rollout (stale)", body: "The rollout section is gone; was the flag dropped?"},
}

var planLines = []string{
	"## Context",
	"",
	"The checkout calls payments.Charge, which wraps the old provider.",
	"",
	"## Approach",
	"",
	"Add a Stripe client behind payments.Provider; switch EU traffic by flag.",
	"",
	"## Affected areas",
	"",
	"- payments/",
	"- checkout/handler.go",
	"",
	"## Acceptance criteria",
	"",
	"- AC-1: With an invalid card the API returns 402.",
	"- AC-2: A successful payment sends exactly one confirmation email.",
	"- AC-3: The feature flag can switch back without a deploy.",
}

func main() {
	variants := map[string]func() []string{"tab": tabVariant, "pane": paneVariant, "markers": markersVariant}
	order := []string{"tab", "pane", "markers"}
	if len(os.Args) > 1 {
		order = os.Args[1:]
	}
	for _, name := range order {
		v, ok := variants[name]
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown variant %q; use tab, pane or markers\n", name)
			os.Exit(1)
		}
		fmt.Printf("=== variant %s ===\n%s\n\n", name, frame(v()))
	}
}

// frame pads or cuts lines to 80x24, the size every screen must fit.
func frame(lines []string) string {
	out := make([]string, height)
	for i := range out {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = fit(l, width)
	}
	return strings.Join(out, "\n")
}

func fit(s string, w int) string {
	if utf8.RuneCountInString(s) > w {
		r := []rune(s)
		return string(r[:w-1]) + "…"
	}
	return s + strings.Repeat(" ", w-utf8.RuneCountInString(s))
}

func header(tab string) string {
	tabs := []string{"plan.md", "intent.md", "tasks", "discussion (2 open)"}
	for i, t := range tabs {
		if strings.HasPrefix(t, tab) {
			tabs[i] = "[" + t + "]"
		}
	}
	return "bfc-5v7x  Payments via Stripe   " + strings.Join(tabs, "  ")
}

const statusBar = " in-review · 1/2 approvals (peter) · jana@example.com          ✓ synced 12s ago "

// A. A discussion tab: the whole Discussion as one document, Threads with
// indented replies, the selected Message marked; keys act on it.
func tabVariant() []string {
	lines := []string{header("discussion"), ""}
	for i, m := range discussion {
		pad := strings.Repeat("  ", m.depth)
		mark := "  "
		if i == 0 {
			mark = "▶ "
		}
		meta := m.by + " · " + m.at
		if m.to != "" {
			meta += " · to " + m.to + " · " + m.status
		}
		if m.ref != "" {
			meta += " · " + m.ref
		}
		lines = append(lines, mark+pad+meta, "  "+pad+"  "+m.body)
		if i+1 < len(discussion) && discussion[i+1].depth == 0 {
			lines = append(lines, "")
		}
	}
	for len(lines) < height-2 {
		lines = append(lines, "")
	}
	return append(lines[:height-2], statusBar,
		"tab switch • ↑↓ select • r reply • n new Thread • q ask • esc back")
}

// B. A side pane: the document on the left, the Threads of the section in
// view on the right; Tab moves the focus between them.
func paneVariant() []string {
	const left = 44
	right := []string{"│ Threads here (2) ─────────────────", "│"}
	for _, m := range discussion[:3] {
		pad := strings.Repeat(" ", m.depth*2)
		right = append(right, "│ "+pad+m.by+" · "+m.at)
		for _, w := range wrap(m.body, 33-m.depth*2) {
			right = append(right, "│ "+pad+"  "+w)
		}
		if m.to != "" {
			right = append(right, "│ "+pad+"  to "+m.to+" · "+m.status)
		}
	}
	right = append(right, "│", "│ + 4 more Threads: d shows all")
	lines := []string{header("plan"), ""}
	for i := 0; i < height-4; i++ {
		l, r := "", "│"
		if i < len(planLines) {
			l = "  " + planLines[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if i == 16 {
			l = "▶ " + planLines[i][0:]
		}
		lines = append(lines, fit(l, left)+r)
	}
	return append(lines, statusBar,
		"tab focus • ↑↓ select • r reply • n new Thread • q ask • d all • esc back")
}

// C. Markers in the rendered document: a count after each heading or AC
// that has Threads; Enter on a marked line opens its Threads below the
// document, the rest of the screen keeps the document.
func markersVariant() []string {
	counts := map[string]string{"## Affected areas": "  ◆1 (answered)", "- AC-2: A successful payment sends exactly one confirmation email.": "  ◆1 open"}
	lines := []string{header("plan"), ""}
	for i, l := range planLines[:14] {
		mark := "  "
		if i == 16 {
			mark = "▶ "
		}
		lines = append(lines, mark+l+counts[l])
	}
	lines = append(lines, "  "+planLines[14], "▶ "+planLines[16]+counts[planLines[16]])
	lines = append(lines, "──── AC-2 · 1 Thread ──────────────────────────────────────────────────────────")
	m := discussion[0]
	lines = append(lines, "  "+m.by+" · "+m.at+" · to "+m.to+" · open", "    "+m.body,
		"    ↳ Jana Nováková: Stripe retries for 3 days; we dedupe by event ID. (+1)")
	for len(lines) < height-2 {
		lines = append(lines, "")
	}
	return append(lines[:height-2], statusBar,
		"↑↓ line • enter Threads • r reply • n new Thread here • q ask • D all • esc")
}

func wrap(s string, w int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > w {
			out = append(out, line)
			line = word
			continue
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	return append(out, line)
}
