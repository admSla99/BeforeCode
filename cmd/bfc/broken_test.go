package main

import (
	"os"
	"testing"
)

// Deliberately broken to show that CI fails (issue #2, AC-4). Reverted in the next commit.
func TestDeliberatelyBroken(t *testing.T) {
	os.Remove("does-not-exist") // unchecked error: errcheck must fail the Lint job
	t.Fatal("deliberate failure: the Test jobs must fail")
}
