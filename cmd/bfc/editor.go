package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/store"
)

// editText writes text to a temporary file outside the repository, opens
// it in the editor git uses and returns what was saved and the file's path.
func editText(cmd *cobra.Command, s *store.Store, pattern string, text []byte) ([]byte, string, error) {
	editor, err := s.Editor(cmd.Context())
	if err != nil {
		return nil, "", err
	}
	f, err := os.CreateTemp("", "bfc-"+pattern)
	if err != nil {
		return nil, "", err
	}
	path := f.Name()
	if _, err := f.Write(text); err != nil {
		_ = f.Close()
		return nil, "", err
	}
	if err := f.Close(); err != nil {
		return nil, "", err
	}
	gitExe, err := exec.LookPath("git")
	if err != nil {
		return nil, "", err
	}
	name, args, err := editorCommand(runtime.GOOS, gitExe, editor, path, fileExists)
	if err != nil {
		return nil, "", fmt.Errorf("%w; your text is in %s", err, path)
	}
	run := exec.CommandContext(cmd.Context(), name, args...)
	run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := run.Run(); err != nil {
		return nil, "", fmt.Errorf("editor %q failed: %w; your text is in %s", editor, err, path)
	}
	edited, err := os.ReadFile(path)
	return edited, path, err
}

// editorCommand returns the program and arguments that open path in editor
// the way git does: through a POSIX shell, so that a setting such as
// "code --wait" or a quoted path with spaces works. On Windows the shell is
// the sh.exe that comes with Git for Windows, found near gitExe; exists
// reports whether a file is there.
func editorCommand(goos, gitExe, editor, path string, exists func(string) bool) (string, []string, error) {
	args := []string{"-c", editor + ` "$@"`, editor, path}
	if goos != "windows" {
		return "sh", args, nil
	}
	// git.exe lives in <root>\cmd, <root>\bin or <root>\mingw64\bin; sh.exe
	// in <root>\bin or <root>\usr\bin.
	dir := gitExe
	for range 3 {
		i := strings.LastIndexAny(dir, `\/`)
		if i < 0 {
			break
		}
		dir = dir[:i]
		for _, sh := range []string{dir + `\bin\sh.exe`, dir + `\usr\bin\sh.exe`} {
			if exists(sh) {
				return sh, args, nil
			}
		}
	}
	return "", nil, fmt.Errorf("cannot find sh.exe of Git for Windows near %s to start the editor", gitExe)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// textKept explains a failure after editing: what did not happen, and where
// the user's text is, if it was edited.
func textKept(err error, outcome, kept string) error {
	if kept == "" {
		return fmt.Errorf("%w; %s", err, outcome)
	}
	return fmt.Errorf("%w; %s\nYour text is kept in %s", err, outcome, kept)
}
