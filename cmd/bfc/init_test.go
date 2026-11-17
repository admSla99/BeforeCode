package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/config"
)

const plansRef = "refs/beforecode/plans"

// TestMain isolates every git subprocess from the developer's own git
// configuration.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bfc-cmd-test")
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

// bfcIn runs bfc with args in dir and returns its combined output.
func bfcIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	var out bytes.Buffer
	cmd := newRootCmd("dev")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// gitIn runs git in dir and returns its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitTry(dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func gitTry(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		err = errors.New(err.Error() + ": " + string(exitErr.Stderr))
	}
	return strings.TrimSpace(string(out)), err
}

// newClone creates a bare repository standing in for GitHub and a clone of
// it with one commit, and returns the clone and the bare repository.
func newClone(t *testing.T) (clone, bare string) {
	t.Helper()
	bare = filepath.Join(t.TempDir(), "remote.git")
	gitIn(t, ".", "init", "-q", "--bare", "-b", "main", bare)
	clone = filepath.Join(t.TempDir(), "work")
	gitIn(t, ".", "init", "-q", "-b", "main", clone)
	gitIn(t, clone, "config", "user.name", "Ada Lovelace")
	gitIn(t, clone, "config", "user.email", "ada@example.com")
	gitIn(t, clone, "remote", "add", "origin", bare)
	writeFile(t, filepath.Join(clone, "README.md"), "hello\n")
	gitIn(t, clone, "add", "README.md")
	gitIn(t, clone, "commit", "-q", "-m", "initial")
	gitIn(t, clone, "push", "-q", "origin", "main")
	return clone, bare
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInitCreatesConfigTemplatesAndPlansRefWithoutCommitting(t *testing.T) {
	clone, _ := newClone(t)
	head := gitIn(t, clone, "rev-parse", "HEAD")
	defaults, err := filepath.Abs(filepath.Join("..", "..", ".beforecode", "templates"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := bfcIn(t, clone, "init")
	if err != nil {
		t.Fatalf("bfc init: %v\n%s", err, out)
	}

	cfg, err := config.Parse([]byte(readFile(t, filepath.Join(clone, ".beforecode", "config.yaml"))))
	if err != nil {
		t.Fatalf("config.yaml: %v", err)
	}
	if want := (config.Config{RequiredApprovals: 1, SyncEnabled: true, Remote: "origin"}); cfg != want {
		t.Errorf("config = %+v, want %+v", cfg, want)
	}
	for _, name := range []string{"intent.md", "plan.md"} {
		want := readFile(t, filepath.Join(defaults, name))
		if got := readFile(t, filepath.Join(clone, ".beforecode", "templates", name)); got != want {
			t.Errorf("template %s differs from the default:\n%s", name, got)
		}
	}
	if got := gitIn(t, clone, "show", plansRef+":format-version"); got != "1" {
		t.Errorf("format-version = %q, want %q", got, "1")
	}
	if got := gitIn(t, clone, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved from %s to %s: init must not commit", head, got)
	}
	if st := gitIn(t, clone, "status", "--porcelain"); st != "?? .beforecode/" {
		t.Errorf("git status = %q, want only the untracked .beforecode/", st)
	}
	for _, want := range []string{"git add .beforecode", "bfc push"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not tell the user %q:\n%s", want, out)
		}
	}
}

func fetchRefspecs(t *testing.T, clone string) []string {
	t.Helper()
	out, err := gitTry(clone, "config", "--get-all", "remote.origin.fetch")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(out, "\n")
}

func TestInitAddsTheFetchRefspecOnce(t *testing.T) {
	clone, _ := newClone(t)

	for range 2 {
		if out, err := bfcIn(t, clone, "init"); err != nil {
			t.Fatalf("bfc init: %v\n%s", err, out)
		}
	}

	n := 0
	for _, r := range fetchRefspecs(t, clone) {
		if r == "+refs/beforecode/*:refs/beforecode-remote/*" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("plans refspec appears %d times in %q, want once", n, fetchRefspecs(t, clone))
	}
}

func TestInitTwiceKeepsEverythingAndSaysSo(t *testing.T) {
	clone, _ := newClone(t)
	if out, err := bfcIn(t, clone, "init"); err != nil {
		t.Fatalf("first bfc init: %v\n%s", err, out)
	}
	// The user has edited their config and a template since.
	cfgPath := filepath.Join(clone, ".beforecode", "config.yaml")
	edited := strings.Replace(readFile(t, cfgPath), "required_approvals: 1", "required_approvals: 2", 1)
	writeFile(t, cfgPath, edited)
	tplPath := filepath.Join(clone, ".beforecode", "templates", "plan.md")
	writeFile(t, tplPath, "my template\n")
	tip := gitIn(t, clone, "rev-parse", plansRef)

	out, err := bfcIn(t, clone, "init")
	if err != nil {
		t.Fatalf("second bfc init: %v\n%s", err, out)
	}

	if got := readFile(t, cfgPath); got != edited {
		t.Errorf("config.yaml was overwritten:\n%s", got)
	}
	if got := readFile(t, tplPath); got != "my template\n" {
		t.Errorf("plan.md template was overwritten:\n%s", got)
	}
	if got := gitIn(t, clone, "rev-parse", plansRef); got != tip {
		t.Errorf("plans ref moved from %s to %s", tip, got)
	}
	for _, want := range []string{".beforecode/config.yaml", ".beforecode/templates/plan.md", plansRef, "refspec"} {
		if !lineHas(out, "kept", want) {
			t.Errorf("output does not report %s as kept:\n%s", want, out)
		}
	}
}

// lineHas reports whether one line of out contains both words.
func lineHas(out, a, b string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, a) && strings.Contains(line, b) {
			return true
		}
	}
	return false
}

func TestInitRefusesWhenTheRemoteAlreadyHasPlans(t *testing.T) {
	first, bare := newClone(t)
	if out, err := bfcIn(t, first, "init"); err != nil {
		t.Fatalf("bfc init: %v\n%s", err, out)
	}
	gitIn(t, first, "push", "-q", "origin", plansRef)
	second := filepath.Join(t.TempDir(), "second")
	gitIn(t, ".", "clone", "-q", bare, second)

	out, err := bfcIn(t, second, "init")

	if err == nil {
		t.Fatalf("bfc init succeeded although origin has plans:\n%s", out)
	}
	if !strings.Contains(out, "bfc bootstrap") {
		t.Errorf("output does not point to bfc bootstrap:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(second, ".beforecode")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("refused init left .beforecode behind (stat: %v)", err)
	}
	if _, err := gitTry(second, "rev-parse", "--verify", "--quiet", plansRef); err == nil {
		t.Error("refused init created the plans ref")
	}
}

func TestInitFailsWhenTheRemoteCannotBeReached(t *testing.T) {
	clone, _ := newClone(t)
	gitIn(t, clone, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	out, err := bfcIn(t, clone, "init")

	if err == nil {
		t.Fatalf("bfc init succeeded with an unreachable remote:\n%s", out)
	}
	if !strings.Contains(out, "--local") {
		t.Errorf("output does not suggest --local:\n%s", out)
	}
}

func TestInitFailsWhenTheRemoteDoesNotExist(t *testing.T) {
	clone, _ := newClone(t)

	out, err := bfcIn(t, clone, "init", "--remote", "upstream")

	if err == nil {
		t.Fatalf("bfc init succeeded with a missing remote:\n%s", out)
	}
	for _, want := range []string{`remote "upstream" does not exist`, "--remote", "--local"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
}

func TestInitWithAnotherRemote(t *testing.T) {
	clone, _ := newClone(t)
	gitIn(t, clone, "remote", "rename", "origin", "upstream")

	if out, err := bfcIn(t, clone, "init", "--remote", "upstream"); err != nil {
		t.Fatalf("bfc init --remote upstream: %v\n%s", err, out)
	}

	cfg, err := config.Parse([]byte(readFile(t, filepath.Join(clone, ".beforecode", "config.yaml"))))
	if err != nil || cfg.Remote != "upstream" {
		t.Errorf("config = %+v, %v; want remote upstream", cfg, err)
	}
	if got := gitIn(t, clone, "config", "--get-all", "remote.upstream.fetch"); !strings.Contains(got, "refs/beforecode/*") {
		t.Errorf("upstream fetch refspecs = %q", got)
	}
}

func TestInitLocalSkipsRemoteAndRecordsSyncOff(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "Ada Lovelace")
	gitIn(t, dir, "config", "user.email", "ada@example.com")

	out, err := bfcIn(t, dir, "init", "--local")
	if err != nil {
		t.Fatalf("bfc init --local: %v\n%s", err, out)
	}

	cfg, err := config.Parse([]byte(readFile(t, filepath.Join(dir, ".beforecode", "config.yaml"))))
	if err != nil {
		t.Fatal(err)
	}
	if want := (config.Config{RequiredApprovals: 1}); cfg != want {
		t.Errorf("config = %+v, want %+v", cfg, want)
	}
	if got := gitIn(t, dir, "show", plansRef+":format-version"); got != "1" {
		t.Errorf("format-version = %q", got)
	}
	if got, _ := gitTry(dir, "config", "--get-regexp", `^remote\.`); got != "" {
		t.Errorf("init --local touched remote config: %q", got)
	}
	if strings.Contains(out, "bfc push") {
		t.Errorf("init --local tells the user to push:\n%s", out)
	}
}

func TestInitOutsideAGitRepositoryFailsClearly(t *testing.T) {
	dir := t.TempDir()

	out, err := bfcIn(t, dir, "init")

	if err == nil {
		t.Fatalf("bfc init succeeded outside a repository:\n%s", out)
	}
	if !strings.Contains(out, "not inside a git working tree") {
		t.Errorf("output does not say why:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".beforecode")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("init left .beforecode behind outside a repository")
	}
}

func TestInitFromASubdirectoryWritesAtTheRepositoryRoot(t *testing.T) {
	clone, _ := newClone(t)
	sub := filepath.Join(clone, "src", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if out, err := bfcIn(t, sub, "init"); err != nil {
		t.Fatalf("bfc init: %v\n%s", err, out)
	}

	if _, err := os.Stat(filepath.Join(clone, ".beforecode", "config.yaml")); err != nil {
		t.Errorf("config.yaml not at the repository root: %v", err)
	}
}
