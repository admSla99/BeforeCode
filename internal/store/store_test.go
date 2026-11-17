package store_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/store"
)

// TestMain isolates every git subprocess from the developer's own git
// configuration, so the tests behave the same on every machine.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bfc-store-test-home")
	if err != nil {
		panic(err)
	}
	empty := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		panic(err)
	}
	for k, v := range map[string]string{"GIT_CONFIG_GLOBAL": empty, "GIT_CONFIG_NOSYSTEM": "1"} {
		if err := os.Setenv(k, v); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// newRepo creates a temporary git repository with one commit on main and an
// author configured, and returns its path.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.name", "Ada Lovelace")
	git(t, dir, "config", "user.email", "ada@example.com")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "README.md")
	git(t, dir, "commit", "-q", "-m", "initial")
	return dir
}

// git runs git in dir and returns its trimmed standard output.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("git %v: %v\n%s", args, err, exitErr.Stderr)
		}
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func open(t *testing.T, dir string) *store.Store {
	t.Helper()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestReadBeforeAnyWriteReportsNotInitialised(t *testing.T) {
	s := open(t, newRepo(t))

	_, err := s.Read(context.Background(), "intent.md")
	if !errors.Is(err, store.ErrNotInitialised) {
		t.Fatalf("Read error = %v, want ErrNotInitialised", err)
	}
}

func TestFirstWriteCreatesRootCommitWithMessage(t *testing.T) {
	dir := newRepo(t)
	s := open(t, dir)

	commit, err := s.Write(context.Background(), "Create plan", map[string][]byte{
		"plans/a/intent.md": []byte("# Intent\n"),
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := git(t, dir, "rev-parse", store.Ref); got != commit {
		t.Errorf("ref points to %s, Write returned %s", got, commit)
	}
	if parents := git(t, dir, "rev-list", "--parents", "-n", "1", commit); parents != commit {
		t.Errorf("first commit has parents: %q", parents)
	}
	if msg := git(t, dir, "log", "-1", "--format=%B", commit); msg != "Create plan" {
		t.Errorf("message = %q, want %q", msg, "Create plan")
	}
	got, err := s.Read(context.Background(), "plans/a/intent.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "# Intent\n" {
		t.Errorf("Read = %q, want %q", got, "# Intent\n")
	}
}

func TestWriteAddsOneCommitOnTopOfThePreviousTipAndKeepsOtherFiles(t *testing.T) {
	dir := newRepo(t)
	s := open(t, dir)
	ctx := context.Background()
	first, err := s.Write(ctx, "first", map[string][]byte{"a/intent.md": []byte("v1\n")})
	if err != nil {
		t.Fatal(err)
	}

	second, err := s.Write(ctx, "second", map[string][]byte{
		"a/plan.md":   []byte("plan\n"),
		"b/intent.md": []byte("other\n"),
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := git(t, dir, "rev-parse", second+"^"); got != first {
		t.Errorf("parent of second commit = %s, want %s", got, first)
	}
	if n := git(t, dir, "rev-list", "--count", store.Ref); n != "2" {
		t.Errorf("ref has %s commits, want 2", n)
	}
	for path, want := range map[string]string{
		"a/intent.md": "v1\n",
		"a/plan.md":   "plan\n",
		"b/intent.md": "other\n",
	} {
		got, err := s.Read(ctx, path)
		if err != nil || string(got) != want {
			t.Errorf("Read(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestWriteReplacesAnExistingFile(t *testing.T) {
	s := open(t, newRepo(t))
	ctx := context.Background()
	if _, err := s.Write(ctx, "v1", map[string][]byte{"a/plan.md": []byte("v1\n")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(ctx, "v2", map[string][]byte{"a/plan.md": []byte("v2\n")}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Read(ctx, "a/plan.md")
	if err != nil || string(got) != "v2\n" {
		t.Errorf("Read = %q, %v; want %q", got, err, "v2\n")
	}
}

func TestWriteLeavesWorkingTreeIndexAndBranchAlone(t *testing.T) {
	dir := newRepo(t)
	s := open(t, dir)

	// A clean repository must stay clean.
	if _, err := s.Write(context.Background(), "clean", map[string][]byte{"a/intent.md": []byte("x\n")}); err != nil {
		t.Fatal(err)
	}
	if st := git(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("git status after write in a clean repository:\n%s", st)
	}

	// A repository with work in progress must keep exactly that work.
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := func() []string {
		return []string{
			git(t, dir, "status", "--porcelain"),
			git(t, dir, "rev-parse", "HEAD"),
			git(t, dir, "symbolic-ref", "HEAD"),
			git(t, dir, "ls-files", "--stage"),
		}
	}
	before := snapshot()

	if _, err := s.Write(context.Background(), "dirty", map[string][]byte{"a/plan.md": []byte("y\n")}); err != nil {
		t.Fatal(err)
	}

	after := snapshot()
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("repository changed by Write:\nbefore: %q\nafter:  %q", before[i], after[i])
		}
	}
}

func TestCommitAuthorComesFromGitConfig(t *testing.T) {
	dir := newRepo(t)
	s := open(t, dir)

	commit, err := s.Write(context.Background(), "msg", map[string][]byte{"f": []byte("x")})
	if err != nil {
		t.Fatal(err)
	}

	if got := git(t, dir, "log", "-1", "--format=%an <%ae>", commit); got != "Ada Lovelace <ada@example.com>" {
		t.Errorf("author = %q, want %q", got, "Ada Lovelace <ada@example.com>")
	}
}

func TestReadMissingFileReportsNotFound(t *testing.T) {
	s := open(t, newRepo(t))
	ctx := context.Background()
	if _, err := s.Write(ctx, "msg", map[string][]byte{"a/intent.md": []byte("x")}); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"a/plan.md", "missing/intent.md", "a"} {
		if _, err := s.Read(ctx, path); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("Read(%q) error = %v, want ErrNotFound", path, err)
		}
	}
}

func TestListReturnsTheEntriesOfADirectoryAtTheTip(t *testing.T) {
	s := open(t, newRepo(t))
	ctx := context.Background()
	if _, err := s.Write(ctx, "one", map[string][]byte{
		"plans/a/intent.md":       []byte("x"),
		"plans/a/events/01A.yaml": []byte("x"),
		"plans/b/intent.md":       []byte("x"),
		"format-version":          []byte("1\n"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(ctx, "two", map[string][]byte{"plans/a/plan.md": []byte("x")}); err != nil {
		t.Fatal(err)
	}

	for dir, want := range map[string][]string{
		"":         {"format-version", "plans/"},
		"plans":    {"a/", "b/"},
		"plans/a":  {"events/", "intent.md", "plan.md"},
		"plans/a/": {"events/", "intent.md", "plan.md"},
	} {
		got, err := s.List(ctx, dir)
		if err != nil {
			t.Errorf("List(%q): %v", dir, err)
			continue
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("List(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestListMissingOrNonDirectoryReportsNotFound(t *testing.T) {
	s := open(t, newRepo(t))
	ctx := context.Background()
	if _, err := s.List(ctx, "plans"); !errors.Is(err, store.ErrNotInitialised) {
		t.Errorf("List before any write: error = %v, want ErrNotInitialised", err)
	}
	if _, err := s.Write(ctx, "msg", map[string][]byte{"plans/a/intent.md": []byte("x")}); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{"nope", "plans/a/intent.md"} {
		if _, err := s.List(ctx, dir); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("List(%q) error = %v, want ErrNotFound", dir, err)
		}
	}
}

// fakeGit puts a git script that prints the given version first on PATH.
func fakeGit(t *testing.T, version string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake git script needs a POSIX shell")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\necho 'git version " + version + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestOpenRejectsAGitOlderThanTheMinimum(t *testing.T) {
	dir := newRepo(t)
	fakeGit(t, "2.30.1")

	_, err := store.Open(dir)
	if err == nil {
		t.Fatal("Open succeeded with git 2.30.1")
	}
	if !strings.Contains(err.Error(), store.MinGitVersion) || !strings.Contains(err.Error(), "2.30.1") {
		t.Errorf("error %q should name the minimum %s and the found version", err, store.MinGitVersion)
	}
}

func TestOpenAcceptsVendorVersionStrings(t *testing.T) {
	dir := newRepo(t)
	for _, v := range []string{store.MinGitVersion + ".0", "2.39.3 (Apple Git-146)", "2.45.1.windows.1", "3.0.0"} {
		t.Run(v, func(t *testing.T) {
			fakeGit(t, v)
			if _, err := store.Open(dir); err != nil {
				t.Errorf("Open with git %s: %v", v, err)
			}
		})
	}
}

func TestOpenReportsMissingGit(t *testing.T) {
	dir := newRepo(t)
	t.Setenv("PATH", t.TempDir())

	_, err := store.Open(dir)
	if err == nil {
		t.Fatal("Open succeeded without git on PATH")
	}
	if !strings.Contains(err.Error(), "git") || !strings.Contains(err.Error(), store.MinGitVersion) {
		t.Errorf("error %q should say git %s or newer is required", err, store.MinGitVersion)
	}
}

func TestWriteRejectsBadInputWithoutCommitting(t *testing.T) {
	dir := newRepo(t)
	s := open(t, dir)
	ctx := context.Background()

	cases := map[string]map[string][]byte{
		"no files":       {},
		"empty path":     {"": []byte("x")},
		"absolute path":  {"/etc/passwd": []byte("x")},
		"parent segment": {"a/../b": []byte("x")},
		"empty segment":  {"a//b": []byte("x")},
		"trailing slash": {"a/": []byte("x")},
		"dot segment":    {"./a": []byte("x")},
		"git directory":  {".git/config": []byte("x")},
		"newline":        {"a\nb": []byte("x")},
		"backslash":      {`a\b`: []byte("x")},
	}
	for name, files := range cases {
		if _, err := s.Write(ctx, "bad", files); err == nil {
			t.Errorf("%s: Write succeeded", name)
		}
	}
	if _, err := s.Read(ctx, "x"); !errors.Is(err, store.ErrNotInitialised) {
		t.Errorf("a rejected write created the ref: %v", err)
	}
}

func TestWriteRejectsFileAndDirectoryClashes(t *testing.T) {
	s := open(t, newRepo(t))
	ctx := context.Background()
	if _, err := s.Write(ctx, "dir", map[string][]byte{"a/b": []byte("x")}); err != nil {
		t.Fatal(err)
	}

	for name, files := range map[string]map[string][]byte{
		"file over directory": {"a": []byte("y")},
		"directory over file": {"a/b/c": []byte("y")},
		"both in one write":   {"c": []byte("y"), "c/d": []byte("y")},
	} {
		if _, err := s.Write(ctx, name, files); err == nil {
			t.Errorf("%s: Write succeeded", name)
		}
	}
	if got, err := s.Read(ctx, "a/b"); err != nil || string(got) != "x" {
		t.Errorf("Read(a/b) = %q, %v after rejected write", got, err)
	}
}

func TestWriteFailsWithoutLosingACommitWhenAnotherWriterMovesTheRef(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wrapper git script needs a POSIX shell")
	}
	dir := newRepo(t)
	s := open(t, dir)
	ctx := context.Background()
	if _, err := s.Write(ctx, "base", map[string][]byte{"a": []byte("base")}); err != nil {
		t.Fatal(err)
	}
	// The other writer's commit, made on top of the same tip.
	theirs := git(t, dir, "commit-tree", git(t, dir, "rev-parse", store.Ref+"^{tree}"),
		"-p", store.Ref, "-m", "theirs")

	// A git wrapper that moves the ref to their commit just before our
	// commit is created, as a concurrent bfc process would.
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := "#!/bin/sh\n" +
		"case \" $* \" in *\" commit-tree \"*) \"" + realGit + "\" update-ref " + store.Ref + " " + theirs + " ;; esac\n" +
		"exec \"" + realGit + "\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, err := s.Write(ctx, "ours", map[string][]byte{"b": []byte("ours")}); err == nil {
		t.Fatal("Write succeeded although the ref moved underneath it")
	}
	if got := git(t, dir, "rev-parse", store.Ref); got != theirs {
		t.Errorf("ref = %s, want the other writer's commit %s", got, theirs)
	}
}
