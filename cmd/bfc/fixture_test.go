package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fixtureRoot is the directory that holds the fixtures of this package run;
// TestMain sets it and removes it at the end.
var fixtureRoot string

// A fixture is a set of repositories made once per package run, the first
// time a test needs it. Each test gets its own copy, so tests share the cost
// of the setup but not the repositories.
type fixture struct {
	once sync.Once
	dir  string
	err  error
}

var fixtures sync.Map // name → *fixture

// copyFixture makes the fixture name with build if it does not exist yet,
// and copies it into a new temporary directory of the test, which it
// returns. build gets an empty directory and must not use a *testing.T,
// whose cleanup would remove the fixture after that test. Remote URLs
// must be written with forward slashes: copyFixture rewrites the fixture's
// path to the copy's in every git config file.
func copyFixture(t *testing.T, name string, build func(dir string) error) string {
	t.Helper()
	v, _ := fixtures.LoadOrStore(name, &fixture{})
	f := v.(*fixture)
	f.once.Do(func() {
		f.dir = filepath.Join(fixtureRoot, name)
		if f.err = os.Mkdir(f.dir, 0o755); f.err == nil {
			f.err = build(f.dir)
		}
	})
	if f.err != nil {
		t.Fatalf("fixture %s: %v", name, f.err)
	}
	dst := t.TempDir()
	if err := copyTree(f.dir, dst); err != nil {
		t.Fatalf("copy fixture %s: %v", name, err)
	}
	return dst
}

// copyTree copies the files under src to dst, replacing the path src with
// dst (both with forward slashes) in files named config.
func copyTree(src, dst string) error {
	from, to := []byte(filepath.ToSlash(src)), []byte(filepath.ToSlash(dst))
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if d.Name() == "config" {
			data = bytes.ReplaceAll(data, from, to)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

// gitSteps runs git commands in order and stops at the first failure.
func gitSteps(steps ...[]string) error {
	for _, s := range steps {
		if _, err := gitTry(s[0], s[1:]...); err != nil {
			return err
		}
	}
	return nil
}

// bfcRun runs bfc with args in dir, with the environment of the process,
// and returns its combined output.
func bfcRun(dir string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := newRootCmd("dev")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(withPlace(context.Background(), place{dir: dir}))
	return out.String(), err
}

// bfcSteps runs bfc commands in order, each in the directory given first,
// and stops at the first failure.
func bfcSteps(steps ...[]string) error {
	for _, s := range steps {
		if out, err := bfcRun(s[0], s[1:]...); err != nil {
			return fmt.Errorf("bfc %s: %w\n%s", strings.Join(s[1:], " "), err, out)
		}
	}
	return nil
}

// buildLocalRepo makes repo, a repository set up with bfc init --local.
func buildLocalRepo(dir string) error {
	repo := filepath.Join(dir, "repo")
	if err := gitSteps(
		[]string{dir, "init", "-q", "-b", "main", repo},
		[]string{repo, "config", "user.name", "Ada Lovelace"},
		[]string{repo, "config", "user.email", "ada@example.com"},
	); err != nil {
		return err
	}
	return bfcSteps([]string{repo, "init", "--local"})
}

// buildClone makes remote.git, a bare repository standing in for GitHub,
// and work, a clone of it with one commit.
func buildClone(dir string) error {
	bare := filepath.ToSlash(filepath.Join(dir, "remote.git"))
	clone := filepath.Join(dir, "work")
	if err := gitSteps(
		[]string{dir, "init", "-q", "--bare", "-b", "main", bare},
		[]string{dir, "init", "-q", "-b", "main", clone},
		[]string{clone, "config", "user.name", "Ada Lovelace"},
		[]string{clone, "config", "user.email", "ada@example.com"},
		[]string{clone, "remote", "add", "origin", bare},
	); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("hello\n"), 0o600); err != nil {
		return err
	}
	return gitSteps(
		[]string{clone, "add", "README.md"},
		[]string{clone, "commit", "-q", "-m", "initial"},
		[]string{clone, "push", "-q", "origin", "main"},
	)
}

// buildTwoClones makes the clone of buildClone set up with bfc init and
// pushed, and ben, a second clone that pulled it.
func buildTwoClones(dir string) error {
	if err := buildClone(dir); err != nil {
		return err
	}
	ana, ben := filepath.Join(dir, "work"), filepath.Join(dir, "ben")
	if err := bfcSteps([]string{ana, "init"}, []string{ana, "push"}); err != nil {
		return err
	}
	if err := buildBenClone(dir); err != nil {
		return err
	}
	out, err := bfcRun(ben, "pull")
	if err == nil && !strings.Contains(out, "took the plans of origin") {
		err = fmt.Errorf("bfc pull: %s", out)
	}
	return err
}

// buildBenClone makes ben, a plain clone of remote.git with Ben's identity.
func buildBenClone(dir string) error {
	ben := filepath.Join(dir, "ben")
	return gitSteps(
		[]string{dir, "clone", "-q", filepath.ToSlash(filepath.Join(dir, "remote.git")), ben},
		[]string{ben, "config", "user.name", "Ben Bitdiddle"},
		[]string{ben, "config", "user.email", "ben@example.com"},
	)
}
