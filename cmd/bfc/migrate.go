package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Upgrade the plans ref to the format version of this bfc",
		Long: `Upgrade the plans ref to the format version this bfc writes (ADR 0013):
rewrite format-version from 1 to 2 in one new commit. Nothing else changes,
because format 2 only adds to format 1.

Nothing is pushed; the next sync shares the migrated ref. Once it is pushed,
an older bfc refuses to write, so everyone in the team updates bfc.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openStore(cmd)
			if err != nil {
				return err
			}
			return runMigrate(cmd, s)
		},
	}
}

func runMigrate(cmd *cobra.Command, s *store.Store) error {
	ctx := cmd.Context()
	data, err := s.Read(ctx, "format-version")
	switch {
	case errors.Is(err, store.ErrNotInitialised):
		return notInitialised(cmd, s)
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("the plans ref %s has no format-version file; run bfc doctor", store.Ref)
	case err != nil:
		return err
	}
	v, err := core.ParseFormatVersion(data)
	switch {
	case err != nil:
		return fmt.Errorf("the plans ref has an unreadable %w; run bfc doctor", err)
	case v == core.FormatVersion:
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "The plans ref already has format-version %d: nothing to do.\n", v)
		return err
	case v > core.FormatVersion:
		return fmt.Errorf("the plans ref has format-version %d, newer than this bfc writes (%d); update bfc", v, core.FormatVersion)
	case v != core.MigratesFrom:
		return fmt.Errorf("the plans ref has format-version %d, which this bfc cannot migrate", v)
	}
	msg := fmt.Sprintf("bfc migrate: format-version %d to %d", v, core.FormatVersion)
	tip, err := s.Write(ctx, msg, map[string][]byte{"format-version": fmt.Appendf(nil, "%d\n", core.FormatVersion)})
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	report(out, "migrated", fmt.Sprintf("%s from format-version %d to %d (%s)", store.Ref, v, core.FormatVersion, short(tip)))
	_, err = fmt.Fprintln(out, "\nNext: bfc push shares it; everyone who writes plans needs this bfc or newer.")
	return err
}
