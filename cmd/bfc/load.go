package main

import (
	"context"
	"errors"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

// withIndex opens the index of the store's repository, brought up to date
// with the plans ref, for the duration of fn.
func withIndex(ctx context.Context, s *store.Store, fn func(*index.Index) error) error {
	ix, err := index.Open(ctx, s)
	if err != nil {
		return err
	}
	err = fn(ix)
	if cerr := ix.Close(); err == nil {
		err = cerr
	}
	return err
}

// loadPlans returns every plan, newest activity first, from the index.
func loadPlans(ctx context.Context, s *store.Store) ([]index.Plan, error) {
	var plans []index.Plan
	err := withIndex(ctx, s, func(ix *index.Index) (err error) {
		plans, err = ix.Plans(ctx)
		return err
	})
	return plans, err
}

// loadPlan returns one plan as it is now in the plans ref.
func loadPlan(ctx context.Context, s *store.Store, id ulid.ULID) (index.Plan, error) {
	var p index.Plan
	err := withIndex(ctx, s, func(ix *index.Index) (err error) {
		p, err = ix.Plan(ctx, id)
		return err
	})
	return p, err
}

// listPlanIDs returns the ULIDs of the plan directories in the plans ref.
func listPlanIDs(ctx context.Context, s *store.Store) ([]ulid.ULID, error) {
	names, err := s.List(ctx, core.PlansDir)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []ulid.ULID
	for _, n := range names {
		if id, err := ulid.ParseStrict(strings.TrimSuffix(n, "/")); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
