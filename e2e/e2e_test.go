// Package e2e runs the real bfc binary the way two developers would: two
// clones of one bare remote, each with its own git identity. It proves the
// phase 1 exit criterion of docs/design.md (issue #19). Nothing here uses
// the network.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const plansRef = "refs/beforecode/plans"

// bfcBin is the binary built once by TestMain.
var bfcBin string

// TestMain builds bfc and isolates every git subprocess from the
// developer's own git configuration.
func TestMain(m *testing.M) {
	os.Exit(buildAndRun(m))
}

func buildAndRun(m *testing.M) int {
	dir, err := os.MkdirTemp("", "bfc-e2e")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	bfcBin = filepath.Join(dir, "bfc")
	if runtime.GOOS == "windows" {
		bfcBin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bfcBin, "github.com/admSla99/BeforeCode/cmd/bfc")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build bfc: %v\n%s", err, out)
		return 1
	}
	// The same isolation as the TestMain of cmd/bfc, which is another
	// package: an empty global config and no system config.
	empty := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for k, v := range map[string]string{"GIT_CONFIG_GLOBAL": empty, "GIT_CONFIG_NOSYSTEM": "1"} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return m.Run()
}

// dev is one developer: a clone with its own git identity and editor.
type dev struct {
	t      *testing.T
	name   string
	dir    string
	editor string // GIT_EDITOR for bfc new and bfc edit; "" for none
}

// bfc runs the binary in the developer's clone and returns its standard
// output, failing the test on a non-zero exit. Standard error, where git's
// own warnings go, is shown only on failure.
func (d *dev) bfc(args ...string) string {
	d.t.Helper()
	cmd := exec.Command(bfcBin, args...)
	cmd.Dir = d.dir
	cmd.Env = os.Environ()
	if d.editor != "" {
		cmd.Env = append(cmd.Env, "GIT_EDITOR="+d.editor)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		d.t.Fatalf("%s: bfc %s: %v\n%s%s", d.name, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// newPlan runs bfc new --no-edit and returns the plan's Short ID, the first
// word bfc new prints.
func (d *dev) newPlan(title string) string {
	d.t.Helper()
	out := strings.Fields(d.bfc("new", "--no-edit", title))
	if len(out) == 0 || !strings.HasPrefix(out[0], "bfc-") {
		d.t.Fatalf("%s: bfc new printed no Short ID: %q", d.name, out)
	}
	return out[0]
}

// sameRefAs fails unless the developer's plans ref is the remote's.
func (d *dev) sameRefAs(bare string) {
	d.t.Helper()
	if got, want := d.git("rev-parse", plansRef), git(d.t, bare, "rev-parse", plansRef); got != want {
		d.t.Errorf("%s: plans ref %s differs from the remote's %s", d.name, got, want)
	}
}

// git runs git in the developer's clone and returns its trimmed output.
func (d *dev) git(args ...string) string {
	d.t.Helper()
	return git(d.t, d.dir, args...)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// useEditor makes the developer's editor run a shell snippet that gets the
// file to edit as $1. bfc starts editors through sh, which on Windows is the
// sh.exe of Git for Windows.
func (d *dev) useEditor(snippet string) {
	d.t.Helper()
	script := filepath.Join(d.t.TempDir(), "editor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+snippet+"\n"), 0o700); err != nil {
		d.t.Fatal(err)
	}
	d.editor = `"` + filepath.ToSlash(script) + `"`
}

// newRemote creates the bare remote the team shares.
func newRemote(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "remote.git")
	git(t, ".", "init", "-q", "--bare", "-b", "main", bare)
	return bare
}

// clone clones the remote as a developer with this name.
func clone(t *testing.T, bare, name string) *dev {
	t.Helper()
	dir := filepath.Join(t.TempDir(), strings.ToLower(name))
	git(t, ".", "clone", "-q", bare, dir)
	d := &dev{t: t, name: name, dir: dir}
	d.git("config", "user.name", name)
	d.git("config", "user.email", strings.ToLower(name)+"@example.com")
	return d
}

// checkout is what a developer's git checkout looks like apart from the
// plans ref: the current branch, every branch, remote-tracking branch and
// tag with its commit, git's staging area, and the working tree.
type checkout struct {
	current, refs, staged, status string
}

func (d *dev) checkout() checkout {
	d.t.Helper()
	return checkout{
		current: d.git("symbolic-ref", "HEAD"),
		refs:    d.git("for-each-ref", "refs/heads", "refs/remotes", "refs/tags"),
		staged:  d.git("ls-files", "--stage"),
		status:  d.git("status", "--porcelain", "--untracked-files=all", "--ignored"),
	}
}

// unchanged fails if the developer's checkout differs from before.
func (d *dev) unchanged(before checkout) {
	d.t.Helper()
	if after := d.checkout(); after != before {
		d.t.Errorf("%s: bfc changed the checkout:\nbefore %+v\nafter  %+v", d.name, before, after)
	}
}

// listed is one plan of bfc list --json.
type listed struct {
	ShortID           string `json:"short_id"`
	State             string `json:"state"`
	Title             string `json:"title"`
	Approvals         int    `json:"approvals"`
	RequiredApprovals int    `json:"required_approvals"`
}

func (d *dev) list() map[string]listed {
	d.t.Helper()
	var plans []listed
	if err := json.Unmarshal([]byte(d.bfc("list", "--json")), &plans); err != nil {
		d.t.Fatalf("%s: bfc list --json: %v", d.name, err)
	}
	byTitle := map[string]listed{}
	for _, p := range plans {
		byTitle[p.Title] = p
	}
	return byTitle
}

// planMD is a plan.md that passes the submit check (ADR 0002 section 6).
const planMD = `## Context

Card payments fail for some EU customers.

## Approach

Move checkout to Stripe Payment Intents.

## Affected areas

The checkout service.

## Out of scope

Refunds.

## Tasks

- Replace the charge call.

## Acceptance criteria

- AC-1: A card that needs 3-D Secure completes checkout.
`

// startProject is Ana setting up BeforeCode and sharing it: the code
// branch on the remote, bfc init, the config committed and pushed.
func startProject(t *testing.T) (ana *dev, bare string) {
	t.Helper()
	bare = newRemote(t)
	ana = clone(t, bare, "Ana")
	ana.git("checkout", "-q", "-b", "main")
	ana.git("commit", "-q", "--allow-empty", "-m", "Start")
	ana.bfc("init")
	ana.git("add", ".beforecode")
	ana.git("commit", "-q", "-m", "Set up BeforeCode")
	ana.git("push", "-q", "origin", "main")
	return ana, bare
}

// AC-1, AC-3: Ana writes and submits a plan, Ben joins and approves it, and
// Ana sees it approved. No bfc command touches either checkout.
func TestTwoDevelopersCreateEditAndApproveAPlan(t *testing.T) {
	ana, bare := startProject(t)
	anaCheckout := ana.checkout()

	short := ana.newPlan("Payments via Stripe")
	planFile := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(planFile, []byte(planMD), 0o600); err != nil {
		t.Fatal(err)
	}
	ana.useEditor(`cp "` + filepath.ToSlash(planFile) + `" "$1"`)
	ana.bfc("edit", short)
	ana.bfc("submit", short)
	ana.bfc("push")

	ben := clone(t, bare, "Ben")
	benCheckout := ben.checkout()
	ben.bfc("bootstrap")
	if p := ben.list()["Payments via Stripe"]; p.State != "in-review" {
		t.Errorf("ben: before approving, list --json = %+v", p)
	}
	if out := ben.bfc("show", short); !strings.Contains(out, "AC-1: A card") {
		t.Errorf("ben: show lacks the plan:\n%s", out)
	}
	ben.bfc("approve", short)
	ben.bfc("push")

	ana.bfc("pull")
	out := ana.bfc("show", short)
	for _, want := range []string{"approved", "1/1 (ben@example.com)", "Ana <ana@example.com>"} {
		if !strings.Contains(out, want) {
			t.Errorf("ana: show lacks %q:\n%s", want, out)
		}
	}
	if p := ana.list()["Payments via Stripe"]; p.State != "approved" || p.Approvals != 1 || p.RequiredApprovals != 1 {
		t.Errorf("ana: list --json = %+v", p)
	}
	ana.sameRefAs(bare)

	ana.unchanged(anaCheckout)
	ben.unchanged(benCheckout)
}

// AC-2, AC-3: Ana and Ben write at the same time, Ana to an existing plan
// and Ben a new plan. Both pushes reach the remote and nothing is lost.
func TestConcurrentWritesFromTwoDevelopersAreBothKept(t *testing.T) {
	ana, bare := startProject(t)
	short := ana.newPlan("Payments via Stripe")
	ana.bfc("push")
	ben := clone(t, bare, "Ben")
	ben.bfc("bootstrap")
	anaCheckout, benCheckout := ana.checkout(), ben.checkout()

	// Neither has the other's change when writing.
	ana.useEditor(`printf '\nAlso Apple Pay.\n' >> "$1"`)
	ana.bfc("edit", "--intent", short)
	ben.newPlan("Search by customer email")
	ana.bfc("push")
	if out := ben.bfc("push"); !strings.Contains(out, "after merging changes pushed meanwhile") {
		t.Errorf("ben: push did not merge Ana's change first:\n%s", out)
	}
	ana.bfc("pull")

	for _, d := range []*dev{ana, ben} {
		plans := d.list()
		if len(plans) != 2 || plans["Payments via Stripe"].ShortID != short || plans["Search by customer email"].State != "draft" {
			t.Errorf("%s: plans = %+v", d.name, plans)
		}
		if out := d.bfc("show", short); !strings.Contains(out, "Also Apple Pay.") {
			t.Errorf("%s: Ana's edit is lost:\n%s", d.name, out)
		}
		d.sameRefAs(bare)
	}
	ana.unchanged(anaCheckout)
	ben.unchanged(benCheckout)
}
