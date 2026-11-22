package beforecode

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// planCheckWorkflow is the shape of .github/workflows/bfc-plan-check.yml
// that issue #103 fixes.
type planCheckWorkflow struct {
	On struct {
		PullRequest struct {
			Types []string `yaml:"types"`
		} `yaml:"pull_request"`
	} `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// Issue #103 AC-1: the plan check runs on pull_request opened, synchronize,
// reopened and edited, may write contents and pull requests, builds bfc
// from the source and fetches the Plans ref.
func TestPlanCheckWorkflowTriggersAndPermissions(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/bfc-plan-check.yml")
	if err != nil {
		t.Fatal(err)
	}
	var w planCheckWorkflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		t.Fatal(err)
	}
	if got := w.On.PullRequest.Types; !slices.Equal(got, []string{"opened", "synchronize", "reopened", "edited"}) {
		t.Errorf("pull_request types = %v", got)
	}
	if w.Permissions["contents"] != "write" || w.Permissions["pull-requests"] != "write" || len(w.Permissions) != 2 {
		t.Errorf("permissions = %v", w.Permissions)
	}
	var runs strings.Builder
	for _, job := range w.Jobs {
		for _, s := range job.Steps {
			runs.WriteString(s.Run + "\n")
		}
	}
	for _, want := range []string{"go build -o", "./cmd/bfc", "bfc bootstrap", "bfc check find", "bfc prime check", "bfc check record", "bfc push"} {
		if !strings.Contains(runs.String(), want) {
			t.Errorf("no step runs %q", want)
		}
	}
}

// The branch name, the title and the description of a pull request are
// written by whoever opens it: the scripts take them from the environment,
// never through ${{ }} inside run, where they would become shell code.
func TestPlanCheckWorkflowKeepsUntrustedTextOutOfScripts(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/bfc-plan-check.yml")
	if err != nil {
		t.Fatal(err)
	}
	var w planCheckWorkflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		t.Fatal(err)
	}
	expr := regexp.MustCompile(`\$\{\{[^}]*\}\}`)
	for _, job := range w.Jobs {
		for _, s := range job.Steps {
			if m := expr.FindString(s.Run); m != "" {
				t.Errorf("step %q puts %s into its script; pass it through env", s.Name, m)
			}
		}
	}
}
