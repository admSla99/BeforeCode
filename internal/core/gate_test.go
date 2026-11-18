package core_test

import (
	"errors"
	"testing"

	"github.com/admSla99/BeforeCode/internal/core"
)

func TestTheGatesAreForPeople(t *testing.T) {
	t.Parallel()
	if err := core.CheckPerson(false); err != nil {
		t.Errorf("a person: %v", err)
	}
	if err := core.CheckPerson(true); !errors.Is(err, core.ErrGateForPeople) {
		t.Errorf("an agent: %v", err)
	}
}
