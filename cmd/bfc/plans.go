package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// errNotInitialised tells the user how to get a plans ref.
var errNotInitialised = errors.New("BeforeCode is not set up in this repository: run bfc init for a new project, or bfc bootstrap to join one that already uses BeforeCode")

// remoteCheckTimeout bounds the network check of notInitialised.
const remoteCheckTimeout = 10 * time.Second

// notSetUpError is a missing Plans ref, with what the sync remote said
// about its plans: remote is empty when it could not tell.
type notSetUpError struct {
	remote         string
	remoteHasPlans bool
}

func (e *notSetUpError) Error() string {
	switch {
	case e.remote == "":
		return errNotInitialised.Error()
	case e.remoteHasPlans:
		return e.remote + " already has plans, but this clone does not: run bfc bootstrap to get them"
	default:
		return "BeforeCode is not set up in this repository and " + e.remote + " has no plans: run bfc init"
	}
}

func (e *notSetUpError) Is(target error) bool { return target == errNotInitialised }

// notInitialised explains a missing plans ref as a *notSetUpError. Only then
// does it ask the sync remote whether it has plans, to suggest bfc
// bootstrap or bfc init. When it cannot tell (local mode, no such remote,
// no answer), it gives the general advice.
func notInitialised(cmd *cobra.Command, s *store.Store) error {
	remote, err := syncRemote(s, "", false)
	if err != nil {
		return &notSetUpError{}
	}
	if ok, err := s.HasRemote(cmd.Context(), remote); err != nil || !ok {
		return &notSetUpError{}
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), remoteCheckTimeout)
	defer cancel()
	has, err := s.RemoteHasPlans(ctx, remote)
	if err != nil {
		return &notSetUpError{}
	}
	return &notSetUpError{remote: remote, remoteHasPlans: has}
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
	s, err := openStore(cmd)
	if err != nil {
		return nil, err
	}
	err = checkPlansRef(cmd, s, mode)
	if errors.Is(err, store.ErrNotInitialised) {
		return nil, notInitialised(cmd, s)
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// checkPlansRef is the check of openPlans on an open store, without asking
// the remote: a missing plans ref is store.ErrNotInitialised.
func checkPlansRef(cmd *cobra.Command, s *store.Store, mode access) error {
	data, err := s.Read(cmd.Context(), "format-version")
	switch {
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("the plans ref %s has no format-version file; run bfc doctor", store.Ref)
	case err != nil:
		return err
	}
	v, err := core.ParseFormatVersion(data)
	switch {
	case err != nil:
		return fmt.Errorf("the plans ref has an unreadable %w; run bfc doctor", err)
	case v < core.FormatVersion && mode == forReading:
		// An older format reads the same way; only writing needs a migration.
	case v > core.FormatVersion && mode == forReading:
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: the plans ref has format-version %d, newer than this bfc (%d); update bfc\n", v, core.FormatVersion)
	case v > core.FormatVersion:
		return fmt.Errorf("the plans ref has format-version %d, newer than this bfc writes (%d); update bfc", v, core.FormatVersion)
	case v == core.MigratesFrom:
		return fmt.Errorf("the plans ref has format-version %d, which this bfc (%d) does not write; run bfc migrate to upgrade it", v, core.FormatVersion)
	case v != core.FormatVersion:
		return fmt.Errorf("the plans ref has format-version %d, which this bfc (%d) does not support; run bfc doctor", v, core.FormatVersion)
	}
	return nil
}
