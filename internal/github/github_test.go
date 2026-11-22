package github_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/admSla99/BeforeCode/internal/github"
)

var update = flag.Bool("update", false, "rewrite .github/workflows/bfc-plan-check.yml from the template")

var ownWorkflow = filepath.Join("..", "..", filepath.FromSlash(github.WorkflowPath))

// Issue #104 AC-5: BeforeCode's own workflow is the template with bfc built
// from source; go test ./internal/github -update rewrites it.
func TestOwnWorkflowIsTheTemplateBuiltFromSource(t *testing.T) {
	t.Parallel()
	want := github.SourceWorkflow()
	if *update {
		if err := os.WriteFile(ownWorkflow, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(ownWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("%s differs from the template; run go test ./internal/github -update", github.WorkflowPath)
	}
	if v, intact, ok := github.ReadMarker(got); !ok || v != github.FromSource || !intact {
		t.Errorf("marker = %q, %v, %v", v, intact, ok)
	}
}

// Issue #104 AC-1: the workflow of a release downloads that release, with
// its checksum, instead of building bfc, and names its version.
func TestWorkflowOfARelease(t *testing.T) {
	t.Parallel()
	data, err := github.Workflow("v0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"- name: Install bfc v0.3.0", "BFC_VERSION: v0.3.0", "checksums.txt", "sha256sum --check",
		"bfc check find", "bfc prime check", "bfc check record", "bfc push",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("workflow lacks %q", want)
		}
	}
	for _, unwanted := range []string{"go build", "setup-go", "[[", "]]"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("workflow has %q", unwanted)
		}
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Errorf("not YAML: %v", err)
	}
	if v, intact, ok := github.ReadMarker(data); !ok || v != "v0.3.0" || !intact {
		t.Errorf("marker = %q, %v, %v", v, intact, ok)
	}
	edited := strings.Replace(text, "cancel-in-progress: false", "cancel-in-progress: true", 1)
	if _, intact, ok := github.ReadMarker([]byte(edited)); !ok || intact {
		t.Errorf("an edited file counts as intact")
	}
	if _, _, ok := github.ReadMarker([]byte("name: Plan check\n")); ok {
		t.Errorf("a file without the marker has one")
	}
}

// Issue #104 AC-1: a dev build, or any version that is no release tag,
// cannot name a release to download.
func TestWorkflowRefusesAVersionWithoutARelease(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"dev", "", "0.3.0", "v1", "main"} {
		if _, err := github.Workflow(v); err == nil {
			t.Errorf("Workflow(%q) accepted it", v)
		}
	}
	if _, err := github.Workflow("dev"); err == nil || !strings.Contains(err.Error(), "release") {
		t.Errorf("Workflow(dev): %v", err)
	}
	if _, err := github.Workflow("v1.2.3-rc.1"); err != nil {
		t.Errorf("a pre-release: %v", err)
	}
}

// Issue #104 AC-6: the workflow downloads the names .goreleaser.yaml
// publishes: bfc_<version without v>_linux_amd64.tar.gz and checksums.txt.
func TestWorkflowDownloadsWhatGoReleaserPublishes(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		ProjectName string `yaml:"project_name"`
		Archives    []struct {
			Formats         []string `yaml:"formats"`
			NameTemplate    string   `yaml:"name_template"`
			FormatOverrides []struct {
				Goos string `yaml:"goos"`
			} `yaml:"format_overrides"`
		} `yaml:"archives"`
		Checksum struct {
			NameTemplate string `yaml:"name_template"`
		} `yaml:"checksum"`
		Builds []struct {
			Goos   []string `yaml:"goos"`
			Goarch []string `yaml:"goarch"`
		} `yaml:"builds"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectName != "bfc" || len(cfg.Archives) != 1 || cfg.Archives[0].NameTemplate != "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}" ||
		strings.Join(cfg.Archives[0].Formats, ",") != "tar.gz" || cfg.Checksum.NameTemplate != "checksums.txt" {
		t.Fatalf(".goreleaser.yaml no longer publishes what the workflow downloads: %+v", cfg)
	}
	for _, o := range cfg.Archives[0].FormatOverrides {
		if o.Goos == "linux" {
			t.Errorf("linux archives are not tar.gz")
		}
	}
	if len(cfg.Builds) != 1 || !strings.Contains(strings.Join(cfg.Builds[0].Goos, ","), "linux") || !strings.Contains(strings.Join(cfg.Builds[0].Goarch, ","), "amd64") {
		t.Errorf("no linux/amd64 build: %+v", cfg.Builds)
	}
	// GoReleaser's .Version is the tag without its v.
	wf, err := github.Workflow("v0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wf), `archive="bfc_${BFC_VERSION#v}_linux_amd64.tar.gz"`) || !strings.Contains(string(wf), "releases/download/$BFC_VERSION") {
		t.Errorf("the workflow does not download bfc_<version>_linux_amd64.tar.gz of the release")
	}
}
