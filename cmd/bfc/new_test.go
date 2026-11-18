package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// newLocalRepo creates a repository set up with bfc init --local.
func newLocalRepo(t *testing.T) string {
	t.Helper()
	return filepath.Join(copyFixture(t, "local", buildLocalRepo), "repo")
}

// fakeEditor makes git (and so bfc) use a shell snippet as the editor. The
// snippet gets the file to edit as $1. bfc starts editors through sh, which
// on Windows is the sh.exe of Git for Windows, so this runs there too.
func fakeEditor(t *testing.T, snippet string) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+snippet+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	// The editor setting is shell text: quote the path and use forward
	// slashes, as a Windows user would in core.editor.
	setEnv(t, "GIT_EDITOR", `"`+filepath.ToSlash(script)+`"`)
}

// planDirs lists the plan directories in the plans ref.
func planDirs(t *testing.T, dir string) []string {
	t.Helper()
	out, err := gitTry(dir, "ls-tree", "--name-only", plansRef+":plans")
	if err != nil {
		return nil
	}
	return strings.Fields(out)
}

var shortIDLine = regexp.MustCompile(`^(bfc-[0-9a-z]{4,16})  (.*)$`)

func TestNewWithEditorCommitsTheSavedIntentAndACreatedEvent(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	before := gitIn(t, dir, "rev-parse", plansRef)
	fakeEditor(t, `printf 'Cards fail at checkout.\n' >> "$1"`)

	out, err := bfcIn(t, dir, "new", "Payments via Stripe")
	if err != nil {
		t.Fatalf("bfc new: %v\n%s", err, out)
	}

	// One commit on top of the previous tip.
	after := gitIn(t, dir, "rev-parse", plansRef)
	if parent := gitIn(t, dir, "rev-parse", after+"^"); parent != before {
		t.Errorf("new commit's parent = %s, want %s", parent, before)
	}
	plans := planDirs(t, dir)
	if len(plans) != 1 {
		t.Fatalf("plans = %v, want one", plans)
	}
	id, err := ulid.ParseStrict(plans[0])
	if err != nil {
		t.Fatalf("plan directory %q is not a ULID: %v", plans[0], err)
	}

	intent := gitIn(t, dir, "show", plansRef+":plans/"+plans[0]+"/intent.md")
	if !strings.Contains(intent, "title: Payments via Stripe") || !strings.HasSuffix(intent, "Cards fail at checkout.") {
		t.Errorf("intent.md is not the saved file:\n%s", intent)
	}
	if !strings.Contains(intent, "## Who it is for") {
		t.Errorf("intent.md was not made from the template:\n%s", intent)
	}

	events := strings.Fields(gitIn(t, dir, "ls-tree", "--name-only", plansRef+":plans/"+plans[0]+"/events"))
	if len(events) != 1 {
		t.Fatalf("events = %v, want one", events)
	}
	eid := ulid.MustParseStrict(strings.TrimSuffix(events[0], ".yaml"))
	e, err := core.ParseEvent(eid, []byte(gitIn(t, dir, "show", plansRef+":plans/"+plans[0]+"/events/"+events[0])))
	if err != nil {
		t.Fatal(err)
	}
	if e.Type != core.Created || e.Plan != id || e.Author != (core.Person{Name: "Ada Lovelace", Email: "ada@example.com"}) {
		t.Errorf("event = %+v", e)
	}

	// AC-2: the Short ID is printed and matches the event and the ULID.
	m := shortIDLine.FindStringSubmatch(strings.Split(out, "\n")[0])
	if m == nil {
		t.Fatalf("first line of output is not \"<short id>  <title>\":\n%s", out)
	}
	if m[1] != e.ShortID || m[1] != core.ShortID(id, nil) || m[2] != "Payments via Stripe" {
		t.Errorf("printed %q %q, event short_id %q, ULID %s", m[1], m[2], e.ShortID, id)
	}
}

func TestNewNoEditUsesTheTemplateWithTheTitle(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	fakeEditor(t, `echo "editor must not run" >&2; exit 1`)

	out, err := bfcIn(t, dir, "new", "--no-edit", "Payments", "via", "Stripe")
	if err != nil {
		t.Fatalf("bfc new --no-edit: %v\n%s", err, out)
	}

	plans := planDirs(t, dir)
	if len(plans) != 1 {
		t.Fatalf("plans = %v, want one", plans)
	}
	intent := gitIn(t, dir, "show", plansRef+":plans/"+plans[0]+"/intent.md")
	if !strings.Contains(intent, "title: Payments via Stripe") {
		t.Errorf("intent.md:\n%s", intent)
	}
}

func TestNewUsesTheRepositoryTemplate(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	writeFile(t, filepath.Join(dir, ".beforecode", "templates", "intent.md"), "---\ntitle: \nlabels: [team-x]\n---\n\n## Our own template\n")

	if out, err := bfcIn(t, dir, "new", "--no-edit", "Payments"); err != nil {
		t.Fatalf("bfc new: %v\n%s", err, out)
	}

	intent := gitIn(t, dir, "show", plansRef+":plans/"+planDirs(t, dir)[0]+"/intent.md")
	if !strings.Contains(intent, "## Our own template") || !strings.Contains(intent, "team-x") {
		t.Errorf("intent.md did not come from the repository template:\n%s", intent)
	}
}

func TestNewAbortsWithoutAPlanWhenTheIntentIsUnchangedOrEmpty(t *testing.T) {
	t.Parallel()
	for name, snippet := range map[string]string{
		"unchanged": `true`,
		"emptied":   `: > "$1"`,
		"blank":     `printf '\n  \n' > "$1"`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := newLocalRepo(t)
			tip := gitIn(t, dir, "rev-parse", plansRef)
			fakeEditor(t, snippet)

			out, err := bfcIn(t, dir, "new", "Payments")

			if err == nil {
				t.Fatalf("bfc new succeeded:\n%s", out)
			}
			if !strings.Contains(out, "no plan created") {
				t.Errorf("output does not say no plan was created:\n%s", out)
			}
			if got := gitIn(t, dir, "rev-parse", plansRef); got != tip {
				t.Errorf("plans ref moved")
			}
		})
	}
}

func TestNewWithATitleRemovedInTheEditorKeepsTheText(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	fakeEditor(t, `printf -- '---\ntitle: \n---\n\nMy long thoughts.\n' > "$1"`)

	out, err := bfcIn(t, dir, "new", "Payments")

	if err == nil {
		t.Fatalf("bfc new succeeded without a title:\n%s", out)
	}
	if !strings.Contains(out, "title") {
		t.Errorf("output does not name the problem:\n%s", out)
	}
	m := regexp.MustCompile(`(\S+\.md)`).FindAllString(out, -1)
	kept := ""
	for _, p := range m {
		if data, err := os.ReadFile(p); err == nil && strings.Contains(string(data), "My long thoughts.") {
			kept = p
		}
	}
	if kept == "" {
		t.Errorf("output does not point to a file with the edited text:\n%s", out)
	}
	if len(planDirs(t, dir)) != 0 {
		t.Error("a plan was created")
	}
}

func TestNewBeforeInitPointsToInitAndBootstrap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")

	out, err := bfcIn(t, dir, "new", "--no-edit", "Payments")

	if err == nil {
		t.Fatalf("bfc new succeeded before init:\n%s", out)
	}
	for _, want := range []string{"bfc init", "bfc bootstrap"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
}

func TestNewRefusesToWriteAnotherFormatVersion(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	// Rewrite format-version to 2 through plumbing, as a newer bfc would.
	tree := gitInStdin(t, dir, "100644 blob "+gitInStdin(t, dir, "2\n", "hash-object", "-w", "--stdin")+"\tformat-version\n", "mktree")
	commit := gitIn(t, dir, "commit-tree", tree, "-p", plansRef, "-m", "newer bfc")
	gitIn(t, dir, "update-ref", plansRef, commit)

	out, err := bfcIn(t, dir, "new", "--no-edit", "Payments")

	if err == nil {
		t.Fatalf("bfc new wrote to a format-version 2 ref:\n%s", out)
	}
	if !strings.Contains(out, "format-version 2") || !strings.Contains(out, "update bfc") {
		t.Errorf("output does not explain:\n%s", out)
	}
}

func TestShortIDsOfTwoPlansDiffer(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	var ids []string
	for _, title := range []string{"One", "Two"} {
		out, err := bfcIn(t, dir, "new", "--no-edit", title)
		if err != nil {
			t.Fatalf("bfc new %s: %v\n%s", title, err, out)
		}
		ids = append(ids, strings.Fields(out)[0])
	}
	if ids[0] == ids[1] {
		t.Errorf("both plans got Short ID %s", ids[0])
	}
	if n := len(planDirs(t, dir)); n != 2 {
		t.Errorf("%d plans in the ref, want 2", n)
	}
}

func TestNewWithoutARepositoryTemplateUsesTheBuiltInOne(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	if err := os.RemoveAll(filepath.Join(dir, ".beforecode", "templates")); err != nil {
		t.Fatal(err)
	}

	if out, err := bfcIn(t, dir, "new", "--no-edit", "Payments"); err != nil {
		t.Fatalf("bfc new: %v\n%s", err, out)
	}

	intent := gitIn(t, dir, "show", plansRef+":plans/"+planDirs(t, dir)[0]+"/intent.md")
	if !strings.Contains(intent, "## How we know it worked") {
		t.Errorf("intent.md was not made from the built-in template:\n%s", intent)
	}
}

func TestNewAcceptsAnIntentSavedWithCRLFAndStoresLF(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	fakeEditor(t, `printf -- '---\r\ntitle: Payments\r\n---\r\n\r\nCards fail.\r\n' > "$1"`)

	out, err := bfcIn(t, dir, "new", "Payments")
	if err != nil {
		t.Fatalf("bfc new: %v\n%s", err, out)
	}

	intent := gitIn(t, dir, "show", plansRef+":plans/"+planDirs(t, dir)[0]+"/intent.md")
	if strings.Contains(intent, "\r") || !strings.Contains(intent, "title: Payments\n") {
		t.Errorf("intent.md not stored with LF line endings: %q", intent)
	}
}
