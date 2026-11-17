package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	beforecode "github.com/admSla99/BeforeCode"
	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// intentTemplate is the path of the intent template, relative to the
// repository root and inside the embedded defaults.
const intentTemplate = ".beforecode/templates/intent.md"

func newNewCmd() *cobra.Command {
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a plan with an intent",
		Long: `Create a plan: intent.md from the template with the title filled in, and a
created event, in one commit on the plans ref. The editor git uses opens on
intent.md first, unless --no-edit is given; saving it unchanged or empty
creates no plan.

The plan's Short ID and title are printed on the first line.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(cmd, strings.Join(args, " "), noEdit)
		},
	}
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "do not open an editor; use the template with the title")
	return cmd
}

func runNew(cmd *cobra.Command, title string, noEdit bool) error {
	ctx := cmd.Context()
	s, err := openPlans(ctx)
	if err != nil {
		return err
	}
	name, email, err := s.Author(ctx)
	if err != nil {
		return err
	}
	tpl, err := readTemplate(s.Root(), intentTemplate)
	if err != nil {
		return err
	}
	intent := core.FillIntentTemplate(tpl, title)

	kept := "" // the edited file, kept when its content cannot be used
	if !noEdit {
		edited, path, err := editText(cmd, s, "intent-*.md", intent)
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(edited)) == 0 || bytes.Equal(edited, intent) {
			_ = os.Remove(path)
			return errors.New("intent.md was not changed or is empty; no plan created")
		}
		// Stored files use LF (ADR 0002); editors on Windows may save CRLF.
		intent, kept = bytes.ReplaceAll(edited, []byte("\r\n"), []byte("\n")), path
	}
	// From here on, an error must not lose the user's edited text.
	keep := func(err error) error {
		if kept != "" {
			return fmt.Errorf("%w; no plan created\nYour text is kept in %s", err, kept)
		}
		return fmt.Errorf("%w; no plan created", err)
	}

	existing, err := planIDs(cmd, s)
	if err != nil {
		return keep(err)
	}
	now := time.Now()
	entropy := ulid.Monotonic(rand.Reader, 0)
	newID := func() ulid.ULID { return ulid.MustNew(ulid.Timestamp(now), entropy) }
	plan, files, err := core.NewPlan(intent, core.Person{Name: name, Email: email}, now, newID, existing)
	if err != nil {
		return keep(err)
	}
	if _, err := s.Write(ctx, "bfc new "+plan.ShortID+": "+plan.Title, files); err != nil {
		return keep(err)
	}
	if kept != "" {
		_ = os.Remove(kept)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", plan.ShortID, plan.Title)
	return err
}

// readTemplate returns the template at rel in the repository's working
// tree, or the default built into bfc when the repository has none.
func readTemplate(root, rel string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return beforecode.Templates.ReadFile(rel)
	}
	return data, err
}

// planIDs returns the ULIDs of the plans in the plans ref.
func planIDs(cmd *cobra.Command, s *store.Store) ([]ulid.ULID, error) {
	names, err := s.List(cmd.Context(), core.PlansDir)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []ulid.ULID
	for _, n := range names {
		if id, err := ulid.ParseStrict(strings.TrimSuffix(n, "/")); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
