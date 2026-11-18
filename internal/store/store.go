package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// MinGitVersion is the oldest git release BeforeCode works with. It is set by
// `git merge-tree --write-tree`, which push and pull need and which first
// shipped in git 2.38. See docs/research/minimum-git-version.md.
const MinGitVersion = "2.38"

// Ref is the plans ref: the hidden git ref that holds all plans.
const Ref = "refs/beforecode/plans"

// ErrNotInitialised is returned when the plans ref does not exist yet.
var ErrNotInitialised = errors.New("store: plans ref " + Ref + " does not exist (not initialised)")

// ErrNotFound is returned when a path does not exist at the tip of the plans
// ref, or is not of the kind asked for (a file for Read, a directory for List).
var ErrNotFound = errors.New("store: path not found in " + Ref)

// Store reads and writes files in the plans ref of one git repository. It
// deals only in slash-separated paths and bytes.
type Store struct {
	dir string
	env []string
}

// Open returns a Store for the git repository whose working tree contains
// dir. It fails if dir is not inside a git working tree, or if the system git
// is missing or older than MinGitVersion. env holds NAME=value variables
// that every git command of the Store gets on top of the environment of
// this process, such as GIT_EDITOR or GIT_COMMITTER_DATE; a later entry
// wins over an earlier one.
func Open(dir string, env ...string) (*Store, error) {
	s := &Store{dir: dir, env: slices.Clone(env)}
	found, ok, err := GitVersion(context.Background())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("store: BeforeCode needs git %s or newer, found %q", MinGitVersion, found)
	}
	out, stderr, err := s.run(context.Background(), nil, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		// git's own message tells "not a git repository" from, say, a
		// repository git refuses because of its owner (safe.directory).
		return nil, fmt.Errorf("store: %s is not inside a git working tree (git: %s)", dir, firstGitLine(stderr, err))
	}
	s.dir = strings.TrimSpace(string(out))
	return s, nil
}

// GitVersion returns what `git version` prints, such as "git version
// 2.45.1", and whether that release is at least MinGitVersion. It fails when
// no git is on PATH.
// The answer is kept per git binary for the life of the process: Open asks
// each time, the TUI opens a Store for many actions, the tests for every
// command, and starting git is slow on Windows.
func GitVersion(ctx context.Context) (version string, ok bool, err error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", false, fmt.Errorf("store: BeforeCode needs git %s or newer on PATH: %w", MinGitVersion, err)
	}
	gitVersions.Lock()
	version, known := gitVersions.byPath[path]
	gitVersions.Unlock()
	if !known {
		out, err := exec.CommandContext(ctx, path, "version").Output()
		if err != nil {
			return "", false, fmt.Errorf("store: BeforeCode needs git %s or newer on PATH: %w", MinGitVersion, err)
		}
		version = strings.TrimSpace(string(out))
		gitVersions.Lock()
		gitVersions.byPath[path] = version
		gitVersions.Unlock()
	}
	return version, atLeast(version, MinGitVersion), nil
}

// gitVersions holds what `git version` printed, by the path of the git
// binary.
var gitVersions = struct {
	sync.Mutex
	byPath map[string]string
}{byPath: map[string]string{}}

// Environ returns the environment the Store runs git with: that of this
// process and the variables given to Open. Programs that git would start,
// such as the editor, need the same.
func (s *Store) Environ() []string {
	return append(os.Environ(), s.env...)
}

// Read returns the content of the file at path at the current tip of the
// plans ref.
func (s *Store) Read(ctx context.Context, path string) ([]byte, error) {
	tip, err := s.tip(ctx)
	if err != nil {
		return nil, err
	}
	return s.ReadAt(ctx, tip, path)
}

// ReadAt returns the content of the file at path in commit, so that a
// reader can see one commit while the plans ref moves.
func (s *Store) ReadAt(ctx context.Context, commit, path string) ([]byte, error) {
	e, err := s.entry(ctx, commit, path)
	if err != nil {
		return nil, err
	}
	if e.typ != "blob" {
		return nil, fmt.Errorf("%w: %s is not a file", ErrNotFound, path)
	}
	return s.git(ctx, nil, nil, "cat-file", "blob", e.id)
}

// List returns the names of the entries directly inside dir at the current
// tip of the plans ref, in git's tree order (byte order). Directories carry a
// trailing "/". An empty dir lists the root of the ref.
func (s *Store) List(ctx context.Context, dir string) ([]string, error) {
	tip, err := s.tip(ctx)
	if err != nil {
		return nil, err
	}
	dir = strings.TrimSuffix(dir, "/")
	tree := tip + "^{tree}"
	if dir != "" {
		e, err := s.entry(ctx, tip, dir)
		if err != nil {
			return nil, err
		}
		if e.typ != "tree" {
			return nil, fmt.Errorf("%w: %s is not a directory", ErrNotFound, dir)
		}
		tree = e.id
	}
	out, err := s.git(ctx, nil, nil, "ls-tree", "-z", tree)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range parseLsTree(out) {
		if e.typ == "tree" {
			e.name += "/"
		}
		names = append(names, e.name)
	}
	return names, nil
}

// Write adds or replaces files in the plans ref and records them as exactly
// one commit with the given message, on top of the current tip. The author is
// taken from the user's git configuration. It returns the ID of the new
// commit. The working tree, the index and the current branch are not touched.
//
// If another writer moves the plans ref while Write runs, the update is
// refused and the ref is left as the other writer set it.
//
// Write knows nothing about plans: it will replace any file it is given.
// Keeping events, messages, handoffs and checks append-only is the job of the
// caller.
func (s *Store) Write(ctx context.Context, message string, files map[string][]byte) (string, error) {
	if len(files) == 0 {
		return "", errors.New("store: nothing to write")
	}
	for path := range files {
		if err := checkPath(path); err != nil {
			return "", err
		}
	}
	parent, err := s.tip(ctx)
	if err != nil && !errors.Is(err, ErrNotInitialised) {
		return "", err
	}
	if err := s.checkClashes(ctx, parent, files); err != nil {
		return "", err
	}

	tree, err := s.writeTree(ctx, parent, files)
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", message}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	commit, err := s.gitLine(ctx, nil, nil, args...)
	if err != nil {
		return "", err
	}
	// The old value (empty for a ref that must not exist yet) makes
	// update-ref refuse the change if the ref moved since we read it.
	if _, err := s.git(ctx, nil, nil, "update-ref", "-m", "bfc: "+message, Ref, commit, parent); err != nil {
		return "", err
	}
	return commit, nil
}

// writeTree builds the tree of parent with files added, in a temporary index
// so that the user's index is never touched, and returns its ID.
func (s *Store) writeTree(ctx context.Context, parent string, files map[string][]byte) (string, error) {
	tmp, err := os.MkdirTemp("", "bfc-index-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(tmp, "index")}

	if parent != "" {
		if _, err := s.git(ctx, env, nil, "read-tree", parent); err != nil {
			return "", err
		}
	}
	// Records end in NUL (-z), so no path needs quoting.
	var entries bytes.Buffer
	for path, content := range files {
		blob, err := s.gitLine(ctx, nil, content, "hash-object", "-w", "--stdin")
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&entries, "100644 %s\t%s\x00", blob, path)
	}
	if _, err := s.git(ctx, env, entries.Bytes(), "update-index", "-z", "--add", "--index-info"); err != nil {
		return "", err
	}
	return s.gitLine(ctx, env, nil, "write-tree")
}

// checkPath rejects anything but a clean, relative, slash-separated path of
// ordinary names.
func checkPath(path string) error {
	if path == "" || strings.ContainsAny(path, "\\\x00\n\r") {
		return fmt.Errorf("store: invalid path %q", path)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.EqualFold(seg, ".git") {
			return fmt.Errorf("store: invalid path %q", path)
		}
	}
	return nil
}

// checkClashes rejects a write that would put a file where a directory is, or
// a directory where a file is, in the parent commit or among the new files.
// git's index would otherwise drop the old entries without a word.
func (s *Store) checkClashes(ctx context.Context, parent string, files map[string][]byte) error {
	dirs := map[string]bool{}
	for path := range files {
		for i := range len(path) {
			if path[i] == '/' {
				dirs[path[:i]] = true
			}
		}
	}
	for path := range files {
		if dirs[path] {
			return fmt.Errorf("store: %s is both a file and a directory in this write", path)
		}
	}
	if parent == "" {
		return nil
	}
	args := []string{"ls-tree", "-z", parent, "--"}
	for path := range files {
		args = append(args, path)
	}
	for dir := range dirs {
		args = append(args, dir)
	}
	out, err := s.git(ctx, nil, nil, args...)
	if err != nil {
		return err
	}
	for _, e := range parseLsTree(out) {
		if _, isFile := files[e.name]; isFile && e.typ == "tree" {
			return fmt.Errorf("store: cannot write file %s over a directory", e.name)
		}
		if dirs[e.name] && e.typ != "tree" {
			return fmt.Errorf("store: cannot write into %s, it is a file", e.name)
		}
	}
	return nil
}

// treeEntry is one line of `git ls-tree` output.
type treeEntry struct {
	typ, id, name string
}

// parseLsTree parses `git ls-tree -z` output, where each entry is
// "<mode> SP <type> SP <id> TAB <name> NUL".
func parseLsTree(out []byte) []treeEntry {
	var entries []treeEntry
	for _, rec := range strings.Split(string(out), "\x00") {
		meta, name, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			continue
		}
		entries = append(entries, treeEntry{typ: fields[1], id: fields[2], name: name})
	}
	return entries
}

// entry returns the tree entry at path in commit, or ErrNotFound.
func (s *Store) entry(ctx context.Context, commit, path string) (treeEntry, error) {
	out, err := s.git(ctx, nil, nil, "ls-tree", "-z", commit, "--", path)
	if err != nil {
		return treeEntry{}, err
	}
	for _, e := range parseLsTree(out) {
		if e.name == path {
			return e, nil
		}
	}
	return treeEntry{}, fmt.Errorf("%w: %s", ErrNotFound, path)
}

// tip returns the commit the plans ref points to, or ErrNotInitialised.
func (s *Store) tip(ctx context.Context) (string, error) {
	commit, err := s.gitLine(ctx, nil, nil, "rev-parse", "--verify", "--quiet", Ref+"^{commit}")
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", ErrNotInitialised
	}
	return commit, err
}

// git runs the system git in the repository and returns its standard output.
// env holds variables on top of those of Store.Environ; stdin may be nil.
// Pathspecs are always literal, so a path such as "a*" names only itself.
func (s *Store) git(ctx context.Context, env []string, stdin []byte, args ...string) ([]byte, error) {
	stdout, stderr, err := s.run(ctx, env, stdin, args...)
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(stderr)))
	}
	return stdout, nil
}

// run runs git like Store.git does and returns both outputs and the bare
// error, for callers that read the output of a failed command.
func (s *Store) run(ctx context.Context, env []string, stdin []byte, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--literal-pathspecs"}, args...)...)
	cmd.Dir = s.dir
	if len(s.env) > 0 || len(env) > 0 {
		cmd.Env = append(s.Environ(), env...)
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

// gitLine runs git like Store.git does and returns its output without the trailing
// newline, for commands that print one line such as an object ID.
func (s *Store) gitLine(ctx context.Context, env []string, stdin []byte, args ...string) (string, error) {
	out, err := s.git(ctx, env, stdin, args...)
	return strings.TrimSpace(string(out)), err
}

// atLeast reports whether the output of `git version`, such as
// "git version 2.39.3 (Apple Git-146)", names a release at or above min.
func atLeast(versionOutput, min string) bool {
	have, ok1 := majorMinor(strings.TrimPrefix(versionOutput, "git version "))
	want, ok2 := majorMinor(min)
	if !ok1 || !ok2 {
		return false
	}
	return have[0] > want[0] || have[0] == want[0] && have[1] >= want[1]
}

// majorMinor parses the leading "X.Y" of a version string.
func majorMinor(v string) ([2]int, bool) {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return [2]int{}, false
	}
	minorDigits := strings.TrimRightFunc(parts[1], func(r rune) bool { return r < '0' || r > '9' })
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(minorDigits)
	if err1 != nil || err2 != nil {
		return [2]int{}, false
	}
	return [2]int{major, minor}, true
}

// firstGitLine returns the first line of what git printed to standard
// error, or err when it printed nothing.
func firstGitLine(stderr []byte, err error) string {
	msg, _, _ := strings.Cut(strings.TrimSpace(string(stderr)), "\n")
	if msg == "" {
		return err.Error()
	}
	return strings.TrimSpace(msg)
}
