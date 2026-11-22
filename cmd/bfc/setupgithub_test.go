package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/github"
)

// bfcVersionIn runs bfc of the given version in dir and returns its
// combined output, like bfcIn for the dev build.
func bfcVersionIn(t *testing.T, dir, version string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd(version)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(inPlace(t, dir))
	return out.String(), err
}

// Issue #104 AC-1, AC-3: bfc setup github writes the workflow of the
// installed release and says what to do next.
func TestSetupGithubWritesTheWorkflowOfThisRelease(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)

	out, err := bfcVersionIn(t, dir, "v0.3.0", "setup", "github")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	file := filepath.Join(dir, filepath.FromSlash(github.WorkflowPath))
	want, _ := github.Workflow("v0.3.0")
	if got := readFile(t, file); got != string(want) {
		t.Errorf("the workflow is not the template of v0.3.0")
	}
	for _, s := range []string{
		"created  " + github.WorkflowPath,
		"pull request", "ANTHROPIC_API_KEY", "gh secret set ANTHROPIC_API_KEY", "plan-mismatch",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	if st := gitIn(t, dir, "status", "--porcelain", ".github"); st != "?? .github/" {
		t.Errorf("git status = %q: setup github must not commit", st)
	}
}

// Issue #104 AC-1: a dev build cannot name a release to download.
func TestSetupGithubRefusesADevBuild(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	out, err := bfcIn(t, dir, "setup", "github")
	if err == nil || !strings.Contains(out, "release") {
		t.Errorf("dev build: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".github")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused setup wrote .github: %v", err)
	}
}

// Issue #104 AC-2: a rerun rewrites the file for the installed version and
// keeps one that someone changed, unless --force, saying how it differs.
func TestSetupGithubRerun(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	file := filepath.Join(dir, filepath.FromSlash(github.WorkflowPath))
	mustVersion := func(version string, args ...string) string {
		t.Helper()
		out, err := bfcVersionIn(t, dir, version, args...)
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return out
	}
	mustVersion("v0.3.0", "setup", "github")

	if out := mustVersion("v0.3.0", "setup", "github"); !strings.Contains(out, "kept     "+github.WorkflowPath) {
		t.Errorf("same version:\n%s", out)
	}
	if out := mustVersion("v0.4.0", "setup", "github"); !strings.Contains(out, "updated  "+github.WorkflowPath+" from bfc v0.3.0 to v0.4.0") {
		t.Errorf("new version:\n%s", out)
	}

	edited := strings.Replace(readFile(t, file), "cancel-in-progress: false", "cancel-in-progress: true", 1)
	writeFile(t, file, edited)
	out, err := bfcVersionIn(t, dir, "v0.5.0", "setup", "github")
	if err == nil || !strings.Contains(out, "was changed by hand") || !strings.Contains(out, "--force") ||
		!strings.Contains(out, "-  cancel-in-progress: true") || !strings.Contains(out, "+  cancel-in-progress: false") {
		t.Errorf("hand-edited file: %v\n%s", err, out)
	}
	if readFile(t, file) != edited {
		t.Errorf("the hand-edited file was replaced without --force")
	}
	mustVersion("v0.5.0", "setup", "github", "--force")
	want, _ := github.Workflow("v0.5.0")
	if readFile(t, file) != string(want) {
		t.Errorf("--force did not replace the file")
	}
}

// Issue #104 AC-4: bfc doctor warns about a missing workflow and one from
// another bfc version.
func TestDoctorChecksThePlanCheckWorkflow(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	out, _ := bfcVersionIn(t, dir, "v0.3.0", "doctor")
	wantDoctor(t, out, "warn", "github workflow", "missing", "bfc setup github")

	if out, err := bfcVersionIn(t, dir, "v0.3.0", "setup", "github"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	out, _ = bfcVersionIn(t, dir, "v0.3.0", "doctor")
	wantDoctor(t, out, "ok", "github workflow", "v0.3.0")
	out, _ = bfcVersionIn(t, dir, "v0.4.0", "doctor")
	wantDoctor(t, out, "warn", "github workflow", "written by bfc v0.3.0", "bfc setup github")

	file := filepath.Join(dir, filepath.FromSlash(github.WorkflowPath))
	writeFile(t, file, readFile(t, file)+"# a local change\n")
	out, _ = bfcVersionIn(t, dir, "v0.3.0", "doctor")
	wantDoctor(t, out, "warn", "github workflow", "changed by hand")

	writeFile(t, file, string(github.SourceWorkflow()))
	out, _ = bfcVersionIn(t, dir, "v0.3.0", "doctor")
	wantDoctor(t, out, "ok", "github workflow", "builds bfc from source")
}

// A source-built workflow, as BeforeCode's own, is replaced only with
// --force; a dev build's bfc doctor does not send anyone to bfc setup
// github, which it cannot run.
func TestSetupGithubKeepsASourceWorkflowAndDoctorOnADevBuild(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	file := filepath.Join(dir, filepath.FromSlash(github.WorkflowPath))
	writeFile(t, file, string(github.SourceWorkflow()))
	out, err := bfcVersionIn(t, dir, "v0.3.0", "setup", "github")
	if err == nil || !strings.Contains(out, "builds bfc from source") || !strings.Contains(out, "--force") {
		t.Errorf("source workflow: %v\n%s", err, out)
	}
	if readFile(t, file) != string(github.SourceWorkflow()) {
		t.Errorf("the source workflow was replaced without --force")
	}

	if out, err := bfcVersionIn(t, dir, "v0.3.0", "setup", "github", "--force"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	out, _ = bfcIn(t, dir, "doctor")
	wantDoctor(t, out, "ok", "github workflow", "runs bfc v0.3.0", "dev build")
}
