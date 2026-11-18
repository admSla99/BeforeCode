package tui

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/charmbracelet/x/vt"
)

// vscreen is the terminal the tests read: the renderer's output goes in,
// the text on the screen comes out. The renderer redraws only the cells
// that changed, so a word on the screen is often not a contiguous string
// in the output; the screen puts the cells together again.
type vscreen struct {
	term *vt.Emulator
	// onlcr makes a newline also return the cursor to the first column,
	// as the terminal driver does with ONLCR.
	onlcr bool
}

func newVScreen(t *testing.T, w, h int) *vscreen {
	t.Helper()
	term := vt.NewEmulator(w, h)
	// The emulator writes its answers to the program's queries into a pipe;
	// read them, or the next write blocks. Closing the pipe stops the reader.
	in, ok := term.InputPipe().(io.Closer)
	if !ok {
		t.Fatal("vscreen: the emulator's input pipe cannot be closed")
	}
	go func() { _, _ = io.Copy(io.Discard, term) }()
	t.Cleanup(func() { _ = in.Close() })
	// Bubble Tea leaves the newline mapping to the terminal driver outside
	// Windows, when it has no tty input (tea.go, mapNl). teatest has none.
	return &vscreen{term: term, onlcr: runtime.GOOS != "windows"}
}

func (s *vscreen) Write(p []byte) (int, error) {
	if s.onlcr {
		if _, err := s.term.Write(bytes.ReplaceAll(p, []byte("\n"), []byte("\r\n"))); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return s.term.Write(p)
}

// String returns the text on the screen, one line per row.
func (s *vscreen) String() string { return s.term.String() }

// Contains reports whether the screen shows want on one of its rows.
func (s *vscreen) Contains(want string) bool { return strings.Contains(s.String(), want) }

// screens holds the screen of each running test model. tm.Output() hands
// out each byte once, so the screen has to outlive a single waitFor.
var screens sync.Map // *teatest.TestModel → *vscreen

// newTestModel runs m in a w x h terminal that waitFor can read. With w and
// h zero the program gets no size, as before the terminal reports one, and
// the screen is 80x24.
func newTestModel(t *testing.T, m tea.Model, w, h int) *teatest.TestModel {
	t.Helper()
	var opts []teatest.TestOption
	if w > 0 && h > 0 {
		opts = append(opts, teatest.WithInitialTermSize(w, h))
	} else {
		w, h = 80, 24
	}
	tm := teatest.NewTestModel(t, m, opts...)
	screens.Store(tm, newVScreen(t, w, h))
	t.Cleanup(func() { screens.Delete(tm) })
	return tm
}

// waitFor waits until the screen shows want. It reads the screen, not the
// output stream, so it also finds text the renderer did not redraw.
func waitFor(t *testing.T, tm *teatest.TestModel, want string) {
	t.Helper()
	v, ok := screens.Load(tm)
	if !ok {
		t.Fatal("waitFor: start the model with newTestModel")
	}
	s := v.(*vscreen)
	deadline := time.Now().Add(3 * time.Second)
	for first := true; ; first = false {
		out, err := io.ReadAll(tm.Output())
		if err != nil {
			t.Fatalf("waitFor: %v", err)
		}
		// Without new output the screen is unchanged since the last check.
		if first || len(out) > 0 {
			_, _ = s.Write(out)
			if s.Contains(want) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitFor: %q not on the screen after 3s. Screen:\n%s", want, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// #73 AC-3: the renderer redraws only changed cells, so it writes "don",
// moves the cursor over the "e" the previous frame left in that cell and
// clears the rest of the line. The screen still shows the whole word.
func TestVScreenFindsTextTheRendererDidNotRedraw(t *testing.T) {
	s := newVScreen(t, 80, 24)
	// Column 20 holds the "e" of "enter", as does "done" in the next frame.
	_, _ = s.Write([]byte("\x1b[1;1HReason: Split AC-1  enter: request changes"))
	_, _ = s.Write([]byte("\rrequest-changes: don\x1b[C\x1b[K"))
	if !s.Contains("request-changes: done") {
		t.Error("the screen does not show request-changes: done")
	}
	if s.Contains("enter: request changes") {
		t.Error("the screen still shows the cleared rest of the line")
	}
}

// #73: outside Windows the renderer leaves "\n" to the terminal driver,
// which also returns the cursor to the first column (ONLCR).
func TestVScreenMapsNewlinesLikeATerminal(t *testing.T) {
	s := newVScreen(t, 80, 24)
	s.onlcr = true
	_, _ = s.Write([]byte("IN-REVIEW (2)\nPayments via Stripe"))
	if rows := strings.Split(s.String(), "\n"); rows[1] != "Payments via Stripe" {
		t.Errorf("second row = %q, want it in the first column", rows[1])
	}
}
