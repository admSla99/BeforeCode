package main

import (
	"bytes"
	"strings"
	"testing"
)

func run(t *testing.T, version string, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd(version)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bfc %v: %v", args, err)
	}
	return out.String()
}

func TestVersionPrintsInjectedVersion(t *testing.T) {
	t.Parallel()
	got := run(t, "v1.2.3", "version")
	if got != "bfc v1.2.3\n" {
		t.Errorf("got %q, want %q", got, "bfc v1.2.3\n")
	}
}

func TestVersionDefaultsToDev(t *testing.T) {
	t.Parallel()
	if version != "dev" {
		t.Errorf("default version is %q, want %q", version, "dev")
	}
}

func TestUsageListsCommands(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"--help"}} {
		got := run(t, "dev", args...)
		for _, want := range []string{"Usage:", "Available Commands:", "version"} {
			if !strings.Contains(got, want) {
				t.Errorf("bfc %v output lacks %q:\n%s", args, want, got)
			}
		}
	}
}
