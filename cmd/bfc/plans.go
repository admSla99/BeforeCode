package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// errNotInitialised tells the user how to get a plans ref.
var errNotInitialised = errors.New("BeforeCode is not set up in this repository: run bfc init for a new project, or bfc bootstrap to join one that already uses BeforeCode")

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
		return nil, errNotInitialised
	case errors.Is(err, store.ErrNotFound):
		return nil, fmt.Errorf("the plans ref %s has no format-version file; run bfc doctor", store.Ref)
	case err != nil:
		return nil, err
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	switch {
	case err != nil:
		return nil, fmt.Errorf("the plans ref has an unreadable format-version %q", data)
	case v > core.FormatVersion && mode == forReading:
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: the plans ref has format-version %d, newer than this bfc (%d); update bfc\n", v, core.FormatVersion)
	case v > core.FormatVersion:
		return nil, fmt.Errorf("the plans ref has format-version %d, newer than this bfc writes (%d); update bfc", v, core.FormatVersion)
	case v != core.FormatVersion:
		return nil, fmt.Errorf("the plans ref has format-version %d, which this bfc (%d) does not write; it needs a migration", v, core.FormatVersion)
	}
	return s, nil
}
