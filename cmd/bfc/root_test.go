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

// AC-1: bfc --version prints the same line as bfc version.
func TestVersionFlagPrintsSameLineAsVersion(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"v0.1.0", "dev"} {
		want := run(t, v, "version")
		if got := run(t, v, "--version"); got != want {
			t.Errorf("version %s: bfc --version printed %q, bfc version %q", v, got, want)
		}
		if want != "bfc "+v+"\n" {
			t.Errorf("version %s: bfc version printed %q", v, want)
		}
	}
}

// AC-2: bfc version --json prints {"version":…}.
func TestVersionJSON(t *testing.T) {
	t.Parallel()
	got := run(t, "v0.1.0", "version", "--json")
	if want := `{"version":"v0.1.0"}` + "\n"; got != want {
		t.Errorf("bfc version --json printed %q, want %q", got, want)
	}
}
