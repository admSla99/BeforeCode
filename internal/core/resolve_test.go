package core_test

import (
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// Two plans whose random parts share "5v7x": A was created first and got
// bfc-5v7x, B later got bfc-5v7x9.
var (
	planA = core.PlanRef{ID: ulid.MustParse("01JD3K8Q2W5V7X1Y0Z1A2B3C4D"), ShortID: "bfc-5v7x"}
	planB = core.PlanRef{ID: ulid.MustParse("01JD3K8Q2W5V7X9Y0Z1A2B3C4D"), ShortID: "bfc-5v7x9"}
	planC = core.PlanRef{ID: ulid.MustParse("01JD3K8Q2WZZZZ0000000000AA"), ShortID: "bfc-zzzz"}
	refs  = []core.PlanRef{planA, planB, planC}
)

func TestResolveIDFindsExactlyOnePlan(t *testing.T) {
	for query, want := range map[string]core.PlanRef{
		"01JD3K8Q2W5V7X9Y0Z1A2B3C4D": planB, // full ULID
		"01jd3k8q2w5v7x9y0z1a2b3c4d": planB, // ULIDs are case-insensitive
		"bfc-5v7x":                   planA, // exact Short ID wins over B's prefix
		"5v7x":                       planA, // the same without bfc-
		"BFC-5V7X9":                  planB,
		"bfc-zzzz0":                  planC, // a longer prefix of the random part
		"5v7x1":                      planA,
	} {
		got, err := core.ResolveID(query, refs)
		if err != nil {
			t.Errorf("ResolveID(%q): %v", query, err)
			continue
		}
		if got != want.ID {
			t.Errorf("ResolveID(%q) = %s, want %s", query, got, want.ID)
		}
	}
}

func TestResolveIDListsCandidatesWhenAmbiguous(t *testing.T) {
	// Two plans created offline at the same time with the same Short ID.
	twin := core.PlanRef{ID: ulid.MustParse("01JD3K8Q2W5V7X2000000000AA"), ShortID: "bfc-5v7x"}

	_, err := core.ResolveID("bfc-5v7x", append(refs, twin))

	var amb *core.AmbiguousIDError
	if !errors.As(err, &amb) {
		t.Fatalf("error = %v, want AmbiguousIDError", err)
	}
	if len(amb.Candidates) != 2 || amb.Candidates[0] != planA || amb.Candidates[1] != twin {
		t.Errorf("candidates = %v, want A and its twin", amb.Candidates)
	}
}

func TestResolveIDRejectsUnknownAndTooShortQueries(t *testing.T) {
	for _, q := range []string{"bfc-aaaa", "01JA0000000000000000000000", "nonsense!"} {
		if _, err := core.ResolveID(q, refs); !errors.Is(err, core.ErrUnknownID) {
			t.Errorf("ResolveID(%q) error = %v, want ErrUnknownID", q, err)
		}
	}
	for _, q := range []string{"5v7", "bfc-5v", ""} {
		_, err := core.ResolveID(q, refs)
		if err == nil || errors.Is(err, core.ErrUnknownID) {
			t.Errorf("ResolveID(%q) error = %v, want a too-short error", q, err)
		}
	}
}
