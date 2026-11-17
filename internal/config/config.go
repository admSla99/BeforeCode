// Package config reads and writes .beforecode/config.yaml, version 1 as
// specified in docs/adr/0003-config-file-v1.md. It works on bytes only.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// Path is where the config file lives, relative to the repository root.
const Path = ".beforecode/config.yaml"

// Version is the only config version this binary understands.
const Version = 1

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
}

// file mirrors the YAML layout. Pointers tell a missing key from a zero value.
type file struct {
	Version  *int `yaml:"version"`
	Approval struct {
		RequiredApprovals *int `yaml:"required_approvals"`
	} `yaml:"approval"`
	Sync struct {
		Enabled *bool   `yaml:"enabled"`
		Remote  *string `yaml:"remote"`
	} `yaml:"sync"`
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
	case *f.Version != Version:
		return Config{}, fmt.Errorf("%s: unsupported version %d", Path, *f.Version)
	case f.Approval.RequiredApprovals == nil || *f.Approval.RequiredApprovals < 1:
		return Config{}, fmt.Errorf("%s: approval.required_approvals must be an integer of at least 1", Path)
	case f.Sync.Enabled == nil:
		return Config{}, fmt.Errorf("%s: sync.enabled is missing", Path)
	case *f.Sync.Enabled && (f.Sync.Remote == nil || *f.Sync.Remote == ""):
		return Config{}, fmt.Errorf("%s: sync.remote is required when sync.enabled is true", Path)
	case !*f.Sync.Enabled && f.Sync.Remote != nil:
		return Config{}, fmt.Errorf("%s: sync.remote must be absent when sync.enabled is false", Path)
	}
	c := Config{RequiredApprovals: *f.Approval.RequiredApprovals, SyncEnabled: *f.Sync.Enabled}
	if f.Sync.Remote != nil {
		c.Remote = *f.Sync.Remote
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
	}
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
