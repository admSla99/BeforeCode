package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// errNotInitialised tells the user how to get a plans ref.
var errNotInitialised = errors.New("BeforeCode is not set up in this repository: run bfc init for a new project, or bfc bootstrap to join one that already uses BeforeCode")

// openPlans opens the store of the repository around the working directory
// for commands that write plans. It is the one place that checks that the
// plans ref exists and has the format version this binary writes.
func openPlans(ctx context.Context) (*store.Store, error) {
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
	case v > core.FormatVersion:
		return nil, fmt.Errorf("the plans ref has format-version %d, newer than this bfc writes (%d); update bfc", v, core.FormatVersion)
	case v != core.FormatVersion:
		return nil, fmt.Errorf("the plans ref has format-version %d, which this bfc (%d) does not write; it needs a migration", v, core.FormatVersion)
	}
	return s, nil
}
