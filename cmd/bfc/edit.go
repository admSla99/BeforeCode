package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// planTemplate is the path of the plan.md template, relative to the
// repository root and inside the embedded defaults.
const planTemplate = ".beforecode/templates/plan.md"

func newEditCmd() *cobra.Command {
	var intent, stdin, force, check bool
	cmd := &cobra.Command{
		Use:   "edit <id> [--intent] [--stdin [--check | --force]]",
		Short: "Edit plan.md (or intent.md) of a plan in your editor, or write it from stdin",
		Long: `Open plan.md of a plan in the editor git uses (GIT_EDITOR, core.editor,
VISUAL, EDITOR), or intent.md with --intent. A missing plan.md starts from the
template. The file is edited in a temporary location; a changed file is
committed to the plans ref, an unchanged one is not.

If the document changed in the plans ref while the editor was open, nothing
is committed and your text is kept in a file whose path is printed.
Editing plan.md makes earlier Approvals stale, so an approved plan returns to
in-review. Problems in plan.md are reported as warnings.

With --stdin the document is read from stdin instead, for agents and
scripts; the same rules apply. It replaces a plan.md only while that is
still the template, unless --force is given. --check prints the problems of
the document on stdin and commits nothing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case (force || check) && !stdin:
				return errors.New("--force and --check only apply with --stdin")
			case force && check:
				return errors.New("--check commits nothing, so --force does not apply")
			case force && intent:
				return errors.New("--force replaces a written plan.md; intent.md needs none")
			}
			doc := "plan.md"
			if intent {
				doc = "intent.md"
			}
			return runEdit(cmd, args[0], doc, editSource{stdin: stdin, force: force, check: check})
		},
	}
	cmd.Flags().BoolVar(&intent, "intent", false, "edit intent.md instead of plan.md")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read the document from stdin instead of opening an editor")
	cmd.Flags().BoolVar(&force, "force", false, "with --stdin: replace a plan.md that is no longer the template")
	cmd.Flags().BoolVar(&check, "check", false, "with --stdin: print the problems of the document and commit nothing")
	return cmd
}

// editSource says where bfc edit takes the document from: the editor, or
// stdin. From stdin a written plan.md is replaced only with force, and
// check only reports the document's problems.
type editSource struct{ stdin, force, check bool }

// runEdit writes doc of the plan query from src.
func runEdit(cmd *cobra.Command, query, doc string, src editSource) error {
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
	if text == nil && doc == "intent.md" {
		return fmt.Errorf("%s has no intent.md; run bfc doctor", v.ShortID)
	}
	if doc == "plan.md" {
		tpl, err := readTemplate(s.Root(), planTemplate)
		if err != nil {
			return err
		}
		if text == nil {
			text = tpl
		} else if src.stdin && !src.force && !src.check && !sameText(text, tpl) {
			return fmt.Errorf("plan.md of %s is already written, not the template; nothing committed\nRead it with bfc show %s, and replace it with --force only if that is what the user wants", v.ShortID, v.ShortID)
		}
	}

	var edited []byte
	var kept string // where the editor's text is kept; stdin keeps none
	if src.stdin {
		if edited, err = io.ReadAll(cmd.InOrStdin()); err != nil {
			return fmt.Errorf("cannot read %s from stdin: %w; nothing committed", doc, err)
		}
	} else if edited, kept, err = editText(cmd, s, "*-"+doc, text); err != nil {
		return err
	}
	// Stored files use LF (ADR 0002); editors on Windows may save CRLF.
	edited = bytes.ReplaceAll(edited, []byte("\r\n"), []byte("\n"))
	if src.check {
		return checkDoc(cmd, doc, edited)
	}
	if bytes.Equal(edited, text) {
		removeKept(kept)
		if _, err := fmt.Fprintf(out, "no changes to %s of %s\n", doc, v.ShortID); err != nil {
			return err
		}
		warnPlan(cmd, doc, edited)
		return nil
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
	removeKept(kept)
	if _, err := fmt.Fprintf(out, "saved %s of %s\n", doc, v.ShortID); err != nil {
		return err
	}

	if doc != "plan.md" {
		return nil
	}
	if v.Status.State == core.StateApproved || v.Status.State == core.StateInProgress {
		_, _ = fmt.Fprintf(out, "%s is back in-review: its approvals were for the previous version of plan.md\n", v.ShortID)
	}
	warnPlan(cmd, doc, edited)
	return nil
}

// warnPlan prints the problems of a plan.md (ADR 0002 section 6) as
// warnings, and reports whether there were any.
func warnPlan(cmd *cobra.Command, doc string, text []byte) bool {
	if doc != "plan.md" {
		return false
	}
	problems := core.ValidatePlan(text)
	for _, p := range problems {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: plan.md %s\n", p)
	}
	return len(problems) > 0
}

// checkDoc is bfc edit --stdin --check: it reports the problems of the
// document and commits nothing. Problems that bfc edit refuses are errors,
// the rest warnings.
func checkDoc(cmd *cobra.Command, doc string, text []byte) error {
	if len(bytes.TrimSpace(text)) == 0 {
		return fmt.Errorf("%s is empty; nothing committed", doc)
	}
	if doc == "intent.md" {
		if _, err := core.ParseIntent(text); err != nil {
			return fmt.Errorf("%w; nothing committed", err)
		}
	}
	if warnPlan(cmd, doc, text) {
		return nil
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s has no problems; nothing committed\n", doc)
	return err
}

// sameText reports whether a and b are the same document, apart from line
// endings and surrounding blank space.
func sameText(a, b []byte) bool {
	norm := func(d []byte) []byte { return bytes.TrimSpace(bytes.ReplaceAll(d, []byte("\r\n"), []byte("\n"))) }
	return bytes.Equal(norm(a), norm(b))
}

// removeKept removes the file the editor's text was kept in, if any.
func removeKept(kept string) {
	if kept != "" {
		_ = os.Remove(kept)
	}
}
