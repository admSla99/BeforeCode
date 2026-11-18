package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
)

// idWith returns a ULID whose random part starts with prefix.
func idWith(prefix string) ulid.ULID {
	s := "01JD000000" + strings.ToUpper(prefix)
	return ulid.MustParseStrict(s + strings.Repeat("0", 26-len(s)))
}

func TestCurrentPlanTakesTheShortIDInTheBranchOverTheUsedPlan(t *testing.T) {
	a, b := idWith("a1b2"), idWith("xyzw")
	plans := []PlanHandle{{ID: a, ShortID: "bfc-a1b2"}, {ID: b, ShortID: "bfc-xyzw"}}

	got, err := CurrentPlan("bfc-a1b2-payments-via-stripe", b, plans)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a || got.Source != FromBranch {
		t.Errorf("got %+v, want %s from the branch", got, a)
	}
}

func TestCurrentPlanFallsBackToTheUsedPlan(t *testing.T) {
	a := idWith("a1b2")
	plans := []PlanHandle{{ID: a, ShortID: "bfc-a1b2"}}
	none := ulid.ULID{}
	for _, tc := range []struct {
		name   string
		branch string
		used   ulid.ULID
		want   Current
	}{
		{"branch without a Short ID", "main", a, Current{ID: a, Source: FromUse}},
		{"detached HEAD", "", a, Current{ID: a, Source: FromUse}},
		{"nothing used", "main", none, Current{}},
		{"detached HEAD, nothing used", "", none, Current{}},
		{"Short ID inside a longer word", "xbfc-a1b2", none, Current{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CurrentPlan(tc.branch, tc.used, plans)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCurrentPlanReportsAShortIDInTheBranchThatNamesNoPlan(t *testing.T) {
	a := idWith("a1b2")
	plans := []PlanHandle{{ID: a, ShortID: "bfc-a1b2"}}

	_, err := CurrentPlan("feat/bfc-zzzz-typo", a, plans)
	var be *BranchPlanError
	if !errors.As(err, &be) || !errors.Is(err, ErrUnknownID) {
		t.Fatalf("err = %v, want a BranchPlanError for an unknown ID", err)
	}
	if be.Branch != "feat/bfc-zzzz-typo" || be.ShortID != "bfc-zzzz" {
		t.Errorf("got %+v", be)
	}
}

func TestCurrentPlanReportsAnAmbiguousShortIDInTheBranch(t *testing.T) {
	a, b := idWith("a1b2c"), idWith("a1b2d")
	plans := []PlanHandle{{ID: a, ShortID: "bfc-a1b2"}, {ID: b, ShortID: "bfc-a1b2"}}

	_, err := CurrentPlan("bfc-a1b2", ulid.ULID{}, plans)
	var amb *AmbiguousIDError
	if !errors.As(err, &amb) || len(amb.Candidates) != 2 {
		t.Fatalf("err = %v, want an ambiguous ID", err)
	}
	if got, err := CurrentPlan("bfc-a1b2d-payments", ulid.ULID{}, plans); err != nil || got.ID != b {
		t.Errorf("a longer prefix: got %+v, %v; want %s", got, err, b)
	}
}

func TestCurrentPlanReportsAUsedPlanThatDoesNotExist(t *testing.T) {
	_, err := CurrentPlan("main", idWith("a1b2"), nil)
	if !errors.Is(err, ErrUnknownID) {
		t.Fatalf("err = %v, want ErrUnknownID", err)
	}
}
