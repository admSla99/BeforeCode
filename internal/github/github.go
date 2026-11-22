// Package github writes the plan check workflow that bfc setup github puts
// into a repository (ADR 0014). It knows only the file; bfc never calls the
// GitHub API.
package github

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"regexp"
	"text/template"
)

// WorkflowPath is where the workflow lives on normal branches (ADR 0010).
const WorkflowPath = ".github/workflows/bfc-plan-check.yml"

// FromSource is the version in the marker of BeforeCode's own workflow,
// which builds bfc from the pull request instead of downloading a release.
const FromSource = "source"

//go:embed bfc-plan-check.yml.tmpl
var workflowTemplate string

// The template uses [[ ]], because the workflow is full of ${{ }}.
var tmpl = template.Must(template.New("workflow").Delims("[[", "]]").Parse(workflowTemplate))

// releaseTag is a version that names a GitHub release of bfc.
var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

// Workflow returns the workflow for the bfc release version, such as
// v0.3.0: it downloads that release and checks its checksum. A dev build or
// any other version that names no release is refused.
func Workflow(version string) ([]byte, error) {
	if !releaseTag.MatchString(version) {
		return nil, fmt.Errorf("this bfc is version %q, which has no release to download; the workflow runs the release of the bfc that wrote it, so run bfc setup github with a released bfc", version)
	}
	return render(version, false), nil
}

// SourceWorkflow returns BeforeCode's own workflow: the same one, with bfc
// built from the pull request's source.
func SourceWorkflow() []byte {
	return render(FromSource, true)
}

func render(version string, fromSource bool) []byte {
	var b bytes.Buffer
	err := tmpl.Execute(&b, struct {
		Version    string
		FromSource bool
	}{version, fromSource})
	if err != nil {
		panic(err) // the template is fixed and tested
	}
	body := b.Bytes()
	return append([]byte(markerLine(version, body)), body...)
}

// marker is the first line of the workflow: the version of bfc that wrote
// it and the hash of everything after it, so that bfc setup github and bfc
// doctor can tell a file someone changed from one bfc wrote.
var marker = regexp.MustCompile(`^# Written by bfc setup github \(bfc (\S+), sha256 ([0-9a-f]{64})\); run it again to update this file\.\r?\n`)

func markerLine(version string, body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("# Written by bfc setup github (bfc %s, sha256 %s); run it again to update this file.\n", version, hex.EncodeToString(sum[:]))
}

// ReadMarker returns the version of bfc that wrote the workflow in data and
// whether the file is still as it wrote it; ok is false when data has no
// marker. Line endings do not count, so a checkout with CRLF is intact.
func ReadMarker(data []byte) (version string, intact, ok bool) {
	m := marker.FindSubmatchIndex(data)
	if m == nil {
		return "", false, false
	}
	body := bytes.ReplaceAll(data[m[1]:], []byte("\r\n"), []byte("\n"))
	sum := sha256.Sum256(body)
	return string(data[m[2]:m[3]]), hex.EncodeToString(sum[:]) == string(data[m[4]:m[5]]), true
}
