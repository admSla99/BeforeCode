package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/agent/claude"
)

func newSetupCmd() *cobra.Command {
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Set up a harness to work with bfc in this repository",
		Args:  cobra.NoArgs,
	}
	setup.AddCommand(&cobra.Command{
		Use:   "claude",
		Short: "Set up Claude Code: the beforecode skill, the edit hook and permissions",
		Long: `Set up Claude Code for this repository, for everyone who uses it here.

It writes .claude/skills/beforecode/SKILL.md, the skill that sends Claude Code
to bfc prime whenever plans come up; the text ships in bfc and is rewritten on
every run. It adds to .claude/settings.json the PreToolUse hook that blocks
edits without an approved Current plan (bfc hook pre-tool-use), allows the
bfc commands agents use and denies the Gates. Everything else in
.claude/settings.json is kept, and running it again changes nothing.

Nothing is committed and the plans ref is not touched: commit .claude through
a pull request, so the setup reaches the whole team.`,
		Args: cobra.NoArgs,
		RunE: runSetupClaude,
	})
	return setup
}

// hookMatcher names the Claude Code tools the hook checks: the editTools.
const hookMatcher = "Edit|Write|MultiEdit|NotebookEdit"

// agentCommands are the bfc commands Claude Code may run without asking.
var agentCommands = []string{"prime", "show", "list", "ask", "reply", "edit", "start"}

// claudeRules are what bfc setup claude puts into .claude/settings.json.
func claudeRules() claude.Rules {
	r := claude.Rules{Matcher: hookMatcher, Command: "bfc " + claude.HookArgs}
	for _, c := range agentCommands {
		r.Allow = append(r.Allow, "Bash(bfc "+c+":*)")
	}
	for _, c := range gateCommands {
		r.Deny = append(r.Deny, "Bash(bfc "+c+":*)")
	}
	return r
}

func runSetupClaude(cmd *cobra.Command, _ []string) error {
	s, err := openStore(cmd)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	settingsFile := filepath.Join(s.Root(), filepath.FromSlash(claude.SettingsPath))
	skillFile := filepath.Join(s.Root(), filepath.FromSlash(claude.SkillPath))

	// Decide everything before writing anything, so a refusal leaves no trace.
	old, err := readOptional(settingsFile)
	if err != nil {
		return err
	}
	settings, added, err := claude.MergeSettings(old, claudeRules())
	if err != nil {
		return fmt.Errorf("%s: %w; fix it, then run bfc setup claude again", claude.SettingsPath, err)
	}
	oldSkill, err := readOptional(skillFile)
	if err != nil {
		return err
	}

	changed := false
	settingsAction := writeAction(old, settings)
	if settingsAction != "kept" {
		if err := writeCreating(settingsFile, settings); err != nil {
			return err
		}
		changed = true
	}
	report(out, settingsAction, claude.SettingsPath)
	for _, a := range added {
		report(out, "added", a)
	}
	skillAction := writeAction(oldSkill, claude.Skill)
	if skillAction != "kept" {
		if err := writeCreating(skillFile, claude.Skill); err != nil {
			return err
		}
		changed = true
	}
	report(out, skillAction, claude.SkillPath)

	if !changed {
		_, err = fmt.Fprintln(out, "\nClaude Code is already set up; nothing changed.")
		return err
	}
	_, err = fmt.Fprintln(out, "\nNext: commit .claude through a pull request, so the setup reaches everyone who runs Claude Code here:\n"+
		"  git add .claude && git commit -m \"Set up Claude Code for BeforeCode\"")
	return err
}

// readOptional returns the content of the file, nil when it does not exist.
func readOptional(name string) ([]byte, error) {
	data, err := os.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// writeAction says what writing data over old does: created, updated or
// kept. A missing file has old nil.
func writeAction(old, data []byte) string {
	switch {
	case old == nil:
		return "created"
	case bytes.Equal(old, data):
		return "kept"
	default:
		return "updated"
	}
}

// writeCreating writes data to the file, creating its directory.
func writeCreating(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o644)
}
