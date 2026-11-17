package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/admSla99/BeforeCode/internal/config"
)

func TestParseReadsAVersionOneFile(t *testing.T) {
	got, err := config.Parse([]byte(`version: 1
approval:
  required_approvals: 2
sync:
  enabled: true
  remote: upstream
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := config.Config{RequiredApprovals: 2, SyncEnabled: true, Remote: "upstream"}
	if got != want {
		t.Errorf("Parse = %+v, want %+v", got, want)
	}
}

func TestParseRejectsInvalidFilesNamingTheProblem(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"unknown key": {
			"version: 1\napproval:\n  required_approvals: 1\n  approvers: 2\nsync:\n  enabled: false\n",
			"approvers",
		},
		"newer version": {
			"version: 3\napproval:\n  required_approvals: 1\nsync:\n  enabled: false\n",
			"update bfc",
		},
		"missing version": {
			"approval:\n  required_approvals: 1\nsync:\n  enabled: false\n",
			"version",
		},
		"zero approvals": {
			"version: 1\napproval:\n  required_approvals: 0\nsync:\n  enabled: false\n",
			"required_approvals",
		},
		"missing approvals": {
			"version: 1\nsync:\n  enabled: false\n",
			"required_approvals",
		},
		"missing sync.enabled": {
			"version: 1\napproval:\n  required_approvals: 1\nsync:\n  remote: origin\n",
			"sync.enabled",
		},
		"sync on without remote": {
			"version: 1\napproval:\n  required_approvals: 1\nsync:\n  enabled: true\n",
			"sync.remote",
		},
		"sync off with remote": {
			"version: 1\napproval:\n  required_approvals: 1\nsync:\n  enabled: false\n  remote: origin\n",
			"sync.remote",
		},
		"interval in version 1": {
			"version: 1\napproval:\n  required_approvals: 1\nsync:\n  enabled: true\n  remote: origin\n  interval: 30\n",
			"version 2",
		},
		"interval too short": {
			"version: 2\napproval:\n  required_approvals: 1\nsync:\n  enabled: true\n  remote: origin\n  interval: 2\n",
			"sync.interval",
		},
		"interval too long": {
			"version: 2\napproval:\n  required_approvals: 1\nsync:\n  enabled: true\n  remote: origin\n  interval: 7200\n",
			"sync.interval",
		},
		"interval with sync off": {
			"version: 2\napproval:\n  required_approvals: 1\nsync:\n  enabled: false\n  interval: 30\n",
			"sync.interval",
		},
		"not yaml": {"version: [", "config"},
		"empty":    {"", "version"},
	}
	for name, c := range cases {
		_, err := config.Parse([]byte(c.yaml))
		if err == nil {
			t.Errorf("%s: Parse succeeded", name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", name, err, c.want)
		}
	}
}

func TestRenderedFileParsesBackAndExplainsItself(t *testing.T) {
	for _, c := range []config.Config{
		{RequiredApprovals: 1, SyncEnabled: true, Remote: "origin"},
		{RequiredApprovals: 1, SyncEnabled: true, Remote: "origin", SyncInterval: 45 * time.Second},
		{RequiredApprovals: 3, SyncEnabled: false},
	} {
		data := config.Render(c)
		got, err := config.Parse(data)
		if err != nil {
			t.Fatalf("Parse(Render(%+v)): %v\n%s", c, err, data)
		}
		if got != c {
			t.Errorf("round trip of %+v gave %+v", c, got)
		}
		if !strings.Contains(string(data), "# ") {
			t.Errorf("rendered file has no comments:\n%s", data)
		}
	}
}

func TestParseReadsVersionTwoWithASyncInterval(t *testing.T) {
	got, err := config.Parse([]byte("version: 2\napproval:\n  required_approvals: 1\nsync:\n  enabled: true\n  remote: origin\n  interval: 30\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.SyncInterval != 30*time.Second || got.Interval() != 30*time.Second {
		t.Errorf("interval = %v, %v; want 30s", got.SyncInterval, got.Interval())
	}
}

func TestTheSyncIntervalDefaultsTo15Seconds(t *testing.T) {
	for _, v := range []string{"1", "2"} {
		got, err := config.Parse([]byte("version: " + v + "\napproval:\n  required_approvals: 1\nsync:\n  enabled: true\n  remote: origin\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got.SyncInterval != 0 || got.Interval() != config.DefaultSyncInterval || config.DefaultSyncInterval != 15*time.Second {
			t.Errorf("version %s: interval = %v, Interval() = %v", v, got.SyncInterval, got.Interval())
		}
	}
}

func TestRenderWritesTheCurrentVersion(t *testing.T) {
	data := string(config.Render(config.Config{RequiredApprovals: 1, SyncEnabled: true, Remote: "origin", SyncInterval: 15 * time.Second}))
	if !strings.Contains(data, "version: 2\n") || !strings.Contains(data, "interval: 15\n") {
		t.Errorf("rendered file:\n%s", data)
	}
}
