package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// planTemplate is the path of the plan.md template, relative to the
// repository root and inside the embedded defaults.
const planTemplate = ".beforecode/templates/plan.md"

func newEditCmd() *cobra.Command {
	var intent bool
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit plan.md (or intent.md) of a plan in your editor",
		Long: `Open plan.md of a plan in the editor git uses (GIT_EDITOR, core.editor,
VISUAL, EDITOR), or intent.md with --intent. A missing plan.md starts from the
template. The file is edited in a temporary location; a changed file is
committed to the plans ref, an unchanged one is not.

If the document changed in the plans ref while the editor was open, nothing
is committed and your text is kept in a file whose path is printed.
Editing plan.md makes earlier Approvals stale, so an approved plan returns to
in-review. Problems in plan.md are reported as warnings.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			doc := "plan.md"
			if intent {
				doc = "intent.md"
			}
			return runEdit(cmd, args[0], doc)
		},
	}
	cmd.Flags().BoolVar(&intent, "intent", false, "edit intent.md instead of plan.md")
	return cmd
}

func runEdit(cmd *cobra.Command, query, doc string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	s, err := openPlans(cmd, forWriting)
	if err != nil {
		return err
	}
	v, err := resolvePlan(ctx, s, query)
	if err != nil {
		return err
	}
	if v.Status.State == core.StateClosed {
		return fmt.Errorf("%s is closed; a closed plan cannot be edited", v.ShortID)
	}
	path := core.PlanDir(v.ID) + "/" + doc

	// Remember the version the user starts from, to notice a concurrent change.
	startBlob, err := s.ObjectID(ctx, path)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	text := v.Plan
	if doc == "intent.md" {
		text = v.Intent
	}
	if text == nil {
		if doc == "intent.md" {
			return fmt.Errorf("%s has no intent.md; run bfc doctor", v.ShortID)
		}
		if text, err = readTemplate(s.Root(), planTemplate); err != nil {
			return err
		}
	}

	edited, kept, err := editText(cmd, s, "*-"+doc, text)
	if err != nil {
		return err
	}
	// Stored files use LF (ADR 0002); editors on Windows may save CRLF.
	edited = bytes.ReplaceAll(edited, []byte("\r\n"), []byte("\n"))
	if bytes.Equal(edited, text) {
		_ = os.Remove(kept)
		_, err := fmt.Fprintf(out, "no changes to %s of %s\n", doc, v.ShortID)
		return err
	}
	keep := func(err error) error { return textKept(err, "nothing committed", kept) }
	if len(bytes.TrimSpace(edited)) == 0 {
		return keep(fmt.Errorf("%s is empty", doc))
	}
	if doc == "intent.md" {
		if _, err := core.ParseIntent(edited); err != nil {
			return keep(err)
		}
	}

	nowBlob, err := s.ObjectID(ctx, path)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return keep(err)
	}
	if nowBlob != startBlob {
		return keep(fmt.Errorf("%s of %s changed in the plans ref while you were editing; run bfc edit again and bring your changes over", doc, v.ShortID))
	}
	// Write refuses if the ref moved after the check above.
	if _, err := s.Write(ctx, "bfc edit "+v.ShortID+": "+doc, map[string][]byte{path: edited}); err != nil {
		return keep(err)
	}
	_ = os.Remove(kept)
	if _, err := fmt.Fprintf(out, "saved %s of %s\n", doc, v.ShortID); err != nil {
		return err
	}

	if doc != "plan.md" {
		return nil
	}
	if v.Status.State == core.StateApproved {
		_, _ = fmt.Fprintf(out, "%s is back in-review: its approvals were for the previous version of plan.md\n", v.ShortID)
	}
	for _, p := range core.ValidatePlan(edited) {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: plan.md %s\n", p)
	}
	return nil
}
