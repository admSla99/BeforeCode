package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	beforecode "github.com/admSla99/BeforeCode"
	"github.com/admSla99/BeforeCode/internal/config"
	"github.com/admSla99/BeforeCode/internal/store"
)

// formatVersion is the content of the format-version file (ADR 0002).
const formatVersion = "1\n"

func newInitCmd() *cobra.Command {
	var local bool
	var remote string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up BeforeCode in this repository",
		Long: `Set up BeforeCode in this repository: write .beforecode/config.yaml and the
default templates, create the plans ref and add its fetch refspec.

Nothing is committed or pushed. Existing files and the plans ref are kept, so
running init again is safe. If the remote already has plans, use bfc bootstrap.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if local && cmd.Flags().Changed("remote") {
				return errors.New("--local and --remote cannot be used together")
			}
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			s, err := store.Open(wd)
			if err != nil {
				return err
			}
			return runInit(cmd, s, local, remote)
		},
	}
	cmd.Flags().BoolVar(&local, "local", false, "keep plans in this clone only; no remote, no sync")
	cmd.Flags().StringVar(&remote, "remote", "origin", "git remote that receives the plans ref")
	return cmd
}

func runInit(cmd *cobra.Command, s *store.Store, local bool, remote string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	root := s.Root()
	cfgFile := filepath.Join(root, filepath.FromSlash(config.Path))

	// Decide everything before writing anything, so a refusal leaves no trace.
	var cfg config.Config
	data, err := os.ReadFile(cfgFile)
	cfgExists := err == nil
	switch {
	case cfgExists:
		if cfg, err = config.Parse(data); err != nil {
			return err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	case local:
		cfg = config.Config{RequiredApprovals: 1}
	default:
		cfg = config.Config{RequiredApprovals: 1, SyncEnabled: true, Remote: remote}
	}
	if cfg.SyncEnabled {
		ok, err := s.HasRemote(ctx, cfg.Remote)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("git remote %q does not exist; name another with --remote, or use --local to keep plans in this clone", cfg.Remote)
		}
	}
	initialised, err := s.Initialised(ctx)
	if err != nil {
		return err
	}
	if !initialised && cfg.SyncEnabled {
		has, err := s.RemoteHasPlans(ctx, cfg.Remote)
		if errors.Is(err, store.ErrRemoteUnreachable) {
			return fmt.Errorf("cannot check whether %s already has plans: %w\nTry again when the remote can be reached, or use --local", cfg.Remote, err)
		}
		if err != nil {
			return err
		}
		if has {
			return fmt.Errorf("%s already has plans in %s; run bfc bootstrap to join them", cfg.Remote, store.Ref)
		}
	}

	if cfgExists {
		report(out, "kept", config.Path)
	} else {
		if err := writeNew(cfgFile, config.Render(cfg)); err != nil {
			return err
		}
		report(out, "created", config.Path)
	}
	if err := copyTemplates(out, root); err != nil {
		return err
	}
	if initialised {
		report(out, "kept", store.Ref)
	} else {
		if _, err := s.Write(ctx, "bfc init", map[string][]byte{"format-version": []byte(formatVersion)}); err != nil {
			return err
		}
		report(out, "created", store.Ref)
	}
	if cfg.SyncEnabled {
		added, err := s.AddFetchRefspec(ctx, cfg.Remote)
		if err != nil {
			return err
		}
		what := "fetch refspec " + store.FetchRefspec + " for " + cfg.Remote
		if added {
			report(out, "added", what)
		} else {
			report(out, "kept", what)
		}
	} else {
		report(out, "local", "sync is off; plans stay in this clone")
	}

	next := "\nNext: git add .beforecode && git commit -m \"Set up BeforeCode\""
	if cfg.SyncEnabled {
		next += ", then bfc push"
	}
	_, err = fmt.Fprintln(out, next)
	return err
}

// copyTemplates writes each default template that is not there yet.
func copyTemplates(out io.Writer, root string) error {
	return fs.WalkDir(beforecode.Templates, ".beforecode/templates", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := beforecode.Templates.ReadFile(name)
		if err != nil {
			return err
		}
		err = writeNew(filepath.Join(root, filepath.FromSlash(name)), data)
		switch {
		case errors.Is(err, fs.ErrExist):
			report(out, "kept", name)
			return nil
		case err != nil:
			return err
		}
		report(out, "created", name)
		return nil
	})
}

// writeNew creates the file at name with data, and its directory. It fails
// with fs.ErrExist rather than overwrite an existing file.
func writeNew(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func report(out io.Writer, action, what string) {
	_, _ = fmt.Fprintf(out, "%-8s %s\n", action, what)
}
