package main

import (
	"fmt"
	"os"
	"os/exec"

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
	// Run the editor the way git does: through the shell, so that an editor
	// setting such as "code --wait" works.
	run := exec.CommandContext(cmd.Context(), "sh", "-c", editor+` "$@"`, editor, path)
	run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := run.Run(); err != nil {
		return nil, "", fmt.Errorf("editor %q failed: %w; your text is in %s", editor, err, path)
	}
	edited, err := os.ReadFile(path)
	return edited, path, err
}
