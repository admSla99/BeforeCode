package core_test

import (
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

func TestShortIDUsesTheFirstFourRandomCharacters(t *testing.T) {
	id := ulid.MustParse("01JD3K8Q2W5V7X9Y0Z1A2B3C4D")

	if got := core.ShortID(id, nil); got != "bfc-5v7x" {
		t.Errorf("ShortID = %q, want %q (ADR 0002 §8 example)", got, "bfc-5v7x")
	}
}

func TestShortIDGrowsWhileAnotherPlanSharesThePrefix(t *testing.T) {
	id := ulid.MustParse("01JD3K8Q2W5V7X9Y0Z1A2B3C4D")
	existing := []ulid.ULID{
		ulid.MustParse("01JA0000005V7XAAAAAAAAAAAA"), // shares 5v7x
		ulid.MustParse("01JB0000005V7X9AAAAAAAAAAA"), // shares 5v7x9
		ulid.MustParse("01JC000000ZZZZZZZZZZZZZZZZ"), // shares nothing
	}

	if got := core.ShortID(id, existing); got != "bfc-5v7x9y" {
		t.Errorf("ShortID = %q, want %q", got, "bfc-5v7x9y")
	}
}

func TestShortIDIgnoresThePlanItself(t *testing.T) {
	id := ulid.MustParse("01JD3K8Q2W5V7X9Y0Z1A2B3C4D")

	if got := core.ShortID(id, []ulid.ULID{id}); got != "bfc-5v7x" {
		t.Errorf("ShortID = %q, want %q", got, "bfc-5v7x")
	}
}
