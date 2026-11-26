package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	beforecode "github.com/admSla99/BeforeCode"
)

// repoSkills is this repository's own .beforecode/skills, the source of the
// skills built into bfc.
var repoSkills = filepath.Join("..", "..", ".beforecode", "skills")

// AC-1, AC-5: the skills built into bfc are this repository's
// .beforecode/skills, file for file, and grill-intent is one of them.
func TestEmbeddedSkillsMatchTheRepository(t *testing.T) {
	t.Parallel()
	var embedded []string
	err := fs.WalkDir(beforecode.Skills, ".beforecode/skills", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		embedded = append(embedded, name)
		data, _ := beforecode.Skills.ReadFile(name)
		rel := strings.TrimPrefix(name, ".beforecode/skills/")
		if want := readFile(t, filepath.Join(repoSkills, filepath.FromSlash(rel))); string(data) != want {
			t.Errorf("embedded %s differs from the repository's", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk := 0
	_ = filepath.WalkDir(repoSkills, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			onDisk++
		}
		return err
	})
	if len(embedded) != onDisk {
		t.Errorf("bfc embeds %d skill files, the repository has %d: %v", len(embedded), onDisk, embedded)
	}
	if _, err := beforecode.Skills.ReadFile(".beforecode/skills/grill-intent/SKILL.md"); err != nil {
		t.Errorf("grill-intent is not built in: %v", err)
	}
}

// AC-2: bfc init writes the default skills and keeps a skill the
// repository already has.
func TestInitWritesTheDefaultSkills(t *testing.T) {
	t.Parallel()
	clone, _ := newClone(t)
	own := filepath.Join(clone, ".beforecode", "skills", "grill-intent", "SKILL.md")
	writeFile(t, own, "our own grill\n")

	out := mustBfc(t, clone, "init")
	if got := readFile(t, own); got != "our own grill\n" {
		t.Errorf("init replaced the repository's skill: %q", got)
	}
	if !strings.Contains(out, "kept     .beforecode/skills/grill-intent/SKILL.md") {
		t.Errorf("init does not say it kept the skill:\n%s", out)
	}

	fresh, _ := newClone(t)
	out = mustBfc(t, fresh, "init")
	want := readFile(t, filepath.Join(repoSkills, "grill-intent", "SKILL.md"))
	if got := readFile(t, filepath.Join(fresh, ".beforecode", "skills", "grill-intent", "SKILL.md")); got != want {
		t.Errorf("init wrote a different grill-intent:\n%s", got)
	}
	if !strings.Contains(out, "created  .beforecode/skills/grill-intent/SKILL.md") {
		t.Errorf("init does not report the skill:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(fresh, ".beforecode", "templates", "plan.md")); err != nil {
		t.Errorf("templates are still written: %v", err)
	}
}

// AC-2: bfc doctor reports a missing skill directory and how to restore it.
func TestDoctorReportsAMissingSkillDirectory(t *testing.T) {
	t.Parallel()
	repo := newLocalRepo(t)
	wantDoctor(t, mustBfc(t, repo, "doctor"), "ok", "skills", "grill-intent")

	if err := os.RemoveAll(filepath.Join(repo, ".beforecode", "skills")); err != nil {
		t.Fatal(err)
	}
	// A warning: bfc falls back to the skills built into it.
	wantDoctor(t, mustBfc(t, repo, "doctor"), "warn", "skills", ".beforecode/skills is missing", "built into bfc", "bfc init")
}

// Issue #102 AC-1, AC-5: bfc init writes plan-check, and bfc doctor warns
// when a repository set up before it lacks a built-in skill.
func TestDoctorCoversThePlanCheckSkill(t *testing.T) {
	t.Parallel()
	repo := newLocalRepo(t)
	if _, err := os.Stat(filepath.Join(repo, ".beforecode", "skills", "plan-check", "SKILL.md")); err != nil {
		t.Fatalf("bfc init did not write plan-check: %v", err)
	}
	wantDoctor(t, mustBfc(t, repo, "doctor"), "ok", "skills", "grill-intent, implement-task, plan-check, write-plan")

	if err := os.RemoveAll(filepath.Join(repo, ".beforecode", "skills", "plan-check")); err != nil {
		t.Fatal(err)
	}
	wantDoctor(t, mustBfc(t, repo, "doctor"), "warn", "skills", "plan-check is missing", "built into bfc", "bfc init")
}

// Issue #138: a repository set up before ADR 0015 keeps the plan-check
// skill of the GitHub Actions run, which wins over the built-in one; bfc
// doctor says so and how to take the new one.
func TestDoctorWarnsAboutAPlanCheckSkillFromBeforeTheLocalRun(t *testing.T) {
	t.Parallel()
	repo := newLocalRepo(t)
	writeFile(t, filepath.Join(repo, ".beforecode", "skills", "plan-check", "SKILL.md"),
		"---\nname: plan-check\n---\n\nEnd with the result in a json fence.\n")
	wantDoctor(t, mustBfc(t, repo, "doctor"), "warn", "skills", "plan-check", "bfc check record", "delete")
}

// bfc-nxpr AC-7 to AC-10: the implement-task skill is built in and holds
// the steps in order, each with "Done when", the rules for a failing check
// or a missing decision, the Handoff headings and the pull-request step.
func TestImplementTaskSkillIsBuiltIn(t *testing.T) {
	t.Parallel()
	data, err := beforecode.Skills.ReadFile(implementSkill)
	if err != nil {
		t.Fatalf("implement-task is not built in: %v", err)
	}
	skill := string(data)

	// AC-7: the numbered steps, in this order.
	steps := []string{"Current plan", "bfc pull", "bfc claim", "Read", "tests first", "Implement", "checks", "Commit", "bfc handoff", "bfc push", "Stop"}
	at := 0
	for i, s := range steps {
		head := fmt.Sprintf("\n%d. **", i+1)
		j := strings.Index(skill[at:], head)
		if j < 0 {
			t.Fatalf("step %d is missing", i+1)
		}
		at += j + len(head)
		end := strings.Index(skill[at:], "\n"+fmt.Sprint(i+2)+". **")
		if end < 0 {
			end = strings.Index(skill[at:], "\n## ")
		}
		step := skill[at : at+end]
		if !strings.Contains(step, s) {
			t.Errorf("step %d does not cover %q:\n%s", i+1, s, step)
		}
		if !strings.Contains(step, "Done when") {
			t.Errorf("step %d has no \"Done when\"", i+1)
		}
	}
	for _, want := range []string{
		// AC-7
		"only when the user asked", "CLAUDE.md", "CONTRIBUTING.md", "push the code branch",
		// AC-8
		"without `--done`", "bfc ask <id> --to <plan author email>", "Never hand off with `--done` while a check fails", "one Task per turn",
		// AC-9
		"one-line summary", "## Changed", "## Acceptance criteria", "## Tests", "## Left", "## Questions",
		// AC-10
		"gh pr create", "Plan: <Short ID>", "Never merge", "Gate",
	} {
		if !strings.Contains(skill, want) {
			t.Errorf("implement-task does not say %q", want)
		}
	}
}
