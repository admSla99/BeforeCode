package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
)

func newCheckFindCmd() *cobra.Command {
	var branch, bodyFile string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "find --branch <head ref> [--body-file <file>]",
		Short: "Name the plan of a pull request",
		Long: `Name the plan a pull request belongs to (ADR 0015): the Short ID in its
branch name, resolved like the Current plan's (bfc start), else a line
"Plan: bfc-…" in its description, read from --body-file (- reads stdin). Prints the Short ID,
or nothing when neither names a plan; with --json it prints
{"found": false} or the plan's id, short_id, title and state. A Short ID
that names no plan is an error.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var body []byte
			switch bodyFile {
			case "":
			case "-":
				var err error
				if body, err = io.ReadAll(cmd.InOrStdin()); err != nil {
					return err
				}
			default:
				var err error
				if body, err = os.ReadFile(bodyFile); err != nil {
					return err
				}
			}
			s, err := openPlans(cmd, forReading)
			if err != nil {
				return err
			}
			views, err := loadPlans(cmd.Context(), s)
			if err != nil {
				return err
			}
			byID, refs := planHandles(views)
			id, found, err := core.PullRequestPlan(branch, string(body), refs)
			if err != nil {
				return err
			}
			out := checkFindJSON{Found: found}
			if found {
				v := byID[id]
				out = checkFindJSON{Found: true, ID: v.ID.String(), ShortID: v.ShortID, Title: v.Title, State: string(v.Status.State)}
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			if found {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), out.ShortID)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "the head branch of the pull request")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "a file with the description of the pull request, - for stdin")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

// checkFindJSON is the --json shape of bfc check find.
type checkFindJSON struct {
	Found   bool   `json:"found"`
	ID      string `json:"id,omitempty"`
	ShortID string `json:"short_id,omitempty"`
	Title   string `json:"title,omitempty"`
	State   string `json:"state,omitempty"`
}
