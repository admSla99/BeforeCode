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
fetch refspec, the remote, the objects of the plans ref, whether the plans
ref is behind the remote, the format-version, the index, and every plan and
event file in the plans ref. Each check prints one line; a failure also
prints the command that fixes it, and says when bfc doctor --fix can repair
it. The exit code is non-zero when any check fails. Warnings do not fail.

With --fix, bfc doctor applies the safe fixes: it adds the fetch refspec,
fetches objects missing from this clone again from the remote, pulls when
the plans ref is behind or diverged, adds a missing format-version in a new
commit, and rebuilds the index. It never pushes and never rewrites the plans
ref. Broken event files and plans without a valid created event are only
reported.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			results := doctor.Run(cmd.Context(), wd, fix)
			if failed := printDoctor(cmd.OutOrStdout(), results, fix); failed > 0 {
				return fmt.Errorf("%d check(s) failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "apply the safe fixes: add the fetch refspec, fetch missing objects, pull, add format-version, rebuild the index")
	return cmd
}

// printDoctor prints one line per check, then the listed items, a failed
// fix and the fix command indented below it. Without --fix it ends with the
// number of failures that --fix would repair. It returns the number of
// failed checks.
func printDoctor(out io.Writer, results []doctor.Result, fix bool) (failed int) {
	const indent = "      "
	repairable := 0
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
		if r.Repair != "" {
			repairable++
		}
		if r.FixErr != nil {
			_, _ = fmt.Fprintf(out, "%sfix failed: %v\n", indent, r.FixErr)
		}
		_, _ = fmt.Fprintf(out, "%sfix: %s\n", indent, r.Fix)
	}
	switch {
	case fix || repairable == 0:
	case repairable == 1:
		_, _ = fmt.Fprintln(out, "1 failed check can be repaired with bfc doctor --fix")
	default:
		_, _ = fmt.Fprintf(out, "%d failed checks can be repaired with bfc doctor --fix\n", repairable)
	}
	return failed
}
