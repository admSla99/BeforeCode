package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// errNotInitialised tells the user how to get a plans ref.
var errNotInitialised = errors.New("BeforeCode is not set up in this repository: run bfc init for a new project, or bfc bootstrap to join one that already uses BeforeCode")

// remoteCheckTimeout bounds the network check of notInitialised.
const remoteCheckTimeout = 10 * time.Second

// notInitialised explains a missing plans ref. Only then does it ask the
// sync remote whether it has plans, to suggest bfc bootstrap or bfc init.
// When it cannot tell (local mode, no such remote, no answer), it gives the
// general advice.
func notInitialised(cmd *cobra.Command, s *store.Store) error {
	remote, err := syncRemote(s, "", false)
	if err != nil {
		return errNotInitialised
	}
	if ok, err := s.HasRemote(cmd.Context(), remote); err != nil || !ok {
		return errNotInitialised
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), remoteCheckTimeout)
	defer cancel()
	has, err := s.RemoteHasPlans(ctx, remote)
	switch {
	case err != nil:
		return errNotInitialised
	case has:
		return fmt.Errorf("%s already has plans, but this clone does not: run bfc bootstrap to get them", remote)
	default:
		return fmt.Errorf("BeforeCode is not set up in this repository and %s has no plans: run bfc init", remote)
	}
}

// access says whether a command only reads plans or also writes them.
type access int

const (
	forReading access = iota
	forWriting
)

// openPlans opens the store of the repository around the working directory.
// It is the one place that checks that the plans ref exists and has a
// format version this binary can use: a writer refuses any other version, a
// reader of a newer version warns and reads on a best-effort basis
// (ADR 0002 section 2).
func openPlans(cmd *cobra.Command, mode access) (*store.Store, error) {
	ctx := cmd.Context()
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	s, err := store.Open(wd)
	if err != nil {
		return nil, err
	}
	data, err := s.Read(ctx, "format-version")
	switch {
	case errors.Is(err, store.ErrNotInitialised):
		return nil, notInitialised(cmd, s)
	case errors.Is(err, store.ErrNotFound):
		return nil, fmt.Errorf("the plans ref %s has no format-version file; run bfc doctor", store.Ref)
	case err != nil:
		return nil, err
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	switch {
	case err != nil:
		return nil, fmt.Errorf("the plans ref has an unreadable format-version %q", data)
	case v < core.FormatVersion && mode == forReading:
		// An older format reads the same way; only writing needs a migration.
	case v > core.FormatVersion && mode == forReading:
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: the plans ref has format-version %d, newer than this bfc (%d); update bfc\n", v, core.FormatVersion)
	case v > core.FormatVersion:
		return nil, fmt.Errorf("the plans ref has format-version %d, newer than this bfc writes (%d); update bfc", v, core.FormatVersion)
	case v != core.FormatVersion:
		return nil, fmt.Errorf("the plans ref has format-version %d, which this bfc (%d) does not write; it needs a migration", v, core.FormatVersion)
	}
	return s, nil
}
