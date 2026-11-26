package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	bfcupdate "github.com/admSla99/BeforeCode/internal/update"
)

// releasesEnv replaces bfcupdate.DefaultBase, for tests and mirrors.
const releasesEnv = "BFC_RELEASES_URL"

// goInstall is how a bfc built from source is updated.
const goInstall = "go install github.com/admSla99/BeforeCode/cmd/bfc@latest"

func newUpdateCmd(version string) *cobra.Command {
	var check, asJSON bool
	cmd := &cobra.Command{
		Use:   "update [<version>]",
		Short: "Install the latest bfc release, or the named one, in place of this binary",
		Long: `Install the latest bfc release, or the named one, in place of this binary.

bfc update finds the latest release of admSla99/BeforeCode on GitHub,
downloads the archive for this system, checks it against the release's
checksums.txt and replaces the running binary. A named <version> is
installed even when it is older (roll back) or a pre-release.

--check only reports: it exits 0 when this binary is the latest release,
1 when a newer one exists and 2 when the lookup fails.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd, version, args, check, asJSON)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only report whether a newer release exists")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the result as JSON")
	return cmd
}

func runUpdate(cmd *cobra.Command, current string, args []string, check, asJSON bool) error {
	// GoReleaser is the only build that sets the version; any other build
	// is updated the way it was installed.
	if current == "dev" {
		err := fmt.Errorf("bfc update: this bfc (dev) was built from source or with go install, so bfc update does not replace it; update it with:\n  %s", goInstall)
		if check {
			return &exitCodeError{code: 2, err: err}
		}
		return err
	}
	if !check {
		if err := checkPerson(cmd); err != nil {
			return fmt.Errorf("bfc update: %w; bfc update --check works inside an agent", err)
		}
	}
	named := ""
	if len(args) == 1 {
		named = args[0]
		if !strings.HasPrefix(named, "v") {
			named = "v" + named
		}
		if !semver.IsValid(named) {
			return fmt.Errorf("bfc update: %q is not a version such as v0.2.0", args[0])
		}
	}

	base := bfcupdate.DefaultBase
	if v, ok := lookupEnv(cmd, releasesEnv); ok && v != "" {
		base = v
	}
	target := named
	if target == "" {
		latest, err := bfcupdate.Latest(cmd.Context(), base)
		if err != nil {
			if check {
				return &exitCodeError{code: 2, err: err}
			}
			return err
		}
		target = latest
	}
	comparable := semver.IsValid(current)
	newer := comparable && semver.Compare(target, current) > 0
	w := cmd.OutOrStdout()

	if check {
		if !comparable {
			return &exitCodeError{code: 2, err: fmt.Errorf("bfc update --check: the version %q of this bfc cannot be compared with %s", current, target)}
		}
		if err := printCheck(w, current, target, newer, asJSON); err != nil {
			return err
		}
		if newer {
			// The output says it; the exit code tells scripts.
			cmd.SilenceErrors = true
			return &exitCodeError{code: 1, err: fmt.Errorf("bfc %s is newer than bfc %s", target, current)}
		}
		return nil
	}

	if target == current || (comparable && semver.Compare(target, current) == 0) {
		if asJSON {
			return printUpdateJSON(w, current, target, false, false)
		}
		_, err := fmt.Fprintf(w, "bfc %s is already installed\n", current)
		return err
	}
	exe, err := executable(cmd)
	if err != nil {
		return fmt.Errorf("bfc update: find this binary: %w", err)
	}
	dir, err := os.MkdirTemp("", "bfc-update-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	bin, err := bfcupdate.Download(cmd.Context(), base, target, runtime.GOOS, runtime.GOARCH, dir)
	if err != nil {
		return fmt.Errorf("bfc update: %w; bfc %s is unchanged", err, current)
	}
	if err := bfcupdate.Replace(exe, bin); err != nil {
		return fmt.Errorf("bfc update: %w; bfc %s is unchanged", err, current)
	}
	if asJSON {
		return printUpdateJSON(w, current, target, newer, true)
	}
	line := fmt.Sprintf("updated bfc %s -> %s", current, target)
	if comparable && semver.Compare(target, current) < 0 {
		line += fmt.Sprintf(" (%s is older than %s)", target, current)
	}
	_, err = fmt.Fprintln(w, line)
	return err
}

func printCheck(w io.Writer, current, latest string, newer, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(w).Encode(struct {
			Current string `json:"current"`
			Latest  string `json:"latest"`
			Newer   bool   `json:"newer"`
		}{current, latest, newer})
	}
	msg := "this is the latest release"
	if newer {
		msg = "a newer release exists; install it with bfc update"
	}
	_, err := fmt.Fprintf(w, "installed  bfc %s\nlatest     bfc %s\n%s\n", current, latest, msg)
	return err
}

func printUpdateJSON(w io.Writer, current, latest string, newer, installed bool) error {
	return json.NewEncoder(w).Encode(struct {
		Current   string `json:"current"`
		Latest    string `json:"latest"`
		Newer     bool   `json:"newer"`
		Installed bool   `json:"installed"`
	}{current, latest, newer, installed})
}
