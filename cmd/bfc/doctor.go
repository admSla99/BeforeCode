package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/doctor"
)

func newDoctorCmd() *cobra.Command {
	var fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the BeforeCode setup of this repository and say how to fix it",
		Long: `Check git, the repository, .beforecode/config.yaml, the plans ref and its
fetch refspec, the remote, whether the plans ref is behind it, the
format-version, the index, and every plan and event file in the plans ref.
Each check prints one line; a failure also prints the command that fixes it.
The exit code is non-zero when any check fails. Warnings do not fail.

With --fix, bfc doctor applies the safe fixes: it adds the fetch refspec,
pulls when the plans ref is behind or diverged, and rebuilds the index. It
never pushes and never rewrites the plans ref.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			results := doctor.Run(cmd.Context(), wd, fix)
			if failed := printDoctor(cmd.OutOrStdout(), results); failed > 0 {
				return fmt.Errorf("%d check(s) failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "apply the safe fixes: add the fetch refspec, pull, rebuild the index")
	return cmd
}

// printDoctor prints one line per check, then the listed items, a failed
// fix and the fix command indented below it. It returns the number of
// failed checks.
func printDoctor(out io.Writer, results []doctor.Result) (failed int) {
	const indent = "      "
	for _, r := range results {
		status := string(r.Status)
		if r.Fixed {
			status = "fixed"
		}
		_, _ = fmt.Fprintf(out, "%-5s %s: %s\n", status, r.Check, r.Detail)
		for _, item := range r.Items {
			_, _ = fmt.Fprintf(out, "%s%s\n", indent, item)
		}
		if r.Status != doctor.Fail {
			continue
		}
		failed++
		if r.FixErr != nil {
			_, _ = fmt.Fprintf(out, "%sfix failed: %v\n", indent, r.FixErr)
		}
		_, _ = fmt.Fprintf(out, "%sfix: %s\n", indent, r.Fix)
	}
	return failed
}
