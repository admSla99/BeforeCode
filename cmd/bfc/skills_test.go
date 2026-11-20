package main

import (
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
