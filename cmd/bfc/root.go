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
			return cmd.Help()
		},
	}
	root.AddCommand(newVersionCmd(version), newInitCmd(), newNewCmd(), newListCmd(), newShowCmd())
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
