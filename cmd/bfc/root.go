package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newRootCmd builds the bfc command tree. Running bfc without a
// subcommand prints usage.
func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Annotations:  map[string]string{versionAnnotation: version},
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
		newAskCmd(), newReplyCmd(), newWaitCmd(), newPrimeCmd(),
		newSearchCmd(), newBootstrapCmd(), newPullCmd(), newPushCmd(), newSyncCmd(), newDoctorCmd(), newMigrateCmd(), newCheckCmd(), newAcknowledgeCmd(), newInboxCmd(), newReadyCmd(), newClaimCmd(), newHandoffCmd(), newCloseCmd(), newDoneCmd(), newHookCmd(), newSetupCmd(), newTUICmd())
	guardGates(root) // the Gates are for people (ADR 0011)
	return root
}

// versionAnnotation holds the version of bfc on the root command, for the
// commands that need it.
const versionAnnotation = "bfc-version"

// bfcVersion is the version of this bfc, "dev" for a build without one.
func bfcVersion(cmd *cobra.Command) string {
	return cmd.Root().Annotations[versionAnnotation]
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
