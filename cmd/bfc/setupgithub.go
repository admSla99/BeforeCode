package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/github"
)

func newSetupGithubCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Set up the plan check of pull requests in GitHub Actions",
		Long: `Write ` + github.WorkflowPath + `, the plan check (ADR 0014): on every
pull request Claude Code compares the change with the plan.md of its plan,
the result is stored in the Plans ref, and a pull request that departs from
its plan gets the plan-mismatch label and a comment. The workflow runs the
release of this bfc, which it downloads and checks against checksums.txt, so
a dev build cannot write it.

Running it again rewrites the file for the installed bfc. A file someone
changed by hand is kept, and the difference is shown, unless --force.
Nothing is committed: commit the file through a pull request.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSetupGithub(cmd, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace a workflow that was changed by hand")
	return cmd
}

func runSetupGithub(cmd *cobra.Command, force bool) error {
	version := bfcVersion(cmd)
	want, err := github.Workflow(version)
	if err != nil {
		return err
	}
	s, err := openStore(cmd)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	file := filepath.Join(s.Root(), filepath.FromSlash(github.WorkflowPath))
	old, err := readOptional(file)
	if err != nil {
		return err
	}
	action, what := "created", github.WorkflowPath
	if old != nil {
		state, wrote := github.Check(old, version)
		switch {
		case state == github.Current:
			action = "kept"
		case state == github.ChangedByHand && !force:
			_, _ = fmt.Fprintf(out, "The changes (- yours, + bfc %s's):\n%s", version, lineDiff(string(old), string(want)))
			return fmt.Errorf("%s was changed by hand, so it is kept; run bfc setup github --force to replace it", github.WorkflowPath)
		case state == github.Source && !force:
			return fmt.Errorf("%s builds bfc from source, as BeforeCode's own workflow does; run bfc setup github --force to replace it with the release workflow of bfc %s", github.WorkflowPath, version)
		case state == github.OtherBfc:
			action, what = "updated", fmt.Sprintf("%s from bfc %s to %s", github.WorkflowPath, wrote, version)
		default:
			action = "replaced"
		}
	}
	if action != "kept" {
		if err := writeCreating(file, want); err != nil {
			return err
		}
	}
	report(out, action, what)
	_, err = fmt.Fprint(out, `
Next:
  1. Commit the workflow on a branch and merge it through a pull request:
     git switch -c plan-check && git add `+github.WorkflowPath+` && git commit -m "Check pull requests against their plans"
  2. Add the ANTHROPIC_API_KEY repository secret, which the plan check uses to run Claude Code:
     gh secret set ANTHROPIC_API_KEY   (or Settings > Secrets and variables > Actions)
The workflow creates the plan-mismatch label the first time a pull request departs from its plan.
`)
	return err
}

// lineDiff shows how a and b differ, line by line, as a unified diff
// does: "-" before lines only in a, "+" before lines only in b, a space
// before two lines of context around each change; at most 40 lines.
func lineDiff(a, b string) string {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	// lcs[i][j] is the length of the longest common subsequence of x[i:]
	// and y[j:].
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type line struct {
		op   byte // ' ', '-' or '+'
		text string
	}
	var all []line
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			all = append(all, line{' ', x[i]})
			i, j = i+1, j+1
		case j < len(y) && (i == len(x) || lcs[i][j+1] >= lcs[i+1][j]):
			all = append(all, line{'+', y[j]})
			j++
		default:
			all = append(all, line{'-', x[i]})
			i++
		}
	}
	var out strings.Builder
	shown, last := 0, -1
	for k, l := range all {
		near := false
		for d := max(k-2, 0); d <= min(k+2, len(all)-1); d++ {
			near = near || all[d].op != ' '
		}
		if !near {
			continue
		}
		if shown == 40 {
			out.WriteString(" …\n")
			break
		}
		if last >= 0 && k > last+1 {
			out.WriteString(" …\n")
		}
		fmt.Fprintf(&out, "%c%s\n", l.op, l.text)
		shown, last = shown+1, k
	}
	return out.String()
}
