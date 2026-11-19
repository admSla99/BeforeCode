package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/config"
	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

func newHookCmd() *cobra.Command {
	hook := &cobra.Command{
		Use:   "hook",
		Short: "Hooks that a harness runs (bfc setup claude installs them)",
		Args:  cobra.NoArgs,
	}
	hook.AddCommand(&cobra.Command{
		Use:   "pre-tool-use",
		Short: "Claude Code PreToolUse hook: no edits without an approved Current plan",
		Args:  cobra.NoArgs,
		RunE:  runPreToolUse,
	})
	return hook
}

// editTools are the Claude Code tools that change files, each with the
// field of its input that names the file. Every other tool, Bash included,
// is allowed (ADR 0011).
var editTools = map[string]string{
	"Edit":         "file_path",
	"Write":        "file_path",
	"MultiEdit":    "file_path",
	"NotebookEdit": "notebook_path",
}

// preToolUse is the part of the Claude Code PreToolUse input the hook reads.
type preToolUse struct {
	Cwd       string         `json:"cwd"`
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
}

// exemptDirs are the directories of the repository root whose files are
// edited without an approved plan: bfc's and the harness's own setup.
var exemptDirs = []string{".beforecode", ".claude"}

func runPreToolUse(cmd *cobra.Command, _ []string) error {
	var in preToolUse
	if err := json.NewDecoder(cmd.InOrStdin()).Decode(&in); err != nil {
		return deny(cmd, fmt.Sprintf("bfc hook pre-tool-use cannot read the PreToolUse input: %v", err))
	}
	field, ok := editTools[in.ToolName]
	if !ok {
		return nil
	}
	s, err := openStore(cmd)
	if err != nil {
		return denyBroken(cmd, err)
	}
	cwd := in.Cwd
	if cwd == "" {
		if cwd, err = workDir(cmd); err != nil {
			return denyBroken(cmd, err)
		}
	}
	path, _ := in.ToolInput[field].(string)
	if !needsPlan(s.Root(), cwd, path) {
		return nil
	}
	switch cfg, _, err := readConfig(s); {
	case err != nil:
		return denyBroken(cmd, fmt.Errorf("%s: %w", config.Path, err))
	case !cfg.Enforce():
		return nil
	}
	// Not openPlans: it asks the remote about a missing plans ref, and the
	// hook reads only this clone.
	switch err := checkPlansRef(cmd, s, forReading); {
	case errors.Is(err, store.ErrNotInitialised):
		return denyBroken(cmd, errNotInitialised)
	case err != nil:
		return denyBroken(cmd, err)
	}
	p, err := currentPlan(cmd.Context(), s)
	var planErr currentPlanError
	switch {
	case errors.As(err, &planErr):
		return deny(cmd, planErr.Error()+"; bfc prime says more")
	case err != nil:
		return denyBroken(cmd, err)
	case p.Status.State != core.StateApproved:
		return deny(cmd, fmt.Sprintf("the Current plan %s  %s is %s, and code is edited only for an approved plan: get it approved (run bfc pull if it was approved elsewhere), or bfc start <id> to work on another plan; bfc prime says more", p.ShortID, p.Title, p.Status.State))
	}
	return nil
}

// denyBroken blocks the tool call because the hook cannot read the setup.
func denyBroken(cmd *cobra.Command, err error) error {
	reason := err.Error()
	if !strings.Contains(reason, "bfc doctor") {
		reason += "; run bfc doctor"
	}
	return deny(cmd, "bfc hook pre-tool-use cannot tell whether the Current plan is approved: "+reason)
}

// deny writes the Claude Code decision that blocks the tool call.
func deny(cmd *cobra.Command, reason string) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetEscapeHTML(false) // keep <id> readable
	return enc.Encode(map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": reason,
	}})
}

// needsPlan reports whether editing path needs an approved plan: it is in
// the repository at root and not under one of exemptDirs. A relative path
// is relative to cwd, the session's directory.
func needsPlan(root, cwd, path string) bool {
	if path == "" {
		return true
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	rel, err := filepath.Rel(realPath(root), realPath(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false // outside the repository, or on another volume
	}
	first, rest, nested := strings.Cut(rel, string(filepath.Separator))
	if !nested || rest == "" {
		return true
	}
	for _, d := range exemptDirs {
		if first == d || caseInsensitiveFS && strings.EqualFold(first, d) {
			return false
		}
	}
	return true
}

// caseInsensitiveFS is true where file names usually ignore case.
const caseInsensitiveFS = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

// realPath returns the clean path with the symbolic links of its longest
// existing part resolved, so that a file that does not exist yet compares
// like the repository root git reports.
func realPath(path string) string {
	path = filepath.Clean(path)
	var rest []string
	for p := path; ; {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(append([]string{real}, rest...)...)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return path
		}
		rest = append([]string{filepath.Base(p)}, rest...)
		p = parent
	}
}
