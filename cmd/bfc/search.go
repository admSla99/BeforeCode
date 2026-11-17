package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/index"
)

func newSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <text>",
		Short: "Find plans whose title, intent.md or plan.md contain the text",
		Long: `Find plans whose title, intent.md or plan.md contain every word of the text,
best match first. Case and diacritics do not matter; front matter and HTML
comments are not searched. Each plan prints its Short ID,
state and title, then a snippet with the matches in [brackets].`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := strings.Join(args, " ")
			s, err := openPlans(cmd, forReading)
			if err != nil {
				return err
			}
			var hits []index.Hit
			err = withIndex(cmd.Context(), s, func(ix *index.Index) (err error) {
				hits, err = ix.Search(cmd.Context(), text)
				return err
			})
			if err != nil {
				return err
			}
			if len(hits) == 0 {
				_, err := fmt.Fprintf(cmd.ErrOrStderr(), "no plans match %q\n", text)
				return err
			}
			var b strings.Builder
			for _, h := range hits {
				fmt.Fprintf(&b, "%s  %s  %s\n    %s\n", h.Plan.ShortID, h.Plan.Status.State, h.Plan.Title, h.Snippet)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), b.String())
			return err
		},
	}
}
