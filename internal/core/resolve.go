package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"
)

// PlanHandle is what ResolveID needs to know about a plan.
type PlanHandle struct {
	ID      ulid.ULID
	ShortID string
}

// ErrUnknownID is returned when no plan matches a query.
var ErrUnknownID = errors.New("no plan with this ID")

// AmbiguousIDError is returned when several plans match a query.
type AmbiguousIDError struct {
	Query      string
	Candidates []PlanHandle
}

func (e *AmbiguousIDError) Error() string {
	return fmt.Sprintf("%q matches %d plans", e.Query, len(e.Candidates))
}

// ResolveID finds the plan a user means (ADR 0002 section 8). The query is
// a full ULID, a Short ID, or a prefix of at least four characters of the
// random part of a plan's ULID, with or without "bfc-", in any case. An
// exact Short ID wins over longer Short IDs it is a prefix of.
func ResolveID(query string, plans []PlanHandle) (ulid.ULID, error) {
	if id, err := ulid.ParseStrict(strings.ToUpper(query)); err == nil {
		for _, p := range plans {
			if p.ID == id {
				return id, nil
			}
		}
		return ulid.ULID{}, fmt.Errorf("%w: %s", ErrUnknownID, query)
	}
	prefix := strings.TrimPrefix(strings.ToLower(query), ShortIDPrefix)
	if len(prefix) < minShortIDLen {
		return ulid.ULID{}, fmt.Errorf("plan ID %q is too short: give at least %d characters after %s", query, minShortIDLen, ShortIDPrefix)
	}
	var exact, byPrefix []PlanHandle
	for _, p := range plans {
		if strings.EqualFold(p.ShortID, ShortIDPrefix+prefix) {
			exact = append(exact, p)
		}
		if strings.HasPrefix(randomPart(p.ID), prefix) {
			byPrefix = append(byPrefix, p)
		}
	}
	for _, matches := range [][]PlanHandle{exact, byPrefix} {
		switch len(matches) {
		case 0:
			continue
		case 1:
			return matches[0].ID, nil
		default:
			return ulid.ULID{}, &AmbiguousIDError{Query: query, Candidates: matches}
		}
	}
	return ulid.ULID{}, fmt.Errorf("%w: %s", ErrUnknownID, query)
}
