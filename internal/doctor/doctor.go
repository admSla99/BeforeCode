// Package doctor diagnoses the BeforeCode setup of a repository for
// `bfc doctor`. Each check is one unit: it reports pass, warn, fail or skip
// with the command that fixes a failure, and some checks have a safe fix
// that Run applies on request. No fix pushes, and none rewrites the plans
// ref: a fix changes the ref only with a pull, which fast-forwards or
// merges, or with a new commit on top of its tip (ADR 0007).
package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/admSla99/BeforeCode/internal/agent/claude"
	"github.com/admSla99/BeforeCode/internal/config"
	"github.com/admSla99/BeforeCode/internal/store"
)

// Status is the outcome of one check.
type Status string

// The outcomes. Only Fail makes bfc doctor exit non-zero.
const (
	Pass Status = "ok"
	Warn Status = "warn"
	Fail Status = "FAIL"
	Skip Status = "skip" // the check does not apply, or needs a check that failed
)

// Result is what one check found.
type Result struct {
	Check  string
	Status Status
	Detail string   // one line
	Items  []string // the files or plans a check lists, one per line
	// Fix says how to fix a Fail: a command where there is one. Without
	// --fix, Run puts the Repair in front of it.
	Fix string
	// Repair says what bfc doctor --fix does about this Fail, "" when it
	// has no repair for it.
	Repair string
	// Fixed is set when Run applied the check's fix and the check passed
	// afterwards; Status and Detail are then those of the second run.
	Fixed bool
	// FixErr is set when Run tried the check's fix and it failed.
	FixErr error
}

// remoteTimeout bounds the question to the remote.
const remoteTimeout = 30 * time.Second

// check is one diagnosis. run reports; fix is the safe fix for a Fail
// whose Repair run has set, after which run is called again.
type check struct {
	name string
	run  func(ctx context.Context, e *env) Result
	fix  func(ctx context.Context, e *env) error
}

// checks in the order bfc doctor prints them. Later checks rely on what
// earlier ones put into env.
var checks = []check{
	{name: "git version", run: checkGit},
	{name: "repository", run: checkRepository},
	{name: "config", run: checkConfig},
	{name: "plans ref", run: checkPlansRef},
	{name: "fetch refspec", run: checkRefspec, fix: fixRefspec},
	{name: "remote", run: checkRemote},
	{name: "objects", run: checkObjects, fix: fixObjects},
	{name: "in sync", run: checkInSync, fix: fixInSync},
	{name: "format-version", run: checkFormatVersion, fix: fixFormatVersion},
	{name: "index", run: checkIndex, fix: fixIndex},
	{name: "event files", run: checkEventFiles},
	{name: "plans", run: checkPlans},
	{name: "skipped events", run: checkSkippedEvents},
	{name: "short IDs", run: checkShortIDs},
	{name: "claude", run: checkClaude},
	{name: "claude hook", run: checkClaudeHook},
	{name: "claude skill", run: checkClaudeSkill},
}

// Run checks the setup of the repository around dir and returns one Result
// per check, in a fixed order. With fix it applies the safe fix of each
// failed check that has a Repair: add the fetch refspec, pull the plans ref
// when it is behind or diverged, rebuild the index, add a missing
// format-version (ADR 0007). Without fix, the Fix of such a check names
// bfc doctor --fix and what it would do. vars are passed to store.Open as
// its extra environment.
func Run(ctx context.Context, dir string, fix bool, vars ...string) []Result {
	e := &env{dir: dir, vars: vars}
	results := make([]Result, 0, len(checks))
	for _, c := range checks {
		r := c.run(ctx, e)
		repairable := r.Status == Fail && r.Repair != "" && c.fix != nil
		if repairable && !fix {
			r.Fix = withRepair(r)
		}
		if repairable && fix {
			if err := c.fix(ctx, e); err != nil {
				r.FixErr = err
				var h fixHint
				switch {
				case errors.As(err, &h):
					r.Fix = h.fix
				case r.Fix == "": // no way by hand: try again once the error is solved
					r.Fix = withRepair(r)
				}
			} else {
				r = c.run(ctx, e)
				r.Fixed = r.Status != Fail
			}
		}
		r.Check = c.name
		results = append(results, r)
	}
	return results
}

// withRepair returns the fix line of a repairable failure: bfc doctor --fix
// and what it does, then the way by hand when there is one.
func withRepair(r Result) string {
	fix := "bfc doctor --fix: " + r.Repair
	if r.Fix != "" {
		fix += "; or " + r.Fix
	}
	return fix
}

// fixHint is a failed fix that knows a better way out than the check's
// usual fix.
type fixHint struct {
	error
	fix string
}

// env is what the checks learn about the repository, shared between them.
type env struct {
	dir   string
	vars  []string // extra environment of git, see store.Open
	gitOK bool
	s     *store.Store // nil outside a repository

	cfg      config.Config
	cfgFound bool
	cfgErr   error
	// remote is the sync remote, "" in local mode or when the config
	// cannot be read.
	remote string

	asked     bool // the remote was asked for its plans ref
	remoteTip string
	remoteErr error
	refetched bool // --fix fetched every object of the remote's plans ref
	scanned   *scan
	scannedAt string // the plans ref commit scanned was read from
}

// tip returns the local plans ref, "" when it does not exist or cannot be
// read.
func (e *env) tip(ctx context.Context) string {
	if e.s == nil {
		return ""
	}
	tip, err := e.s.Tip(ctx)
	if err != nil {
		return ""
	}
	return tip
}

// askRemote asks the sync remote for its plans ref once.
func (e *env) askRemote(ctx context.Context) (string, error) {
	if !e.asked {
		ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
		defer cancel()
		e.remoteTip, e.remoteErr = e.s.RemoteTip(ctx, e.remote)
		e.asked = true
	}
	return e.remoteTip, e.remoteErr
}

// remoteExists reports whether the sync remote is a git remote of this
// clone.
func (e *env) remoteExists(ctx context.Context) bool {
	ok, err := e.s.HasRemote(ctx, e.remote)
	return err == nil && ok
}

// skipWithoutRemote returns a Skip result when the remote checks cannot
// run, and ok false.
func (e *env) skipWithoutRemote(ctx context.Context) (Result, bool) {
	switch {
	case e.s == nil:
		return skip("needs a git repository"), false
	case e.cfgErr != nil:
		return skip("needs a valid " + config.Path), false
	case e.remote == "":
		return skip("local mode: plans are not shared"), false
	case !e.remoteExists(ctx):
		return skip("no git remote " + e.remote), false
	}
	return Result{}, true
}

func skip(why string) Result { return Result{Status: Skip, Detail: why} }

func pass(format string, args ...any) Result {
	return Result{Status: Pass, Detail: fmt.Sprintf(format, args...)}
}

func warn(format string, args ...any) Result {
	return Result{Status: Warn, Detail: fmt.Sprintf(format, args...)}
}

func fail(fix, format string, args ...any) Result {
	return Result{Status: Fail, Detail: fmt.Sprintf(format, args...), Fix: fix}
}

func checkGit(ctx context.Context, e *env) Result {
	const install = "install git " + store.MinGitVersion + " or newer"
	version, ok, err := store.GitVersion(ctx)
	switch {
	case err != nil:
		return fail(install, "no git on PATH")
	case !ok:
		return fail(install, "%s is older than %s", version, store.MinGitVersion)
	}
	e.gitOK = true
	return pass("%s (needs %s)", version, store.MinGitVersion)
}

func checkRepository(_ context.Context, e *env) Result {
	if !e.gitOK {
		return skip("needs git")
	}
	s, err := store.Open(e.dir, e.vars...)
	if err != nil {
		return fail("run bfc inside a git working tree; git init creates one", "%s", strings.TrimPrefix(err.Error(), "store: "))
	}
	e.s = s
	return pass("%s", s.Root())
}

func checkConfig(_ context.Context, e *env) Result {
	if e.s == nil {
		return skip("needs a git repository")
	}
	data, err := os.ReadFile(filepath.Join(e.s.Root(), filepath.FromSlash(config.Path)))
	if errors.Is(err, os.ErrNotExist) {
		// Commands still sync with origin without a config file.
		e.remote = "origin"
		return fail("bfc init, then commit "+config.Path, "%s is missing", config.Path)
	}
	if err == nil {
		e.cfg, err = config.Parse(data)
	}
	if err != nil {
		e.cfgErr = err
		return fail("correct "+config.Path+" (see docs/adr/0003-config-file-v1.md), or update bfc if it is newer", "%v", err)
	}
	e.cfgFound = true
	if !e.cfg.SyncEnabled {
		return pass("%s is valid; local mode", config.Path)
	}
	e.remote = e.cfg.Remote
	return pass("%s is valid; syncs with %s", config.Path, e.remote)
}

// checkClaude looks for the command that starts Claude Code. bfc works
// without it, so a missing one is only a warning.
func checkClaude(_ context.Context, e *env) Result {
	if e.cfgErr != nil {
		return skip("needs a valid " + config.Path)
	}
	command := e.cfg.Command()
	path, err := lookPath(command, e.vars)
	if err != nil {
		return warn("%s not found; install Claude Code, or set claude.command in %s, to use it with bfc", command, config.Path)
	}
	return pass("%s is %s", command, path)
}

// checkClaudeHook reports whether the repository's Claude Code settings run
// bfc hook pre-tool-use for every edit tool, and warns when they run it with
// a bfc that cannot be found: Claude Code then reports the hook as failed
// and lets the edit through.
func checkClaudeHook(_ context.Context, e *env) Result {
	if e.s == nil {
		return skip("needs a git repository")
	}
	data, err := os.ReadFile(filepath.Join(e.s.Root(), filepath.FromSlash(claude.SettingsPath)))
	if errors.Is(err, os.ErrNotExist) {
		return skip("no " + claude.SettingsPath + "; bfc setup claude sets up Claude Code")
	}
	if err != nil {
		return warn("cannot read %s: %v", claude.SettingsPath, err)
	}
	h, ok, err := claude.FindHook(data)
	switch {
	case err != nil:
		return warn("%s is not valid JSON: %v", claude.SettingsPath, err)
	case !ok:
		return warn("%s does not run bfc %s, so Claude Code edits go unchecked; run bfc setup claude", claude.SettingsPath, claude.HookArgs)
	case !h.CoversEdits():
		return warn("%s runs %s only for %s, so other edits go unchecked; run bfc setup claude", claude.SettingsPath, h.Command, h.Matcher)
	}
	bin, _ := claude.HookBinary(h.Command)
	path, err := lookPath(bin, e.vars)
	switch {
	case err == nil:
		return pass("%s runs %s with %s", claude.SettingsPath, h.Command, path)
	case bin == "bfc":
		return warn("%s runs %s, but bfc is not on PATH, so Claude Code cannot run the hook and edits go unchecked; install bfc on PATH", claude.SettingsPath, h.Command)
	default:
		return warn("%s runs %s, but %s is not found, so Claude Code cannot run the hook and edits go unchecked", claude.SettingsPath, h.Command, bin)
	}
}

// checkClaudeSkill reports whether the beforecode skill is this bfc's
// version of it. Without any Claude Code setup it does not apply.
func checkClaudeSkill(_ context.Context, e *env) Result {
	if e.s == nil {
		return skip("needs a git repository")
	}
	data, err := os.ReadFile(filepath.Join(e.s.Root(), filepath.FromSlash(claude.SkillPath)))
	switch {
	case errors.Is(err, os.ErrNotExist):
		if _, err := os.Stat(filepath.Join(e.s.Root(), filepath.FromSlash(claude.SettingsPath))); errors.Is(err, os.ErrNotExist) {
			return skip("no " + claude.SkillPath + "; bfc setup claude writes it")
		}
		return warn("%s is missing, so Claude Code does not know to use bfc; run bfc setup claude", claude.SkillPath)
	case err != nil:
		return warn("cannot read %s: %v", claude.SkillPath, err)
	case !claude.SkillMatches(data):
		return warn("%s differs from this bfc's version; run bfc setup claude to rewrite it", claude.SkillPath)
	}
	return pass("%s matches this bfc", claude.SkillPath)
}

// lookPath finds a command like exec.LookPath, on the PATH of vars when they
// set one, else on the PATH of the process.
func lookPath(name string, vars []string) (string, error) {
	path, ok := "", false
	for _, v := range vars {
		if p, found := strings.CutPrefix(v, "PATH="); found {
			path, ok = p, true
		}
	}
	if !ok || strings.ContainsAny(name, `/\`) {
		return exec.LookPath(name)
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		if p, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

func checkPlansRef(ctx context.Context, e *env) Result {
	if e.s == nil {
		return skip("needs a git repository")
	}
	tip, err := e.s.Tip(ctx)
	if err == nil {
		return pass("%s at %s", store.Ref, short(tip))
	}
	if !errors.Is(err, store.ErrNotInitialised) {
		return fail("check the repository with git fsck", "cannot read %s: %v", store.Ref, err)
	}
	fix := "bfc init in a new project, or bfc bootstrap to join one that uses BeforeCode"
	if _, ok := e.skipWithoutRemote(ctx); ok {
		switch has, err := e.askRemote(ctx); {
		case err == nil && has != "":
			fix = "bfc bootstrap"
		case err == nil:
			fix = "bfc init"
		}
	} else if e.cfgFound && !e.cfg.SyncEnabled {
		fix = "bfc init"
	}
	return fail(fix, "%s does not exist", store.Ref)
}

func checkRefspec(ctx context.Context, e *env) Result {
	if r, ok := e.skipWithoutRemote(ctx); !ok {
		return r
	}
	has, err := e.s.HasFetchRefspec(ctx, e.remote)
	if err != nil {
		return fail("check .git/config with git config --get-all remote."+e.remote+".fetch", "cannot read the fetch refspecs of %s: %v", e.remote, err)
	}
	if !has {
		r := fail("git config --add remote."+e.remote+".fetch '"+store.FetchRefspec+"'", "%s is missing for %s", store.FetchRefspec, e.remote)
		r.Repair = "adds it"
		return r
	}
	return pass("%s for %s", store.FetchRefspec, e.remote)
}

func fixRefspec(ctx context.Context, e *env) error {
	_, err := e.s.AddFetchRefspec(ctx, e.remote)
	return err
}

func checkRemote(ctx context.Context, e *env) Result {
	switch {
	case e.s == nil:
		return skip("needs a git repository")
	case e.cfgErr != nil:
		return skip("needs a valid " + config.Path)
	case e.remote == "":
		return skip("local mode: plans are not shared")
	case !e.remoteExists(ctx):
		return fail("git remote add "+e.remote+" <url>, or set sync.remote in "+config.Path, "git remote %s does not exist", e.remote)
	}
	tip, err := e.askRemote(ctx)
	switch {
	case err != nil:
		return fail("check the network and your access with git ls-remote "+e.remote, "cannot reach %s: %s", e.remote, firstLine(err))
	case tip == "" && e.tip(ctx) != "":
		return fail("bfc push", "%s has no %s yet", e.remote, store.Ref)
	case tip == "":
		return fail("bfc init", "%s has no %s", e.remote, store.Ref)
	}
	return pass("%s has %s at %s", e.remote, store.Ref, short(tip))
}

// lostFix is the fix of objects that neither this clone nor the remote has.
const lostFix = "bfc cannot restore them; copy each from a clone that still has it " +
	"(there git cat-file -p <id> prints it, here git hash-object -w --stdin stores it), and run git fsck for the rest of the repository"

// checkObjects looks for objects of the plans ref's tip that this clone
// lacks. Those the sync remote has, --fix fetches again; the rest are lost
// (ADR 0007).
func checkObjects(ctx context.Context, e *env) Result {
	tip := e.tip(ctx)
	if tip == "" {
		return skip("needs " + store.Ref)
	}
	missing, err := e.s.MissingObjects(ctx, tip)
	if err != nil {
		return fail("check the repository with git fsck", "cannot read the tree of %s: %s", store.Ref, firstLine(err))
	}
	if len(missing) == 0 {
		return pass("all present")
	}
	items := make([]string, len(missing))
	for i, o := range missing {
		path := o.Path
		if path == "" {
			path = "(root tree)"
		}
		items[i] = path + " (" + o.ID + ")"
	}
	var r Result
	_, remote := e.skipWithoutRemote(ctx)
	switch {
	case e.remote == "" && e.cfgErr == nil:
		r = fail(lostFix, "%d missing from this clone; local mode has no remote to fetch them from: lost", len(missing))
	case !remote:
		r = fail(lostFix, "%d missing from this clone; no usable sync remote to fetch them from: lost", len(missing))
	case e.refetched:
		r = fail(lostFix, "%d missing here and on %s: lost", len(missing), e.remote)
	default:
		r = fail("git fetch --refetch "+e.remote+" '"+store.FetchRefspec+"'", "%d missing from this clone", len(missing))
		r.Repair = "fetches them again from " + e.remote
	}
	r.Items = items
	return r
}

// fixObjects fetches every object of the remote's plans ref again. The
// plans ref does not move, so no commit is needed.
func fixObjects(ctx context.Context, e *env) error {
	if err := e.s.Refetch(ctx, e.remote); err != nil {
		return err
	}
	e.refetched = true
	return nil
}

func checkInSync(ctx context.Context, e *env) Result {
	if r, ok := e.skipWithoutRemote(ctx); !ok {
		return r
	}
	theirs, err := e.askRemote(ctx)
	ours := e.tip(ctx)
	if err != nil || theirs == "" || ours == "" {
		return skip("needs " + store.Ref + " here and on " + e.remote)
	}
	if ours == theirs {
		return pass("up to date with %s", e.remote)
	}
	pull := func(format string, args ...any) Result {
		r := fail("bfc pull", format, args...)
		r.Repair = "pulls, with a fast-forward or a merge commit"
		return r
	}
	if !e.s.HasCommit(ctx, theirs) {
		return pull("%s has commits this clone lacks (behind or diverged)", e.remote)
	}
	if ahead, err := e.s.IsAncestor(ctx, theirs, ours); err == nil && ahead {
		return pass("ahead of %s; bfc push shares the new commits", e.remote)
	}
	if behind, err := e.s.IsAncestor(ctx, ours, theirs); err == nil && behind {
		return pull("behind %s", e.remote)
	}
	return pull("diverged from %s", e.remote)
}

// fixInSync pulls: a fast-forward or a merge commit, never a rewrite. On a
// conflict nothing is committed.
func fixInSync(ctx context.Context, e *env) error {
	_, err := e.s.Pull(ctx, e.remote, store.KeepNeither)
	var ce *store.ConflictError
	if errors.As(err, &ce) {
		return fixHint{err, "bfc pull --keep local or bfc pull --keep remote, to keep one side of the conflicting files"}
	}
	return err
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

// firstLine returns the first line of err's message.
func firstLine(err error) string {
	msg := err.Error()
	for i, r := range msg {
		if r == '\n' {
			return msg[:i]
		}
	}
	return msg
}
