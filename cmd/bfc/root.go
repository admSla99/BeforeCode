package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newRootCmd builds the bfc command tree. Running bfc without a
// subcommand prints usage.
func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:          "bfc",
		Short:        "Plan before code: write, grill, approve and share plans for AI coding agents",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// In a terminal bfc opens the TUI; in scripts and pipes it
			// prints usage.
			if !inTerminal(cmd) {
				return cmd.Help()
			}
			return runTUI(cmd)
		},
	}
	root.AddCommand(newVersionCmd(version), newInitCmd(), newNewCmd(), newListCmd(), newShowCmd(), newStartCmd(), newUseCmd(), newEditCmd(),
		newSubmitCmd(), newApproveCmd(), newRequestChangesCmd(), newWithdrawCmd(),
		newAskCmd(), newReplyCmd(), newPrimeCmd(),
		newSearchCmd(), newBootstrapCmd(), newPullCmd(), newPushCmd(), newSyncCmd(), newDoctorCmd(), newCloseCmd(), newHookCmd(), newTUICmd())
	return root
}

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the bfc version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "bfc %s\n", version)
			return err
		},
	}
}
