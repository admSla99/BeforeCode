package index_test

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

// TestMain isolates every git subprocess from the developer's own git
// configuration.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bfc-index-test-home")
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

var ada = core.Person{Name: "Ada Lovelace", Email: "ada@example.com"}

// newStore returns a store on a new repository whose plans ref holds only
// format-version.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", ada.Name},
		{"config", "user.email", ada.Email},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, s, map[string]string{"format-version": "1\n"})
	return s
}

// clock sets the commit time of the next writes through s, and of the git
// helper on s. It opens s again with the dates as its environment rather
// than calling t.Setenv, so that the test can run in parallel; an index
// opened on s sees the change.
func clock(t *testing.T, s *store.Store, minute int) time.Time {
	t.Helper()
	ts := time.Date(2026, 11, 16, 9, minute, 0, 0, time.UTC)
	dated, err := store.Open(s.Root(), "GIT_COMMITTER_DATE="+ts.Format(time.RFC3339), "GIT_AUTHOR_DATE="+ts.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	*s = *dated
	return ts
}

func write(t *testing.T, s *store.Store, files map[string]string) {
	t.Helper()
	b := map[string][]byte{}
	for k, v := range files {
		b[k] = []byte(v)
	}
	if _, err := s.Write(context.Background(), "test", b); err != nil {
		t.Fatal(err)
	}
}

// newPlan writes a plan with this title and intent body and returns its ID.
func newPlan(t *testing.T, s *store.Store, title, body string) ulid.ULID {
	t.Helper()
	entropy := ulid.Monotonic(rand.Reader, 0)
	intent := []byte("---\ntitle: " + title + "\nlabels: [search]\n---\n\n" + body)
	p, files, err := core.NewPlan(intent, ada, time.Now(), func() ulid.ULID { return ulid.MustNew(ulid.Now(), entropy) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(context.Background(), "new", files); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func openIndex(t *testing.T, s *store.Store) *index.Index {
	t.Helper()
	ix, err := index.Open(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return ix
}

func plans(t *testing.T, ix *index.Index) []index.Plan {
	t.Helper()
	got, err := ix.Plans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func titles(ps []index.Plan) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Title)
	}
	return out
}

func TestPlansReadsThePlansRefNewestActivityFirst(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	clock(t, s, 1)
	old := newPlan(t, s, "Old draft", "Nothing yet.\n")
	t2 := clock(t, s, 2)
	pay := newPlan(t, s, "Payments", "Charge cards.\n")
	t3 := clock(t, s, 3)
	write(t, s, map[string]string{core.PlanDir(old) + "/plan.md": "## Context\n"})

	ix := openIndex(t, s)
	got := plans(t, ix)

	if want := []string{"Old draft", "Payments"}; !reflect.DeepEqual(titles(got), want) {
		t.Fatalf("titles = %v, want %v", titles(got), want)
	}
	if got[0].ID != old || !got[0].LastActivity.Equal(t3) {
		t.Errorf("old draft = %+v", got[0])
	}
	p := got[1]
	if p.ID != pay || !p.LastActivity.Equal(t2) || p.Status.State != core.StateDraft ||
		p.Status.Author != ada || !strings.HasPrefix(p.ShortID, "bfc-") || p.ShortID != p.Status.ShortID ||
		!reflect.DeepEqual(p.Labels, []string{"search"}) {
		t.Errorf("payments = %+v", p)
	}
	ctx := context.Background()
	if d, err := ix.Plan(ctx, old); err != nil || string(d.Plan) != "## Context\n" {
		t.Errorf("old draft documents = %+v, %v", d, err)
	}
	if d, err := ix.Plan(ctx, pay); err != nil || d.Plan != nil || !strings.Contains(string(d.Intent), "Charge cards.") {
		t.Errorf("payments documents = %+v, %v", d, err)
	}
	if _, err := os.Stat(ix.Path()); err != nil {
		t.Errorf("no database file: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(ix.Path()), ".git/beforecode/") {
		t.Errorf("database at %s, want it under .git/beforecode/", ix.Path())
	}
}

func TestUpdateRecordsTheRefCommitAndRereadsOnlyChangedPlans(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	clock(t, s, 1)
	a := newPlan(t, s, "A", "a\n")
	b := newPlan(t, s, "B", "b\n")
	ix := openIndex(t, s)

	first, err := ix.Update(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tip, _ := s.Tip(ctx)
	if first.From != "" || first.To != tip || len(first.Reread) != 2 {
		t.Errorf("first update = %+v, want both plans read up to %s", first, tip)
	}
	if again, err := ix.Update(ctx); err != nil || again.From != tip || len(again.Reread) != 0 {
		t.Errorf("update without a move = %+v, %v; want nothing read", again, err)
	}

	t5 := clock(t, s, 5)
	write(t, s, map[string]string{core.PlanDir(b) + "/plan.md": "## Context\n"})
	moved, err := ix.Update(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(moved.Reread, []ulid.ULID{b}) {
		t.Errorf("after editing B, reread %v, want only %s", moved.Reread, b)
	}
	got := plans(t, ix)
	if got[0].ID != b || !got[0].LastActivity.Equal(t5) || got[1].ID != a {
		t.Errorf("plans after the edit = %+v", got)
	}
	if d, err := ix.Plan(ctx, b); err != nil || string(d.Plan) != "## Context\n" {
		t.Errorf("B after the edit = %+v, %v", d, err)
	}
}

func TestPlansFollowsARefThatWasReplaced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	clock(t, s, 1)
	newPlan(t, s, "Kept", "k\n")
	ix := openIndex(t, s)
	plans(t, ix)

	// Point the ref at a history that does not contain the indexed commit.
	other := newStore(t)
	clock(t, s, 2)
	newPlan(t, other, "Elsewhere", "e\n")
	otherTip, _ := other.Tip(ctx)
	for _, args := range [][]string{
		{"fetch", "-q", other.Root(), store.Ref + ":refs/other"},
		{"update-ref", store.Ref, otherTip},
	} {
		if out, err := exec.Command("git", append([]string{"-C", s.Root()}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	if got := titles(plans(t, ix)); !reflect.DeepEqual(got, []string{"Elsewhere"}) {
		t.Errorf("titles = %v, want only the plan of the new history", got)
	}
}

func TestDeletingTheDatabaseLosesNothing(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	clock(t, s, 1)
	newPlan(t, s, "One", "first\n")
	clock(t, s, 2)
	newPlan(t, s, "Two", "second\n")
	ix := openIndex(t, s)
	before := plans(t, ix)
	path := ix.Path()
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	after := plans(t, openIndex(t, s))
	if !reflect.DeepEqual(before, after) {
		t.Errorf("after rebuild\n%+v\nwant\n%+v", after, before)
	}
}

// damage replaces the index database of s by what spoil writes there.
func damage(t *testing.T, s *store.Store, spoil func(path string)) {
	t.Helper()
	ix := openIndex(t, s)
	plans(t, ix)
	path := ix.Path()
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	spoil(path)
}

func TestABrokenDatabaseIsRebuiltSilently(t *testing.T) {
	t.Parallel()
	for name, spoil := range map[string]func(t *testing.T, path string){
		"not a database": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte(strings.Repeat("garbage ", 1000)), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"corrupt pages": func(t *testing.T, path string) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for i := 100; i < len(data); i++ { // keep the 100-byte header
				data[i] = 0xA5
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"missing table": func(t *testing.T, path string) {
			if err := index.DropTable(path, "plans"); err != nil {
				t.Fatal(err)
			}
		},
		"older schema": func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			// A database of an earlier schema that has nothing this
			// version needs.
			if err := index.WriteOldSchema(path); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newStore(t)
			clock(t, s, 1)
			newPlan(t, s, "Survivor", "still here\n")
			damage(t, s, func(path string) { spoil(t, path) })

			ix := openIndex(t, s)
			if got := titles(plans(t, ix)); !reflect.DeepEqual(got, []string{"Survivor"}) {
				t.Errorf("titles = %v", got)
			}
			hits, err := ix.Search(context.Background(), "still")
			if err != nil || len(hits) != 1 {
				t.Errorf("Search after rebuild = %v, %v", hits, err)
			}
		})
	}
}

func TestSearchFindsPlansByIntentAndPlanText(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	clock(t, s, 1)
	pay := newPlan(t, s, "Payments", "Customers pay by card.\n")
	clock(t, s, 2)
	login := newPlan(t, s, "Login", "Sign in with GitHub.\n")
	write(t, s, map[string]string{core.PlanDir(login) + "/plan.md": "## Approach\n\nAn invalid card is refused with 402 and the order stays awaiting payment.\n"})
	clock(t, s, 3)
	newPlan(t, s, "Platby", "Zamietnutá karta vráti chybu.\n")
	ix := openIndex(t, s)

	hits, err := ix.Search(ctx, "card")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("Search(card) = %+v, want two plans", hits)
	}
	byID := map[ulid.ULID]string{}
	for _, h := range hits {
		byID[h.Plan.ID] = h.Snippet
	}
	if !strings.Contains(byID[pay], "[card]") || !strings.Contains(byID[login], "invalid [card] is refused") {
		t.Errorf("snippets = %q", byID)
	}
	for _, snip := range byID {
		if strings.Contains(snip, "\n") {
			t.Errorf("snippet %q spans lines", snip)
		}
	}

	if hits, _ := ix.Search(ctx, "card github"); len(hits) != 1 || hits[0].Plan.ID != login {
		t.Errorf("Search(card github) = %+v, want only Login: every word must match", hits)
	}
	if hits, _ := ix.Search(ctx, "zamietnuta"); len(hits) != 1 || hits[0].Plan.Title != "Platby" {
		t.Errorf("Search without diacritics = %+v, want Platby", hits)
	}
	if hits, err := ix.Search(ctx, "nothing-like-this"); err != nil || len(hits) != 0 {
		t.Errorf("Search(no match) = %+v, %v", hits, err)
	}
	for _, q := range []string{`"card`, "card AND", "NOT", "c*rd)", "title:card"} {
		if _, err := ix.Search(ctx, q); err != nil {
			t.Errorf("Search(%q) fails: %v", q, err)
		}
	}
	if _, err := ix.Search(ctx, "   "); !errors.Is(err, index.ErrEmptyQuery) {
		t.Errorf("Search(blank) = %v, want ErrEmptyQuery", err)
	}
}

func TestSearchSeesChangesToThePlansRef(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	clock(t, s, 1)
	id := newPlan(t, s, "Payments", "By card.\n")
	ix := openIndex(t, s)
	if hits, _ := ix.Search(ctx, "stripe"); len(hits) != 0 {
		t.Fatalf("unexpected hits %+v", hits)
	}
	write(t, s, map[string]string{core.PlanDir(id) + "/plan.md": "Use Stripe.\n"})
	if hits, _ := ix.Search(ctx, "stripe"); len(hits) != 1 {
		t.Errorf("Search after the edit = %+v, want the plan", hits)
	}
}

func TestSearchIgnoresFrontMatterAndTemplateHints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	clock(t, s, 1)
	newPlan(t, s, "Payments", "## Problem\n\n<!-- What is wrong or missing today? -->\nCards are refused.\n")
	ix := openIndex(t, s)

	if hits, _ := ix.Search(ctx, "missing"); len(hits) != 0 {
		t.Errorf("Search(missing) = %+v; text inside <!-- --> is a template hint, not content", hits)
	}
	if hits, _ := ix.Search(ctx, "labels"); len(hits) != 0 {
		t.Errorf("Search(labels) = %+v; front matter keys are not content", hits)
	}
	hits, err := ix.Search(ctx, "payments")
	if err != nil || len(hits) != 1 || hits[0].Snippet != "[Payments]" {
		t.Errorf("Search(payments) = %+v, %v; want the title as the snippet", hits, err)
	}
	hits, _ = ix.Search(ctx, "refused")
	if len(hits) != 1 || strings.Contains(hits[0].Snippet, "<!--") || !strings.Contains(hits[0].Snippet, "Cards are [refused].") {
		t.Errorf("Search(refused) = %+v", hits)
	}
}

// git runs git in the repository of s and returns its trimmed output.
func git(t *testing.T, s *store.Store, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", s.Root()}, args...)...)
	cmd.Env = s.Environ()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestAnIndexKeptUpToDateAcrossAMergeMatchesAFreshOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	clock(t, s, 1)
	a := newPlan(t, s, "A", "a\n")
	base, _ := s.Tip(ctx)
	clock(t, s, 5)
	write(t, s, map[string]string{core.PlanDir(a) + "/plan.md": "Theirs.\n"})
	theirs, _ := s.Tip(ctx)
	git(t, s, "update-ref", store.Ref, base)
	clock(t, s, 2)
	newPlan(t, s, "B", "b\n")
	ix := openIndex(t, s)
	plans(t, ix) // indexed at ours

	ours, _ := s.Tip(ctx)
	tree := strings.Fields(git(t, s, "merge-tree", "--write-tree", ours, theirs))[0]
	clock(t, s, 6)
	merge := git(t, s, "commit-tree", tree, "-p", ours, "-p", theirs, "-m", "merge")
	git(t, s, "update-ref", store.Ref, merge, ours)

	got := plans(t, ix)
	path := ix.Path()
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	want := plans(t, openIndex(t, s))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("updated index\n%+v\nfresh index\n%+v", got, want)
	}
	if want[0].ID != a || want[0].LastActivity.Hour() != 9 || want[0].LastActivity.Minute() != 5 {
		t.Errorf("A = %+v, want last activity 09:05 from the merged side", want[0])
	}
}

func TestAReplacedHistoryRereadsOnlyChangedPlans(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	clock(t, s, 1)
	newPlan(t, s, "Same", "s\n")
	clock(t, s, 2)
	changed := newPlan(t, s, "Changed", "c\n")
	ix := openIndex(t, s)
	plans(t, ix)

	// The same files as a new root commit, so the indexed commit is not in
	// the history any more, then an edit of one plan on top.
	old, _ := s.Tip(ctx)
	clock(t, s, 3)
	root := git(t, s, "commit-tree", old+"^{tree}", "-m", "rewritten")
	git(t, s, "update-ref", store.Ref, root, old)
	clock(t, s, 4)
	write(t, s, map[string]string{core.PlanDir(changed) + "/plan.md": "New.\n"})

	u, err := ix.Update(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(u.Reread, []ulid.ULID{changed}) {
		t.Errorf("reread %v, want only %s", u.Reread, changed)
	}
	got := plans(t, ix)
	if got[0].ID != changed || got[0].LastActivity.Minute() != 4 || got[1].LastActivity.Minute() != 3 {
		t.Errorf("last activity not recomputed from the new history: %+v", got)
	}
}

func TestAnEmptyFileIsNotAMissingOne(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	clock(t, s, 1)
	id := newPlan(t, s, "Empty plan", "x\n")
	write(t, s, map[string]string{core.PlanDir(id) + "/plan.md": ""})
	ix := openIndex(t, s)

	p, err := ix.Plan(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Plan == nil || len(p.Plan) != 0 {
		t.Errorf("plan.md = %#v, want an empty, non-nil slice", p.Plan)
	}
}

func TestAPlanDirectoryWithALowercaseULIDIsListed(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	clock(t, s, 1)
	newPlan(t, s, "Normal", "n\n")
	odd := ulid.MustNew(ulid.Now(), rand.Reader)
	write(t, s, map[string]string{"plans/" + strings.ToLower(odd.String()) + "/plan.md": "## Context\n"})
	ix := openIndex(t, s)

	got := plans(t, ix)
	if len(got) != 2 {
		t.Fatalf("plans = %v", titles(got))
	}
	p, err := ix.Plan(context.Background(), odd)
	if err != nil || p.Status.State != core.StateInvalid || string(p.Plan) != "## Context\n" || !strings.HasPrefix(p.ShortID, "bfc-") {
		t.Errorf("the lowercase plan = %+v, %v; want it invalid, with its plan.md", p, err)
	}
	if u, err := ix.Update(context.Background()); err != nil || len(u.Reread) != 0 {
		t.Errorf("second update = %+v, %v; want nothing read again", u, err)
	}
}

func TestAnIndexBuiltFromACommitThatIsGoneIsRebuilt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	clock(t, s, 1)
	newPlan(t, s, "Old", "o\n")
	ix := openIndex(t, s)
	plans(t, ix)

	// The plans ref is replaced and the old commits are pruned.
	other := newStore(t)
	newPlan(t, other, "New", "n\n")
	otherTip, _ := other.Tip(ctx)
	git(t, s, "fetch", "-q", other.Root(), store.Ref+":refs/other")
	git(t, s, "update-ref", store.Ref, otherTip)
	git(t, s, "update-ref", "-d", "refs/other")
	git(t, s, "reflog", "expire", "--expire=now", "--all")
	git(t, s, "gc", "-q", "--prune=now")

	if got := titles(plans(t, ix)); !reflect.DeepEqual(got, []string{"New"}) {
		t.Errorf("titles = %v", got)
	}
}

func TestSearchForTextWithoutWordsIsAnEmptyQuery(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	newPlan(t, s, "Any", "a\n")
	ix := openIndex(t, s)
	for _, q := range []string{"-", "!! --", `"`} {
		if _, err := ix.Search(context.Background(), q); !errors.Is(err, index.ErrEmptyQuery) {
			t.Errorf("Search(%q) = %v, want ErrEmptyQuery", q, err)
		}
	}
}

func TestPlansLeavesOutTheDocuments(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := newPlan(t, s, "Docs", "body\n")
	write(t, s, map[string]string{core.PlanDir(id) + "/plan.md": "plan\n"})
	ix := openIndex(t, s)
	got := plans(t, ix)
	if got[0].Intent != nil || got[0].Plan != nil {
		t.Errorf("Plans returned the documents: %+v", got[0])
	}
	p, err := ix.Plan(context.Background(), id)
	if err != nil || p.Intent == nil || string(p.Plan) != "plan\n" {
		t.Errorf("Plan = %+v, %v; want both documents", p, err)
	}
}

func TestARepositoryPathWithAQuestionMark(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows paths cannot contain '?'")
	}
	parent := filepath.Join(t.TempDir(), "what?#x")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", parent, "init", "-q").CombinedOutput()
	if err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	s, err := store.Open(parent, "GIT_AUTHOR_NAME="+ada.Name, "GIT_AUTHOR_EMAIL="+ada.Email, "GIT_COMMITTER_NAME="+ada.Name, "GIT_COMMITTER_EMAIL="+ada.Email)
	if err != nil {
		t.Fatal(err)
	}
	write(t, s, map[string]string{"format-version": "1\n"})
	newPlan(t, s, "Odd path", "p\n")
	ix := openIndex(t, s)
	plans(t, ix)
	if _, err := os.Stat(filepath.Join(parent, ".git", "beforecode", "index.db")); err != nil {
		t.Errorf("the database is not where it belongs: %v", err)
	}
}
