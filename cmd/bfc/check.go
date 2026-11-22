package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Plan check results of pull requests",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newCheckRecordCmd())
	return cmd
}

func newCheckRecordCmd() *cobra.Command {
	var pr int
	var head, base string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "record <id> --pr <n> --head <sha> --base <sha>",
		Short: "Store the result of a plan check of a pull request",
		Long: `Store the result of a plan check of a pull request as one new file in the
plans ref (ADR 0013 section 6). The result is JSON on stdin:

  {"criteria": {"AC-1": "met", "AC-2": "not-met", "AC-3": "cannot-verify"},
   "out_of_scope": ["Changes the retry policy of the email worker."],
   "summary": "<Markdown posted to the pull request>"}

criteria must name every acceptance criterion of the current plan.md and no
other, each met, not-met or cannot-verify; otherwise nothing is written and
every problem is listed. Prints the ULID of the check, or with --json the
stored check. Nothing is pushed; bfc push sends it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if pr <= 0 {
				return errors.New("name the pull request with --pr <number>")
			}
			if head == "" || base == "" {
				return errors.New("give the commits of the pull request with --head <sha> and --base <sha>")
			}
			result, err := readCheckResult(cmd.InOrStdin())
			if err != nil {
				return err
			}
			s, err := openPlans(cmd, forWriting)
			if err != nil {
				return err
			}
			v, err := resolvePlan(cmd.Context(), s, args[0])
			if err != nil {
				return err
			}
			c, err := recordCheck(cmd, s, v, core.Check{PR: pr, HeadSHA: head, BaseSHA: base}, result)
			if err != nil {
				return err
			}
			if !asJSON {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), c.ID)
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(toCheckJSON(v, c))
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "the number of the pull request")
	cmd.Flags().StringVar(&head, "head", "", "the head commit of the pull request")
	cmd.Flags().StringVar(&base, "base", "", "the base commit of the pull request")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the stored check as JSON")
	return cmd
}

// checkResult is the JSON a plan check ends with (ADR 0014).
type checkResult struct {
	Criteria   map[string]string `json:"criteria"`
	OutOfScope []string          `json:"out_of_scope"`
	Summary    string            `json:"summary"`
}

// lastJSONFence finds the last ```json fence of a reply and its content.
var lastJSONFence = regexp.MustCompile("(?s).*```json[ \t]*\r?\n(.*?)\r?\n[ \t]*```")

// readCheckResult parses the result on r, refusing unknown fields and an
// empty summary. r holds the JSON object itself, or the agent's whole
// reply, whose last json fence holds it (the plan-check skill).
func readCheckResult(r io.Reader) (checkResult, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return checkResult{}, err
	}
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		m := lastJSONFence.FindSubmatch(data)
		if m == nil {
			return checkResult{}, errors.New("the check result on stdin is neither a JSON object nor a reply that ends with one in a ```json fence")
		}
		data = m[1]
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var res checkResult
	if err := dec.Decode(&res); err != nil {
		return checkResult{}, fmt.Errorf("the check result on stdin is not the expected JSON: %w", err)
	}
	if strings.TrimSpace(res.Summary) == "" {
		return checkResult{}, errors.New("the check result has no summary")
	}
	for _, item := range res.OutOfScope {
		if strings.TrimSpace(item) == "" {
			return checkResult{}, errors.New("the check result has an empty out_of_scope item")
		}
	}
	return res, nil
}

// recordCheck validates res against the current plan.md of v, completes c
// with it, the current user, the time and a new ULID, and writes it as one
// new file in one commit.
func recordCheck(cmd *cobra.Command, s *store.Store, v index.Plan, c core.Check, res checkResult) (core.Check, error) {
	ctx := cmd.Context()
	if v.Status.State == core.StateClosed {
		return core.Check{}, fmt.Errorf("%s is closed; closed is final and takes no checks", v.ShortID)
	}
	blob, plan, err := currentPlanDoc(ctx, s, v.ID)
	if errors.Is(err, store.ErrNotFound) {
		return core.Check{}, fmt.Errorf("%s has no plan.md to check against", v.ShortID)
	}
	if err != nil {
		return core.Check{}, err
	}
	criteria, err := core.CheckCriteria(plan, res.Criteria)
	if err != nil {
		return core.Check{}, fmt.Errorf("the check result does not match plan.md of %s:\n%w", v.ShortID, err)
	}
	actor, err := currentUser(ctx, s)
	if err != nil {
		return core.Check{}, err
	}
	c.Author, c.At, c.Agent = actor, time.Now(), insideAgent(cmd)
	c.ID = ulid.MustNew(ulid.Timestamp(c.At), rand.Reader)
	c.Plan, c.PlanBlob, c.Criteria = v.ID, blob, criteria
	c.OutOfScope, c.Summary = res.OutOfScope, res.Summary
	data := core.EncodeCheck(c)
	if _, err := s.Write(ctx, fmt.Sprintf("bfc check record %s #%d", v.ShortID, c.PR), map[string][]byte{core.CheckPath(v.ID, c.ID): data}); err != nil {
		return core.Check{}, err
	}
	// As stored: whole seconds, trimmed summary.
	return core.ParseCheck(c.ID, data)
}

// currentPlanDoc returns the object ID and the content of the plan's
// plan.md, both from the same tip of the plans ref, or store.ErrNotFound.
func currentPlanDoc(ctx context.Context, s *store.Store, plan ulid.ULID) (string, []byte, error) {
	tip, err := s.Tip(ctx)
	if err != nil {
		return "", nil, err
	}
	entries, err := s.Entries(ctx, tip, core.PlanDir(plan))
	if err != nil {
		return "", nil, err
	}
	for _, e := range entries {
		if e.Name == "plan.md" && !e.Dir {
			content, err := s.Blobs(ctx, []string{e.ID})
			if err != nil {
				return "", nil, err
			}
			return e.ID, content[e.ID], nil
		}
	}
	return "", nil, store.ErrNotFound
}

// planChecks returns, for each pull request of the plan, the check that
// counts with its Acknowledgement among msgs, the plan's Messages
// (core.PullRequestChecks). A file that cannot be read as a check, or names
// another plan, is skipped; bfc doctor reports it.
func planChecks(ctx context.Context, s *store.Store, plan ulid.ULID, msgs []core.Message) ([]core.PullRequestCheck, error) {
	files, err := readPlanFiles(ctx, s, plan, "checks")
	if err != nil {
		return nil, err
	}
	var checks []core.Check
	for _, f := range files {
		if c, err := core.ParseCheck(f.id, f.data); err == nil && c.Plan == plan {
			checks = append(checks, c)
		}
	}
	blob, err := s.ObjectID(ctx, core.PlanDir(plan)+"/plan.md")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	return core.PullRequestChecks(checks, blob, msgs), nil
}

// planFile is one append-only file of a plan, named by its ULID.
type planFile struct {
	id   ulid.ULID
	data []byte
}

// readPlanFiles returns the <ULID>.md files in the directory dir of a plan
// (messages, checks) at the tip of the plans ref, in ULID order. Other
// entries are ignored.
func readPlanFiles(ctx context.Context, s *store.Store, plan ulid.ULID, dir string) ([]planFile, error) {
	tip, err := s.Tip(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := s.Entries(ctx, tip, core.PlanDir(plan)+"/"+dir)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []ulid.ULID
	var oids []string
	for _, e := range entries {
		id, err := ulid.ParseStrict(strings.TrimSuffix(e.Name, ".md"))
		if err != nil || e.Dir || !strings.HasSuffix(e.Name, ".md") {
			continue
		}
		ids, oids = append(ids, id), append(oids, e.ID)
	}
	content, err := s.Blobs(ctx, oids)
	if err != nil {
		return nil, err
	}
	files := make([]planFile, len(ids))
	for i, id := range ids {
		files[i] = planFile{id: id, data: content[oids[i]]}
	}
	return files, nil
}

// checkJSON is the --json shape of a check, documented in docs/design.md.
type checkJSON struct {
	ID         string          `json:"id"`
	Plan       string          `json:"plan"`
	ShortID    string          `json:"short_id"`
	Author     core.Person     `json:"author"`
	At         string          `json:"at"`
	Agent      string          `json:"agent,omitempty"`
	PR         int             `json:"pr"`
	HeadSHA    string          `json:"head_sha"`
	BaseSHA    string          `json:"base_sha"`
	PlanBlob   string          `json:"plan_blob"`
	Criteria   []criterionJSON `json:"criteria"`
	OutOfScope []string        `json:"out_of_scope"`
	Summary    string          `json:"summary"`
}

// countingCheckJSON is the check that counts for its pull request, in the
// checks of bfc show --json: with its stale marker and its Acknowledgement,
// null without one.
type countingCheckJSON struct {
	checkJSON
	Stale        bool              `json:"stale"`
	Acknowledged *acknowledgedJSON `json:"acknowledged"`
}

// acknowledgedJSON is the Acknowledgement of a check: the Message and its
// reason.
type acknowledgedJSON struct {
	ID     string      `json:"id"`
	Author core.Person `json:"author"`
	At     string      `json:"at"`
	Reason string      `json:"reason"`
}

type criterionJSON struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// findingJSON is a Finding in bfc show --json: a criterion that is not met
// or an out-of-scope change.
type findingJSON struct {
	PR           int    `json:"pr"`
	Check        string `json:"check"`
	Criterion    string `json:"criterion,omitempty"`
	OutOfScope   string `json:"out_of_scope,omitempty"`
	Acknowledged bool   `json:"acknowledged"`
}

// toCountingCheckJSON is the check that counts for its pull request, with
// its stale marker and Acknowledgement.
func toCountingCheckJSON(v index.Plan, p core.PullRequestCheck) countingCheckJSON {
	out := countingCheckJSON{checkJSON: toCheckJSON(v, p.Check), Stale: p.Stale}
	if a := p.Acknowledgement; a != nil {
		out.Acknowledged = &acknowledgedJSON{ID: a.ID.String(), Author: a.Author, At: a.At.UTC().Format(time.RFC3339), Reason: a.Body}
	}
	return out
}

func toCheckJSON(v index.Plan, c core.Check) checkJSON {
	out := checkJSON{
		ID: c.ID.String(), Plan: v.ID.String(), ShortID: v.ShortID, Author: c.Author,
		At: c.At.UTC().Format(time.RFC3339), Agent: c.Agent, PR: c.PR, HeadSHA: c.HeadSHA, BaseSHA: c.BaseSHA,
		PlanBlob: c.PlanBlob, Criteria: []criterionJSON{}, OutOfScope: append([]string{}, c.OutOfScope...),
		Summary: c.Summary,
	}
	for _, r := range c.Criteria {
		out.Criteria = append(out.Criteria, criterionJSON{ID: r.ID, Status: string(r.Status)})
	}
	return out
}

func toFindingJSON(f core.Finding) findingJSON {
	return findingJSON{PR: f.PR, Check: f.Check.String(), Criterion: f.Criterion, OutOfScope: f.OutOfScope, Acknowledged: f.Acknowledged}
}

// writeChecks prints the Checks section of bfc show: per pull request its
// newest check, the stale marker and the Findings.
func writeChecks(b *strings.Builder, prs []core.PullRequestCheck) {
	if len(prs) == 0 {
		return
	}
	b.WriteString("\n--- Checks ---\n")
	for _, p := range prs {
		c := p.Check
		fields := []string{fmt.Sprintf("PR #%d", c.PR), c.ID.String()}
		if p.Stale {
			fields = append(fields, "stale: plan.md changed since")
		}
		if len(p.Findings) == 0 {
			fields = append(fields, "no findings")
		}
		fields = append(fields, formatTime(c.At))
		fmt.Fprintf(b, "%s\n", strings.Join(fields, "  "))
		if a := p.Acknowledgement; a != nil {
			fmt.Fprintf(b, "  acknowledged by %s: %s\n", orDash(a.Author.Name), firstLine(a.Body))
		}
		for _, f := range p.Findings {
			if f.Criterion != "" {
				fmt.Fprintf(b, "  not met: %s\n", f.Criterion)
			} else {
				fmt.Fprintf(b, "  out of scope: %s\n", f.OutOfScope)
			}
		}
		for _, id := range p.Unverified {
			fmt.Fprintf(b, "  cannot verify: %s\n", id)
		}
	}
}
