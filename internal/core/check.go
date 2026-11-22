package core

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"go.yaml.in/yaml/v3"
)

// CriterionStatus is what a Plan check says about one acceptance
// criterion (ADR 0013 section 6).
type CriterionStatus string

// The statuses of a criterion in a Plan check.
const (
	Met          CriterionStatus = "met"
	NotMet       CriterionStatus = "not-met"
	CannotVerify CriterionStatus = "cannot-verify"
)

func (s CriterionStatus) valid() bool {
	return s == Met || s == NotMet || s == CannotVerify
}

// badStatus is the problem of a criterion with another status.
func badStatus(id, status string) string {
	return fmt.Sprintf("%s: status %q is not %s, %s or %s", id, status, Met, NotMet, CannotVerify)
}

var criterionID = regexp.MustCompile(`^AC-[1-9][0-9]*$`)

// CriterionResult is the status of one acceptance criterion in a check.
type CriterionResult struct {
	ID     string // AC-<n>
	Status CriterionStatus
}

// Check is the result of one Plan check of a pull request, stored as
// plans/<plan ULID>/checks/<check ULID>.md (ADR 0013 section 6).
type Check struct {
	ID     ulid.ULID // from the file name
	Author Person
	At     time.Time
	Agent  string

	Plan     ulid.ULID
	PR       int
	HeadSHA  string
	BaseSHA  string
	PlanBlob string // the object ID of the plan.md that was checked
	// Criteria lists every acceptance criterion of that plan.md, in its
	// order.
	Criteria   []CriterionResult
	OutOfScope []string
	Summary    string // Markdown, the comment posted to the pull request
}

// CheckPath is the file of the check of the plan with ULID plan.
func CheckPath(plan, check ulid.ULID) string {
	return PlanDir(plan) + "/checks/" + check.String() + ".md"
}

// checkFile is the YAML front matter of a check file. Criteria is a node so
// that the criteria keep the order of plan.md.
type checkFile struct {
	Author     Person    `yaml:"author"`
	At         time.Time `yaml:"at"`
	Agent      string    `yaml:"agent,omitempty"`
	Plan       string    `yaml:"plan"`
	PR         int       `yaml:"pr"`
	HeadSHA    string    `yaml:"head_sha"`
	BaseSHA    string    `yaml:"base_sha"`
	PlanBlob   string    `yaml:"plan_blob"`
	Criteria   yaml.Node `yaml:"criteria"`
	OutOfScope []string  `yaml:"out_of_scope"`
}

// EncodeCheck returns the file content of c, with LF line endings. The ID
// is the file name and is not part of the content.
func EncodeCheck(c Check) []byte {
	criteria := yaml.Node{Kind: yaml.MappingNode}
	for _, r := range c.Criteria {
		criteria.Content = append(criteria.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: r.ID},
			&yaml.Node{Kind: yaml.ScalarNode, Value: string(r.Status)})
	}
	outOfScope := c.OutOfScope
	if outOfScope == nil {
		outOfScope = []string{}
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	err := enc.Encode(checkFile{
		Author: c.Author, At: c.At.UTC().Truncate(time.Second), Agent: c.Agent,
		Plan: c.Plan.String(), PR: c.PR, HeadSHA: c.HeadSHA, BaseSHA: c.BaseSHA, PlanBlob: c.PlanBlob,
		Criteria: criteria, OutOfScope: outOfScope,
	})
	if err != nil {
		panic(err) // plain strings and numbers always encode
	}
	body := strings.TrimSpace(strings.ReplaceAll(c.Summary, "\r\n", "\n"))
	b.WriteString("---\n\n" + body + "\n")
	return b.Bytes()
}

// ParseCheck reads the check with the given ID from its file content. It
// fails when the front matter is missing or broken, lacks the author email,
// the time, the plan or the pull request, or has a criterion status that is
// not met, not-met or cannot-verify.
func ParseCheck(id ulid.ULID, data []byte) (Check, error) {
	fail := func(err error) (Check, error) { return Check{}, fmt.Errorf("check %s: %w", id, err) }
	fm, body, ok := splitFrontMatter(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	if !ok {
		return fail(errors.New("front matter between two --- lines is missing"))
	}
	var f checkFile
	if err := yaml.Unmarshal(fm, &f); err != nil {
		return fail(err)
	}
	plan, err := ulid.ParseStrict(f.Plan)
	switch {
	case f.Author.Email == "" || f.At.IsZero():
		return fail(errors.New("author email and at are required"))
	case err != nil:
		return fail(fmt.Errorf("plan %q is not a ULID", f.Plan))
	case f.PR <= 0:
		return fail(errors.New("pr must be a pull request number"))
	case f.HeadSHA == "" || f.BaseSHA == "" || f.PlanBlob == "":
		return fail(errors.New("head_sha, base_sha and plan_blob are required"))
	case f.Criteria.Kind != yaml.MappingNode:
		return fail(errors.New("criteria must map each AC to its status"))
	}
	c := Check{
		ID: id, Author: f.Author, At: f.At, Agent: f.Agent, Plan: plan, PR: f.PR,
		HeadSHA: f.HeadSHA, BaseSHA: f.BaseSHA, PlanBlob: f.PlanBlob, OutOfScope: f.OutOfScope,
		Summary: strings.TrimSpace(string(body)),
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(f.Criteria.Content); i += 2 {
		k, v := f.Criteria.Content[i], f.Criteria.Content[i+1]
		switch {
		case k.Kind != yaml.ScalarNode || !criterionID.MatchString(k.Value):
			return fail(fmt.Errorf("criteria key %q is not AC-<n>", k.Value))
		case seen[k.Value]:
			return fail(fmt.Errorf("%s is in criteria more than once", k.Value))
		case v.Kind != yaml.ScalarNode || !CriterionStatus(v.Value).valid():
			return fail(errors.New(badStatus(k.Value, v.Value)))
		}
		seen[k.Value] = true
		c.Criteria = append(c.Criteria, CriterionResult{ID: k.Value, Status: CriterionStatus(v.Value)})
	}
	return c, nil
}

// CheckCriteria validates the criteria of a check result against plan.md:
// every acceptance criterion of plan.md has a result, the result names no
// other, and each status is met, not-met or cannot-verify. It returns the
// results in the order of plan.md, or an error that lists every problem.
func CheckCriteria(plan []byte, criteria map[string]string) ([]CriterionResult, error) {
	ids := AcceptanceCriteria(plan)
	var problems []string
	var extra []string
	for id := range criteria {
		if !slices.Contains(ids, id) {
			extra = append(extra, id)
		}
	}
	slices.Sort(extra)
	for _, id := range extra {
		problems = append(problems, id+" is not an acceptance criterion of plan.md")
	}
	var out []CriterionResult
	for _, id := range ids {
		s, ok := criteria[id]
		switch {
		case !ok:
			problems = append(problems, id+" of plan.md has no result")
		case !CriterionStatus(s).valid():
			problems = append(problems, badStatus(id, s))
		default:
			out = append(out, CriterionResult{ID: id, Status: CriterionStatus(s)})
		}
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "\n"))
	}
	return out, nil
}

// Finding is a mismatch in the newest Plan check of a pull request: an
// acceptance criterion that is not met (Criterion set) or an out-of-scope
// change (OutOfScope set). Cannot-verify is not a Finding (ADR 0013
// section 6).
type Finding struct {
	PR         int
	Check      ulid.ULID
	Criterion  string
	OutOfScope string
	// Acknowledged is set when its check has an Acknowledgement: the
	// Finding no longer waits on anyone.
	Acknowledged bool
}

// Text says what the Finding is: the criterion that is not met, or the
// out-of-scope change.
func (f Finding) Text() string {
	if f.Criterion != "" {
		return f.Criterion + " is not met"
	}
	return "out of scope: " + f.OutOfScope
}

// PullRequestCheck is the check that counts for one pull request: its
// newest, with its Findings. Stale is set when it checked another plan.md
// than the current one; a stale check still counts until the next check of
// that pull request.
type PullRequestCheck struct {
	Check    Check
	Stale    bool
	Findings []Finding
	// Unverified lists the criteria the check could not verify. They are
	// shown, but are not Findings.
	Unverified []string
	// Acknowledgement is the first Acknowledgement of the check, nil when
	// it has none (ADR 0013 section 7).
	Acknowledgement *Message
}

// PullRequestChecks returns, for each pull request with a check among
// checks, the newest one by ULID, ordered by pull request number. planBlob
// is the object ID of the current plan.md; msgs are the plan's Messages in
// ULID order, among them the Acknowledgements (see IsAcknowledgement). An
// Acknowledgement covers only the check it names, so a newer check of the
// same pull request needs its own.
func PullRequestChecks(checks []Check, planBlob string, msgs []Message) []PullRequestCheck {
	acks := map[ulid.ULID]*Message{}
	for i, m := range msgs {
		if id, ok := m.Acknowledges(); ok && acks[id] == nil {
			acks[id] = &msgs[i]
		}
	}
	newest := map[int]Check{}
	for _, c := range checks {
		if n, ok := newest[c.PR]; !ok || c.ID.Compare(n.ID) > 0 {
			newest[c.PR] = c
		}
	}
	out := make([]PullRequestCheck, 0, len(newest))
	for _, c := range newest {
		p := PullRequestCheck{Check: c, Stale: c.PlanBlob != planBlob, Acknowledgement: acks[c.ID]}
		for _, r := range c.Criteria {
			switch r.Status {
			case NotMet:
				p.Findings = append(p.Findings, Finding{PR: c.PR, Check: c.ID, Criterion: r.ID})
			case CannotVerify:
				p.Unverified = append(p.Unverified, r.ID)
			}
		}
		for _, item := range c.OutOfScope {
			p.Findings = append(p.Findings, Finding{PR: c.PR, Check: c.ID, OutOfScope: item})
		}
		for i := range p.Findings {
			p.Findings[i].Acknowledged = p.Acknowledgement != nil
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b PullRequestCheck) int { return a.Check.PR - b.Check.PR })
	return out
}

// Findings returns the Findings of prs in their order.
func Findings(prs []PullRequestCheck) []Finding {
	var out []Finding
	for _, p := range prs {
		out = append(out, p.Findings...)
	}
	return out
}
