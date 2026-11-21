// Package claude runs Claude Code headless (claude -p) and turns its
// stream-json output into agent Events.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/admSla99/BeforeCode/internal/agent"
)

// Name is the name of this harness, as bfc writes it.
const Name = "claude-code"

// Options configure the adapter.
type Options struct {
	// Command starts Claude Code: claude.command from the config.
	Command string
	// Env is added to the environment of the process.
	Env []string
}

// Agent runs Claude Code.
type Agent struct{ opts Options }

var _ agent.Agent = Agent{}

// New returns an Agent that starts opts.Command.
func New(opts Options) Agent { return Agent{opts: opts} }

// Name implements agent.Agent.
func (Agent) Name() string { return Name }

// Capabilities implements agent.Agent.
func (Agent) Capabilities() agent.Caps {
	return agent.Caps{Hooks: true, Resume: true, Headless: true}
}

// Run implements agent.Agent. It starts
//
//	<command> -p --output-format stream-json --verbose [--resume <session>] [--allowedTools <tool>...] -- <prompt>
//
// in t.Dir and sends one event for each assistant text, tool call and
// result; lines of other types are ignored. A result that reports an
// error, a line that is not JSON, an assistant or result line it cannot
// read and a non-zero exit each become an error event; so does a cancelled
// ctx that stopped the process. After cancelling, a caller may stop reading: Run
// then drops the remaining events and its goroutines end.
func (a Agent) Run(ctx context.Context, t agent.Task) (<-chan agent.Event, error) {
	cmd := exec.CommandContext(ctx, a.opts.Command, args(t)...)
	cmd.Dir = t.Dir
	cmd.Env = append(os.Environ(), a.opts.Env...)
	// A process that ignores the kill, or a child that keeps its output
	// open, must not hang Run.
	cmd.WaitDelay = waitDelay
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	stderr := &headWriter{max: maxStderr}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	waited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = pw.Close()
		waited <- err
	}()

	ch := make(chan agent.Event)
	go func() {
		defer close(ch)
		// send delivers e unless the caller cancelled and stopped reading.
		send := func(e agent.Event) {
			select {
			case ch <- e:
			case <-ctx.Done():
			}
		}
		failed := false
		r := bufio.NewReader(pr)
		for {
			// Keep reading after a cancel, so that the process's output
			// never blocks.
			data, readErr := r.ReadBytes('\n')
			if data = bytes.TrimSpace(data); len(data) > 0 {
				for _, e := range parse(data) {
					failed = failed || e.Kind == agent.Error
					send(e)
				}
			}
			if readErr != nil {
				break
			}
		}
		err := <-waited
		var last agent.Event
		switch {
		case err != nil && ctx.Err() != nil:
			last = errorEvent(fmt.Errorf("claude was stopped: %w", ctx.Err()), "")
		case err != nil && !failed:
			last = errorEvent(exitError(err, stderr.String()), "")
		default:
			return
		}
		// The context may be done already: wait a little for a caller
		// that still reads, but not for one that left.
		timer := time.NewTimer(waitDelay)
		defer timer.Stop()
		select {
		case ch <- last:
		case <-timer.C:
		}
	}()
	return ch, nil
}

// maxStderr is how much of the standard error of claude Run keeps for an
// error event.
const maxStderr = 4096

// headWriter keeps the first max bytes written to it.
type headWriter struct {
	max int
	buf bytes.Buffer
}

func (w *headWriter) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (w *headWriter) String() string { return w.buf.String() }

// waitDelay is how long Run waits for the output to close after the
// process was stopped or exited.
const waitDelay = 5 * time.Second

// exitError describes a failed claude process with its standard error.
func exitError(err error, stderr string) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		err = fmt.Errorf("claude exited with status %d", exit.ExitCode())
	} else {
		err = fmt.Errorf("claude failed: %w", err)
	}
	if msg := strings.TrimSpace(stderr); msg != "" {
		return fmt.Errorf("%w: %s", err, msg)
	}
	return err
}

// errorEvent returns an Error Event for err.
func errorEvent(err error, session string) agent.Event {
	return agent.Event{Kind: agent.Error, Text: err.Error(), Err: err, Session: session}
}

// args returns the arguments of claude for t.
func args(t agent.Task) []string {
	a := []string{"-p", "--output-format", "stream-json", "--verbose"}
	if t.Session != "" {
		a = append(a, "--resume", t.Session)
	}
	if len(t.AllowedTools) > 0 {
		a = append(a, "--allowedTools")
		a = append(a, t.AllowedTools...)
	}
	// After --, a prompt that starts with - is not read as a flag.
	return append(a, "--", t.Prompt)
}

// line is the part of a stream-json line that bfc reads.
type line struct {
	SessionID string `json:"session_id"`
	Message   struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
	Result  string   `json:"result"`
	IsError bool     `json:"is_error"`
	Errors  []string `json:"errors"`
}

// maxQuoted is how much of a line an Error quotes.
const maxQuoted = 200

// quoted is the start of a line for an Error.
func quoted(data []byte) []byte {
	if len(data) > maxQuoted {
		return data[:maxQuoted]
	}
	return data
}

// parse turns one stream-json line into Events. It reads only the type of
// a line first: a line of a type bfc does not use is ignored whatever its
// other fields hold, and so is JSON that is not an object with a string
// type (#93).
func parse(data []byte) []agent.Event {
	if !json.Valid(data) {
		return []agent.Event{errorEvent(fmt.Errorf("claude printed a line that is not JSON: %q", quoted(data)), "")}
	}
	var head struct {
		Type json.RawMessage `json:"type"`
	}
	var lineType string
	if json.Unmarshal(data, &head) != nil || json.Unmarshal(head.Type, &lineType) != nil {
		return nil
	}
	switch lineType {
	case "assistant", "result":
	default:
		return nil
	}
	var l line
	if err := json.Unmarshal(data, &l); err != nil {
		return []agent.Event{errorEvent(fmt.Errorf("claude printed a %q line that bfc cannot read: %v: %q", lineType, err, quoted(data)), "")}
	}
	var events []agent.Event
	switch lineType {
	case "assistant":
		for _, c := range l.Message.Content {
			switch c.Type {
			case "text":
				events = append(events, agent.Event{Kind: agent.Text, Text: c.Text, Session: l.SessionID})
			case "tool_use":
				events = append(events, agent.Event{Kind: agent.ToolUse, Tool: c.Name, ToolID: c.ID, Input: c.Input, Session: l.SessionID})
			}
		}
	case "result":
		if !l.IsError {
			events = append(events, agent.Event{Kind: agent.Result, Text: l.Result, Session: l.SessionID})
			break
		}
		msg := strings.Join(l.Errors, "; ")
		if msg == "" {
			msg = l.Result
		}
		if msg == "" {
			msg = "claude reported an error"
		}
		events = append(events, errorEvent(errors.New(msg), l.SessionID))
	}
	return events
}
