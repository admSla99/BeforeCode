package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/store"
)

// place is where a command runs: the directory it treats as its working
// directory and the environment variables git and the editor get on top of
// those of the process. bfc runs in the process's working directory with its
// environment; tests give each command its own place, so that they do not
// share the process's working directory or environment and can run in
// parallel.
type place struct {
	dir string
	env []string
	// executable is the binary bfc update replaces, instead of the running
	// one, so that tests do not replace the test binary.
	executable string
}

type placeKey struct{}

// withPlace returns ctx with the place commands run in.
func withPlace(ctx context.Context, p place) context.Context {
	return context.WithValue(ctx, placeKey{}, p)
}

func placeOf(cmd *cobra.Command) place {
	if ctx := cmd.Context(); ctx != nil {
		if p, ok := ctx.Value(placeKey{}).(place); ok {
			return p
		}
	}
	return place{}
}

// workDir is the working directory of the command.
func workDir(cmd *cobra.Command) (string, error) {
	if p := placeOf(cmd); p.dir != "" {
		return p.dir, nil
	}
	return os.Getwd()
}

// executable is the path of the bfc binary the command replaces, with
// symlinks resolved.
func executable(cmd *cobra.Command) (string, error) {
	if p := placeOf(cmd); p.executable != "" {
		return p.executable, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// extraEnv is the environment the command adds for git and the editor.
func extraEnv(cmd *cobra.Command) []string {
	return placeOf(cmd).env
}

// openStore opens the store of the repository around the working directory
// of the command, without checking the plans ref; openPlans checks it.
func openStore(cmd *cobra.Command) (*store.Store, error) {
	wd, err := workDir(cmd)
	if err != nil {
		return nil, err
	}
	return store.Open(wd, extraEnv(cmd)...)
}
