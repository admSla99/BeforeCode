package main

import (
	"encoding/json"
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
		// bfc --version prints the same line as bfc version.
		Version: version,
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
		newSearchCmd(), newBootstrapCmd(), newPullCmd(), newPushCmd(), newSyncCmd(), newDoctorCmd(), newMigrateCmd(), newCheckCmd(), newAcknowledgeCmd(), newInboxCmd(), newReadyCmd(), newClaimCmd(), newHandoffCmd(), newCloseCmd(), newDoneCmd(), newExportCmd(), newHookCmd(), newSetupCmd(), newUpdateCmd(version), newTUICmd())
	root.SetVersionTemplate("bfc {{.Version}}\n")
	guardGates(root) // the Gates are for people (ADR 0011)
	return root
}

func newVersionCmd(version string) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the bfc version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Version string `json:"version"`
				}{version})
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "bfc %s\n", version)
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the version as JSON")
	return cmd
}
