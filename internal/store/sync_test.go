package store_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/store"
)

// team creates a bare repository standing in for GitHub and two clones of
// it, ana and ben, each with its own author.
func team(t *testing.T) (bare, ana, ben string) {
	t.Helper()
	bare = filepath.Join(t.TempDir(), "remote.git")
	git(t, ".", "init", "-q", "--bare", "-b", "main", bare)
	ana = newRepo(t)
	git(t, ana, "remote", "add", "origin", bare)
	git(t, ana, "push", "-q", "origin", "main")
	ben = filepath.Join(t.TempDir(), "ben")
	git(t, ".", "clone", "-q", bare, ben)
	git(t, ben, "config", "user.name", "Ben Bitdiddle")
	git(t, ben, "config", "user.email", "ben@example.com")
	return bare, ana, ben
}

func write(t *testing.T, s *store.Store, msg string, files map[string]string) string {
	t.Helper()
	b := map[string][]byte{}
	for k, v := range files {
		b[k] = []byte(v)
	}
	c, err := s.Write(context.Background(), msg, b)
	if err != nil {
		t.Fatalf("Write %s: %v", msg, err)
	}
	return c
}

func read(t *testing.T, s *store.Store, path string) string {
	t.Helper()
	b, err := s.Read(context.Background(), path)
	if err != nil {
		t.Fatalf("Read %s: %v", path, err)
	}
	return string(b)
}

func push(t *testing.T, s *store.Store) store.PushResult {
	t.Helper()
	r, err := s.Push(context.Background(), "origin")
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	return r
}

func pull(t *testing.T, s *store.Store, keep store.Keep) store.PullResult {
	t.Helper()
	r, err := s.Pull(context.Background(), "origin", keep)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	return r
}

// shared sets up a plans ref with base content that both clones have.
func shared(t *testing.T) (bare string, a, b *store.Store, anaDir, benDir string) {
	t.Helper()
	bare, anaDir, benDir = team(t)
	a, b = open(t, anaDir), open(t, benDir)
	write(t, a, "base", map[string]string{
		"format-version":    "1\n",
		"plans/p/intent.md": "# Title\n",
		"plans/p/plan.md":   "one\ntwo\nthree\n",
	})
	push(t, a)
	if r := pull(t, b, store.KeepNeither); r.Outcome != store.PullCreated {
		t.Fatalf("first pull outcome = %v, want PullCreated", r.Outcome)
	}
	return bare, a, b, anaDir, benDir
}

func TestPushCreatesTheRefOnTheRemoteAndPullBringsItToTheOtherClone(t *testing.T) {
	bare, anaDir, benDir := team(t)
	a, b := open(t, anaDir), open(t, benDir)
	tip := write(t, a, "init", map[string]string{"format-version": "1\n"})

	r := push(t, a)
	if r.Outcome != store.Pushed || r.Tip != tip || r.Attempts != 1 {
		t.Errorf("push = %+v, want Pushed %s in 1 attempt", r, tip)
	}
	if got := git(t, bare, "rev-parse", store.Ref); got != tip {
		t.Errorf("remote ref = %s, want %s", got, tip)
	}
	if r := push(t, a); r.Outcome != store.PushUpToDate {
		t.Errorf("second push outcome = %v, want PushUpToDate", r.Outcome)
	}

	if r := pull(t, b, store.KeepNeither); r.Outcome != store.PullCreated || r.Tip != tip {
		t.Errorf("pull = %+v, want PullCreated %s", r, tip)
	}
	if got := read(t, b, "format-version"); got != "1\n" {
		t.Errorf("format-version = %q", got)
	}
	if r := pull(t, b, store.KeepNeither); r.Outcome != store.PullUpToDate {
		t.Errorf("second pull outcome = %v, want PullUpToDate", r.Outcome)
	}
}

func TestPullFromARemoteWithoutPlans(t *testing.T) {
	_, anaDir, _ := team(t)
	a := open(t, anaDir)
	if r := pull(t, a, store.KeepNeither); r.Outcome != store.PullRemoteEmpty {
		t.Errorf("outcome = %v, want PullRemoteEmpty", r.Outcome)
	}
	if ok, _ := a.Initialised(context.Background()); ok {
		t.Error("pull from an empty remote created the plans ref")
	}
}

func TestPushWithoutPlansRefReportsNotInitialised(t *testing.T) {
	_, anaDir, _ := team(t)
	if _, err := open(t, anaDir).Push(context.Background(), "origin"); !errors.Is(err, store.ErrNotInitialised) {
		t.Errorf("err = %v, want ErrNotInitialised", err)
	}
}

func TestPullFastForwardsAndKeepsLocalCommitsThatAreAhead(t *testing.T) {
	_, a, b, _, _ := shared(t)
	tip := write(t, a, "ana adds", map[string]string{"plans/p/messages/01A.md": "hi\n"})
	push(t, a)

	if r := pull(t, b, store.KeepNeither); r.Outcome != store.PullFastForward || r.Tip != tip {
		t.Errorf("pull = %+v, want PullFastForward %s", r, tip)
	}
	ahead := write(t, b, "ben adds", map[string]string{"plans/p/messages/01B.md": "yo\n"})
	if r := pull(t, b, store.KeepNeither); r.Outcome != store.PullUpToDate || r.Tip != ahead {
		t.Errorf("pull while ahead = %+v, want PullUpToDate %s", r, ahead)
	}
}

// AC-2 and AC-4: diverged clones that only added files merge in memory with
// a merge commit, and the working tree, index and branch are untouched.
func TestPullMergesDivergedAdditionsWithoutTouchingTheWorkingTree(t *testing.T) {
	_, a, b, _, benDir := shared(t)
	anaTip := write(t, a, "ana", map[string]string{"plans/p/events/01A.yaml": "a\n", "plans/p/messages/01A.md": "a\n"})
	push(t, a)
	benTip := write(t, b, "ben", map[string]string{"plans/p/events/01B.yaml": "b\n", "plans/q/intent.md": "# Q\n"})

	head := git(t, benDir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(benDir, "README.md"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, benDir, "add", "README.md")
	status := git(t, benDir, "status", "--porcelain")

	r := pull(t, b, store.KeepNeither)
	if r.Outcome != store.PullMerged {
		t.Fatalf("outcome = %v, want PullMerged", r.Outcome)
	}
	parents := strings.Fields(git(t, benDir, "rev-list", "--parents", "-n", "1", store.Ref))
	if want := []string{r.Tip, benTip, anaTip}; !slices.Equal(parents, want) {
		t.Errorf("merge commit and parents = %v, want %v", parents, want)
	}
	for _, p := range []string{"plans/p/events/01A.yaml", "plans/p/messages/01A.md", "plans/p/events/01B.yaml", "plans/q/intent.md", "plans/p/plan.md"} {
		read(t, b, p)
	}
	if got := git(t, benDir, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved to %s", got)
	}
	if got := git(t, benDir, "status", "--porcelain"); got != status {
		t.Errorf("status changed from %q to %q", status, got)
	}
}

// AC-3 and AC-4: two clones write at once; the second push is rejected,
// merged and retried, and both sides end up with every file.
func TestPushAfterAConcurrentPushMergesAndRetries(t *testing.T) {
	bare, a, b, _, _ := shared(t)
	write(t, a, "ana", map[string]string{"plans/p/events/01A.yaml": "a\n"})
	write(t, b, "ben", map[string]string{"plans/p/events/01B.yaml": "b\n"})
	push(t, a)

	r := push(t, b)
	if r.Outcome != store.Pushed || r.Attempts != 2 || r.Merges != 1 {
		t.Errorf("push = %+v, want Pushed after 2 attempts and 1 merge", r)
	}
	if got := git(t, bare, "rev-parse", store.Ref); got != r.Tip {
		t.Errorf("remote = %s, want %s", got, r.Tip)
	}
	pull(t, a, store.KeepNeither)
	for _, s := range []*store.Store{a, b} {
		read(t, s, "plans/p/events/01A.yaml")
		read(t, s, "plans/p/events/01B.yaml")
	}
}

func TestPullWithUnrelatedHistoriesMergesThem(t *testing.T) {
	_, anaDir, benDir := team(t)
	a, b := open(t, anaDir), open(t, benDir)
	write(t, a, "init", map[string]string{"format-version": "1\n", "plans/a/intent.md": "# A\n"})
	write(t, b, "init", map[string]string{"format-version": "1\n", "plans/b/intent.md": "# B\n"})
	push(t, a)

	if r := push(t, b); r.Outcome != store.Pushed || r.Merges != 1 {
		t.Errorf("push = %+v, want Pushed with 1 merge", r)
	}
	read(t, b, "plans/a/intent.md")
	read(t, b, "plans/b/intent.md")
}

// conflicted makes ana and ben change the same line of plan.md, ana pushes.
func conflicted(t *testing.T) (a, b *store.Store, anaTip, benTip, benDir string) {
	t.Helper()
	_, a, b, _, benDir = shared(t)
	anaTip = write(t, a, "ana", map[string]string{"plans/p/plan.md": "one\nANA\nthree\n", "plans/p/events/01A.yaml": "a\n"})
	push(t, a)
	benTip = write(t, b, "ben", map[string]string{"plans/p/plan.md": "one\nBEN\nthree\n", "plans/p/events/01B.yaml": "b\n"})
	return a, b, anaTip, benTip, benDir
}

// AC-5: a real conflict commits nothing and reports both versions.
func TestPullWithAConflictCommitsNothingAndReportsBothVersions(t *testing.T) {
	_, b, anaTip, benTip, benDir := conflicted(t)

	_, err := b.Pull(context.Background(), "origin", store.KeepNeither)
	var ce *store.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *ConflictError", err)
	}
	if ce.Remote != "origin" || ce.LocalCommit != benTip || ce.RemoteCommit != anaTip {
		t.Errorf("conflict = %+v, want local %s remote %s", ce, benTip, anaTip)
	}
	if len(ce.Conflicts) != 1 || ce.Conflicts[0].Path != "plans/p/plan.md" {
		t.Fatalf("conflicts = %+v, want plans/p/plan.md", ce.Conflicts)
	}
	c := ce.Conflicts[0]
	if got := git(t, benDir, "cat-file", "blob", c.Local); got != "one\nBEN\nthree" {
		t.Errorf("local version = %q", got)
	}
	if got := git(t, benDir, "cat-file", "blob", c.Remote); got != "one\nANA\nthree" {
		t.Errorf("remote version = %q", got)
	}
	if got := git(t, benDir, "rev-parse", store.Ref); got != benTip {
		t.Errorf("local ref moved to %s", got)
	}
	if !strings.Contains(err.Error(), "plans/p/plan.md") {
		t.Errorf("error %q does not name the file", err)
	}

	// A push that runs into the conflict pushes nothing.
	_, err = b.Push(context.Background(), "origin")
	if !errors.As(err, &ce) {
		t.Fatalf("push err = %v, want *ConflictError", err)
	}
	if got := git(t, benDir, "rev-parse", store.Ref); got != benTip {
		t.Errorf("local ref moved to %s", got)
	}
}

func TestPullKeepResolvesConflictsWithTheChosenSideAndMergesTheRest(t *testing.T) {
	for _, tc := range []struct {
		keep store.Keep
		want string
	}{{store.KeepLocal, "one\nBEN\nthree\n"}, {store.KeepRemote, "one\nANA\nthree\n"}} {
		t.Run(tc.want, func(t *testing.T) {
			_, b, anaTip, benTip, benDir := conflicted(t)
			r := pull(t, b, tc.keep)
			if r.Outcome != store.PullMerged || !slices.Equal(r.Kept, []string{"plans/p/plan.md"}) {
				t.Errorf("pull = %+v, want PullMerged keeping plans/p/plan.md", r)
			}
			if got := read(t, b, "plans/p/plan.md"); got != tc.want {
				t.Errorf("plan.md = %q, want %q", got, tc.want)
			}
			read(t, b, "plans/p/events/01A.yaml")
			read(t, b, "plans/p/events/01B.yaml")
			parents := strings.Fields(git(t, benDir, "rev-list", "--parents", "-n", "1", store.Ref))
			if want := []string{r.Tip, benTip, anaTip}; !slices.Equal(parents, want) {
				t.Errorf("parents = %v, want %v", parents, want)
			}
			push(t, b)
		})
	}
}

// deleteInRef commits the removal of path on the plans ref of dir, as an
// older or other tool might.
func deleteInRef(t *testing.T, dir, path string) {
	t.Helper()
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(t.TempDir(), "index"))
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	run("read-tree", store.Ref)
	run("update-index", "--force-remove", path)
	c := run("commit-tree", run("write-tree"), "-p", store.Ref, "-m", "delete "+path)
	run("update-ref", store.Ref, c)
}

func TestPullKeepWhenOneSideDeletedTheFile(t *testing.T) {
	for _, keep := range []store.Keep{store.KeepLocal, store.KeepRemote} {
		t.Run(keep.String(), func(t *testing.T) {
			_, a, b, anaDir, _ := shared(t)
			deleteInRef(t, anaDir, "plans/p/plan.md")
			push(t, a)
			write(t, b, "ben", map[string]string{"plans/p/plan.md": "one\nBEN\nthree\n"})

			_, err := b.Pull(context.Background(), "origin", store.KeepNeither)
			var ce *store.ConflictError
			if !errors.As(err, &ce) || len(ce.Conflicts) != 1 || ce.Conflicts[0].Remote != "" || ce.Conflicts[0].Local == "" {
				t.Fatalf("err = %v, want a conflict with no remote version", err)
			}

			pull(t, b, keep)
			_, err = b.Read(context.Background(), "plans/p/plan.md")
			if keep == store.KeepRemote && !errors.Is(err, store.ErrNotFound) {
				t.Errorf("keep remote: plan.md still there (%v)", err)
			}
			if keep == store.KeepLocal && read(t, b, "plans/p/plan.md") != "one\nBEN\nthree\n" {
				t.Error("keep local lost ben's plan.md")
			}
			read(t, b, "plans/p/intent.md")
		})
	}
}

func TestRemoteErrorsCarryGitsOwnMessage(t *testing.T) {
	dir := newRepo(t)
	s := open(t, dir)
	write(t, s, "init", map[string]string{"format-version": "1\n"})
	git(t, dir, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))

	for name, f := range map[string]func() error{
		"pull": func() error { _, err := s.Pull(context.Background(), "origin", store.KeepNeither); return err },
		"push": func() error { _, err := s.Push(context.Background(), "origin"); return err },
	} {
		err := f()
		var re *store.RemoteError
		if !errors.As(err, &re) {
			t.Fatalf("%s: err = %v, want *RemoteError", name, err)
		}
		if re.Remote != "origin" || !strings.Contains(re.Message, "does not appear to be a git repository") {
			t.Errorf("%s: %+v should carry git's message", name, re)
		}
	}
}

// gitLog puts a git wrapper first on PATH that appends the arguments of
// every call to a file, runs before (shell code) and then the real git.
func gitLog(t *testing.T, before string) (logFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("wrapper git script needs a POSIX shell")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logFile = filepath.Join(bin, "calls")
	script := "#!/bin/sh\nGIT=\"" + realGit + "\"\n" +
		"printf '%s\\n' \"$*\" >> \"" + logFile + "\"\n" +
		before + "\nexec \"$GIT\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logFile
}

// AC-3: the retries are bounded, and nothing is force-pushed meanwhile.
func TestPushGivesUpAfterABoundedNumberOfAttempts(t *testing.T) {
	bare, _, b, _, _ := shared(t)
	write(t, b, "ben", map[string]string{"plans/p/events/01B.yaml": "b\n"})
	// Someone else wins the race before every one of ben's pushes.
	gitLog(t, `case " $* " in *" push "*)
  export GIT_AUTHOR_NAME=Eve GIT_AUTHOR_EMAIL=eve@example.com GIT_COMMITTER_NAME=Eve GIT_COMMITTER_EMAIL=eve@example.com
  tip=$("$GIT" -C "`+bare+`" rev-parse `+store.Ref+`)
  c=$("$GIT" -C "`+bare+`" commit-tree "$tip^{tree}" -p "$tip" -m race)
  "$GIT" -C "`+bare+`" update-ref `+store.Ref+` "$c" ;;
esac`)

	r, err := b.Push(context.Background(), "origin")
	if !errors.Is(err, store.ErrPushRetriesExhausted) {
		t.Fatalf("err = %v, want ErrPushRetriesExhausted; result %+v", err, r)
	}
	if r.Attempts != store.MaxPushAttempts {
		t.Errorf("attempts = %d, want %d", r.Attempts, store.MaxPushAttempts)
	}
	races := git(t, bare, "rev-list", "--count", "--grep=race", store.Ref)
	if races != "5" {
		t.Errorf("remote has %s race commits, want all %d kept", races, store.MaxPushAttempts)
	}
}

// AC-6: no code path force-pushes the ref.
func TestNoPushIsEverForced(t *testing.T) {
	_, a, b, _, _ := shared(t)
	logFile := gitLog(t, "")
	ctx := context.Background()

	write(t, a, "ana", map[string]string{"plans/p/plan.md": "one\nANA\nthree\n"})
	write(t, b, "ben", map[string]string{"plans/p/plan.md": "one\nBEN\nthree\n", "plans/p/events/01B.yaml": "b\n"})
	if _, err := a.Push(ctx, "origin"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Push(ctx, "origin"); err == nil {
		t.Fatal("push over a conflict succeeded")
	}
	if _, _, err := b.Sync(ctx, "origin", store.KeepRemote); err != nil {
		t.Fatal(err)
	}
	write(t, a, "ana again", map[string]string{"plans/p/events/01C.yaml": "c\n"})
	if _, _, err := a.Sync(ctx, "origin", store.KeepNeither); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	pushes := 0
	for _, line := range strings.Split(string(data), "\n") {
		args := strings.Fields(line)
		if !slices.Contains(args, "push") {
			continue
		}
		pushes++
		for _, arg := range args {
			if arg == "-f" || strings.HasPrefix(arg, "--force") || arg == "--mirror" || arg == "--delete" || strings.HasPrefix(arg, "+") {
				t.Errorf("forced push: git %s", line)
			}
		}
	}
	if pushes < 3 {
		t.Errorf("saw %d pushes, want at least 3", pushes)
	}
}

func TestSyncPullsThenPushes(t *testing.T) {
	bare, a, b, _, _ := shared(t)
	write(t, a, "ana", map[string]string{"plans/p/events/01A.yaml": "a\n"})
	push(t, a)
	write(t, b, "ben", map[string]string{"plans/p/events/01B.yaml": "b\n"})

	pr, ps, err := b.Sync(context.Background(), "origin", store.KeepNeither)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Outcome != store.PullMerged || ps.Outcome != store.Pushed || ps.Attempts != 1 {
		t.Errorf("sync = %+v, %+v; want merged, then pushed at once", pr, ps)
	}
	if got := git(t, bare, "rev-parse", store.Ref); got != ps.Tip {
		t.Errorf("remote = %s, want %s", got, ps.Tip)
	}
}

func TestARemoteNameThatLooksLikeAnOptionIsRefused(t *testing.T) {
	s := open(t, newRepo(t))
	write(t, s, "init", map[string]string{"format-version": "1\n"})
	for _, remote := range []string{"", "--upload-pack=touch pwned"} {
		if _, err := s.Pull(context.Background(), remote, store.KeepNeither); err == nil {
			t.Errorf("Pull(%q) succeeded", remote)
		}
		if _, err := s.Push(context.Background(), remote); err == nil {
			t.Errorf("Push(%q) succeeded", remote)
		}
	}
}
