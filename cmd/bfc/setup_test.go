package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/agent/claude"
)

// AC-1, AC-2, AC-4, AC-6: in a repository without .claude, bfc setup claude
// writes the settings and the skill and says what it did.
func TestSetupClaudeInANewRepository(t *testing.T) {
	t.Parallel()
	repo := newLocalRepo(t)
	tip := gitIn(t, repo, "rev-parse", plansRef)

	out := mustBfc(t, repo, "setup", "claude")
	golden(t, out)
	t.Run("settings", func(t *testing.T) {
		golden(t, readFile(t, filepath.Join(repo, ".claude", "settings.json")))
	})
	t.Run("skill", func(t *testing.T) {
		got := readFile(t, filepath.Join(repo, ".claude", "skills", "beforecode", "SKILL.md"))
		if got != string(claude.Skill) {
			t.Errorf("SKILL.md is not the skill in the binary:\n%s", got)
		}
		golden(t, got)
	})
	if after := gitIn(t, repo, "rev-parse", plansRef); after != tip {
		t.Errorf("setup changed the plans ref: %s → %s", tip, after)
	}
	if status := gitIn(t, repo, "status", "--porcelain", "--untracked-files=all"); !strings.Contains(status, "?? .claude/settings.json") {
		t.Errorf("setup must leave .claude uncommitted, git status:\n%s", status)
	}
}

// AC-3, AC-6: an existing settings file keeps its other keys, hooks and
// permissions, in their order; a second run changes nothing; other skills
// stay as they are.
func TestSetupClaudeMergesExistingSettings(t *testing.T) {
	t.Parallel()
	repo := newLocalRepo(t)
	settings := filepath.Join(repo, ".claude", "settings.json")
	writeFile(t, settings, `{
  "model": "opus",
  "permissions": {
    "allow": ["Bash(go test:*)", "Bash(bfc show:*)"],
    "defaultMode": "acceptEdits"
  },
  "hooks": {
    "PostToolUse": [{"matcher": "Edit", "hooks": [{"type": "command", "command": "gofmt -w ."}]}],
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "./check <cmd> && true"}]}]
  },
  "env": {"GOFLAGS": "-mod=mod"}
}
`)
	other := filepath.Join(repo, ".claude", "skills", "release", "SKILL.md")
	writeFile(t, other, "# Release\n")
	skill := filepath.Join(repo, ".claude", "skills", "beforecode", "SKILL.md")
	writeFile(t, skill, "an older skill\n")

	out := mustBfc(t, repo, "setup", "claude")
	golden(t, out)
	merged := readFile(t, settings)
	t.Run("settings", func(t *testing.T) { golden(t, merged) })
	if got := readFile(t, skill); got != string(claude.Skill) {
		t.Errorf("the beforecode skill was not rewritten:\n%s", got)
	}
	if got := readFile(t, other); got != "# Release\n" {
		t.Errorf("another skill changed: %q", got)
	}

	again := mustBfc(t, repo, "setup", "claude")
	if got := readFile(t, settings); got != merged {
		t.Errorf("a second run changed the settings:\n%s", got)
	}
	for _, line := range strings.Split(strings.TrimSpace(again), "\n") {
		if strings.HasPrefix(line, "added") || strings.HasPrefix(line, "updated") || strings.HasPrefix(line, "created") {
			t.Errorf("a second run reports a change: %q\n%s", line, again)
		}
	}
	if !strings.Contains(again, "Nothing changed") || !strings.Contains(again, "pull request") {
		t.Errorf("a second run should say nothing changed and remind of the pull request:\n%s", again)
	}
}

// AC-3: a hook bfc runs through another path counts as bfc's hook.
func TestSetupClaudeKeepsTheHookWithAPathToBfc(t *testing.T) {
	t.Parallel()
	repo := newLocalRepo(t)
	settings := filepath.Join(repo, ".claude", "settings.json")
	writeFile(t, settings, `{"hooks": {"PreToolUse": [{"matcher": "`+claude.HookMatcher+`", "hooks": [{"type": "command", "command": "\"C:/Program Files/bfc/bfc.exe\" hook pre-tool-use"}]}]}}`)

	out := mustBfc(t, repo, "setup", "claude")
	if strings.Contains(out, "hooks.PreToolUse") {
		t.Errorf("setup added a second hook:\n%s", out)
	}
	if got := readFile(t, settings); strings.Count(got, "hook pre-tool-use") != 1 {
		t.Errorf("want one hook:\n%s", got)
	}
}

// AC-3: settings that are not valid JSON are refused with the file name,
// and nothing is written.
func TestSetupClaudeRefusesInvalidSettings(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"not JSON":        `{"hooks": `,
		"not an object":   `[]`,
		"hooks no object": `{"hooks": []}`,
		"allow no array":  `{"permissions": {"allow": "Bash(bfc prime:*)"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := newLocalRepo(t)
			settings := filepath.Join(repo, ".claude", "settings.json")
			writeFile(t, settings, content)

			out, err := bfcIn(t, repo, "setup", "claude")
			if err == nil || !strings.Contains(out, ".claude/settings.json") {
				t.Errorf("want a refusal naming .claude/settings.json, got %v:\n%s", err, out)
			}
			if got := readFile(t, settings); got != content {
				t.Errorf("settings changed: %s", got)
			}
			if _, err := os.Stat(filepath.Join(repo, ".claude", "skills")); err == nil {
				t.Error("the skill was written despite the refusal")
			}
		})
	}
}

// The hook checks exactly the tools that bfc hook pre-tool-use treats as
// edits.
func TestHookMatcherNamesTheEditTools(t *testing.T) {
	t.Parallel()
	got := strings.Split(claude.HookMatcher, "|")
	slices.Sort(got)
	if want := slices.Sorted(maps.Keys(editTools)); !slices.Equal(got, want) {
		t.Errorf("claude.HookMatcher names %v, the hook checks %v", got, want)
	}
}

// AC-3: bfc's hook with a matcher that leaves out edit tools gets the full
// matcher, not a second entry.
func TestSetupClaudeWidensTheMatcherOfTheHook(t *testing.T) {
	t.Parallel()
	repo := newLocalRepo(t)
	settings := filepath.Join(repo, ".claude", "settings.json")
	writeFile(t, settings, `{"hooks": {"PreToolUse": [{"matcher": "Write|Edit", "hooks": [{"type": "command", "command": "bfc hook pre-tool-use", "timeout": 5}]}]}}`)

	out := mustBfc(t, repo, "setup", "claude")
	if want := `updated  hooks.PreToolUse bfc hook pre-tool-use for ` + claude.HookMatcher + `, was "Write|Edit"`; !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
	got := readFile(t, settings)
	if strings.Count(got, "hook pre-tool-use") != 1 || !strings.Contains(got, `"matcher": "`+claude.HookMatcher+`"`) || !strings.Contains(got, `"timeout": 5`) {
		t.Errorf("want one hook with the full matcher and its own keys:\n%s", got)
	}
}

// AC-3: a checkout with CRLF line endings keeps the skill.
func TestSetupClaudeKeepsTheSkillWithCRLF(t *testing.T) {
	t.Parallel()
	repo := newLocalRepo(t)
	mustBfc(t, repo, "setup", "claude")
	skill := filepath.Join(repo, ".claude", "skills", "beforecode", "SKILL.md")
	crlf := strings.ReplaceAll(string(claude.Skill), "\n", "\r\n")
	writeFile(t, skill, crlf)

	out := mustBfc(t, repo, "setup", "claude")
	if !strings.Contains(out, "kept     "+claude.SkillPath) {
		t.Errorf("want the skill kept:\n%s", out)
	}
	if got := readFile(t, skill); got != crlf {
		t.Error("the skill was rewritten")
	}
}
