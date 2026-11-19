// Package config reads and writes .beforecode/config.yaml as specified in
// docs/adr/0003-config-file-v1.md (versions 1 to 3). It works on bytes only.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"

	"go.yaml.in/yaml/v3"
)

// Path is where the config file lives, relative to the repository root.
const Path = ".beforecode/config.yaml"

// Version is the config version this binary writes. It also reads every
// older version.
const Version = 3

// DefaultClaudeCommand is the command that starts Claude Code when
// claude.command is not set.
const DefaultClaudeCommand = "claude"

// DefaultSyncInterval is the time between background syncs in the TUI when
// sync.interval is not set.
const DefaultSyncInterval = 15 * time.Second

// The bounds of sync.interval, in seconds.
const (
	minSyncInterval = 5
	maxSyncInterval = 3600
)

// Config is the content of .beforecode/config.yaml.
type Config struct {
	// RequiredApprovals is the Approval policy: how many Approvals a Plan
	// needs. At least 1.
	RequiredApprovals int
	// SyncEnabled is false in Local mode.
	SyncEnabled bool
	// Remote is the name of the git remote that receives the plans ref. It
	// is set exactly when SyncEnabled is true.
	Remote string
	// SyncInterval is sync.interval: the time between background syncs in
	// the TUI. Zero means not set; use Interval.
	SyncInterval time.Duration
	// ClaudeCommand is claude.command: the command that starts Claude Code.
	// Empty means not set; use Command.
	ClaudeCommand string
	// EnforceOff is claude.enforce: false. The PreToolUse hook then allows
	// edits without an approved plan (ADR 0011).
	EnforceOff bool
}

// Command returns the command that starts Claude Code: ClaudeCommand, or
// DefaultClaudeCommand when it is not set.
func (c Config) Command() string {
	if c.ClaudeCommand == "" {
		return DefaultClaudeCommand
	}
	return c.ClaudeCommand
}

// Enforce reports whether the PreToolUse hook blocks edits without an
// approved plan: claude.enforce, true unless it is set to false.
func (c Config) Enforce() bool { return !c.EnforceOff }

// Interval returns the time between background syncs: SyncInterval, or
// DefaultSyncInterval when it is not set.
func (c Config) Interval() time.Duration {
	if c.SyncInterval == 0 {
		return DefaultSyncInterval
	}
	return c.SyncInterval
}

// file mirrors the YAML layout. Pointers tell a missing key from a zero value.
type file struct {
	Version  *int `yaml:"version"`
	Approval struct {
		RequiredApprovals *int `yaml:"required_approvals"`
	} `yaml:"approval"`
	Sync struct {
		Enabled  *bool   `yaml:"enabled"`
		Remote   *string `yaml:"remote"`
		Interval *int    `yaml:"interval"`
	} `yaml:"sync"`
	Claude *struct {
		Command *string `yaml:"command"`
		Enforce *bool   `yaml:"enforce"`
	} `yaml:"claude"`
}

// Parse reads and validates a config file. Unknown keys are errors.
func Parse(data []byte) (Config, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("%s: %w", Path, err)
	}
	switch {
	case f.Version == nil:
		return Config{}, fmt.Errorf("%s: version is missing", Path)
	case *f.Version > Version:
		return Config{}, fmt.Errorf("%s: version %d is newer than this bfc understands (%d); update bfc", Path, *f.Version, Version)
	case *f.Version < 1:
		return Config{}, fmt.Errorf("%s: unsupported version %d", Path, *f.Version)
	case f.Approval.RequiredApprovals == nil || *f.Approval.RequiredApprovals < 1:
		return Config{}, fmt.Errorf("%s: approval.required_approvals must be an integer of at least 1", Path)
	case f.Sync.Enabled == nil:
		return Config{}, fmt.Errorf("%s: sync.enabled is missing", Path)
	case *f.Sync.Enabled && (f.Sync.Remote == nil || *f.Sync.Remote == ""):
		return Config{}, fmt.Errorf("%s: sync.remote is required when sync.enabled is true", Path)
	case !*f.Sync.Enabled && f.Sync.Remote != nil:
		return Config{}, fmt.Errorf("%s: sync.remote must be absent when sync.enabled is false", Path)
	case f.Sync.Interval != nil && *f.Version < 2:
		return Config{}, fmt.Errorf("%s: sync.interval needs version 2", Path)
	case f.Sync.Interval != nil && !*f.Sync.Enabled:
		return Config{}, fmt.Errorf("%s: sync.interval must be absent when sync.enabled is false", Path)
	case f.Sync.Interval != nil && (*f.Sync.Interval < minSyncInterval || *f.Sync.Interval > maxSyncInterval):
		return Config{}, fmt.Errorf("%s: sync.interval must be between %d and %d seconds", Path, minSyncInterval, maxSyncInterval)
	case f.Claude != nil && *f.Version < 3:
		return Config{}, fmt.Errorf("%s: the claude section needs version 3", Path)
	case f.Claude != nil && f.Claude.Command != nil && *f.Claude.Command == "":
		return Config{}, fmt.Errorf("%s: claude.command must not be empty", Path)
	}
	c := Config{RequiredApprovals: *f.Approval.RequiredApprovals, SyncEnabled: *f.Sync.Enabled}
	if f.Sync.Remote != nil {
		c.Remote = *f.Sync.Remote
	}
	if f.Sync.Interval != nil {
		c.SyncInterval = time.Duration(*f.Sync.Interval) * time.Second
	}
	if f.Claude != nil {
		if f.Claude.Command != nil {
			c.ClaudeCommand = *f.Claude.Command
		}
		c.EnforceOff = f.Claude.Enforce != nil && !*f.Claude.Enforce
	}
	return c, nil
}

// Render writes c as a commented config file.
func Render(c Config) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, `# BeforeCode configuration. Change it through a pull request.
version: %d

approval:
  # Approvals by people other than the plan author needed before a plan is approved.
  required_approvals: %d

sync:
  # false: plans stay in this clone and are never pushed (bfc init --local).
  enabled: %t
`, Version, c.RequiredApprovals, c.SyncEnabled)
	if c.SyncEnabled {
		fmt.Fprintf(&b, "  # The git remote that receives refs/beforecode/plans. Only when enabled.\n  remote: %s\n", quote(c.Remote))
		if c.SyncInterval != 0 {
			fmt.Fprintf(&b, "  # Seconds between background syncs while the TUI is open (5 to 3600).\n  interval: %d\n", int(c.SyncInterval/time.Second))
		}
	}
	fmt.Fprintf(&b, `
claude:
  # The command that starts Claude Code: a name on PATH or an absolute path.
  command: %s
  # false: the PreToolUse hook allows edits without an approved plan.
  enforce: %t
`, quote(c.Command()), c.Enforce())
	return b.Bytes()
}

// quote returns s as a YAML scalar.
func quote(s string) string {
	out, err := yaml.Marshal(s)
	if err != nil {
		panic(err) // a string always marshals
	}
	return string(bytes.TrimSuffix(out, []byte("\n")))
}
