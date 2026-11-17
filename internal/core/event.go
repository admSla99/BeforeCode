package core

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"go.yaml.in/yaml/v3"
)

// Person is someone who writes to the plans ref, identified by email
// (compared case-insensitively). It comes from user.name and user.email in
// git config.
type Person struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

// EventType names a kind of Event (ADR 0002 section 4).
type EventType string

// The Event types of format version 1.
const (
	Created          EventType = "created"
	Submitted        EventType = "submitted"
	Withdrawn        EventType = "withdrawn"
	Approved         EventType = "approved"
	ChangesRequested EventType = "changes-requested"
	Closed           EventType = "closed"
)

// Event is an append-only record of a state change of a Plan, stored as
// plans/<plan ULID>/events/<event ULID>.yaml. Fields that do not belong to
// its Type are empty.
type Event struct {
	ID     ulid.ULID // from the file name
	Type   EventType
	Plan   ulid.ULID
	Author Person
	At     time.Time
	Agent  string // set when an AI wrote the event through a harness

	ShortID           string // created
	PlanBlob          string // submitted, approved, changes-requested
	RequiredApprovals int    // submitted
	Message           string // changes-requested, closed: ULID of a Message
}

// eventFile is the YAML layout: common fields first, then the fields of the
// event type.
type eventFile struct {
	Type              EventType `yaml:"type"`
	Plan              string    `yaml:"plan"`
	Author            Person    `yaml:"author"`
	At                time.Time `yaml:"at"`
	Agent             string    `yaml:"agent,omitempty"`
	ShortID           string    `yaml:"short_id,omitempty"`
	PlanBlob          string    `yaml:"plan_blob,omitempty"`
	RequiredApprovals int       `yaml:"required_approvals,omitempty"`
	Message           string    `yaml:"message,omitempty"`
}

// EncodeEvent returns the file content of e.
func EncodeEvent(e Event) []byte {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	err := enc.Encode(eventFile{
		Type:              e.Type,
		Plan:              e.Plan.String(),
		Author:            e.Author,
		At:                e.At.UTC().Truncate(time.Second),
		Agent:             e.Agent,
		ShortID:           e.ShortID,
		PlanBlob:          e.PlanBlob,
		RequiredApprovals: e.RequiredApprovals,
		Message:           e.Message,
	})
	if err != nil {
		panic(err) // plain strings and ints always encode
	}
	return b.Bytes()
}

// ParseEvent reads the event file named by id. Unknown fields are ignored,
// as ADR 0002 requires of readers.
func ParseEvent(id ulid.ULID, data []byte) (Event, error) {
	var f eventFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Event{}, fmt.Errorf("event %s: %w", id, err)
	}
	if f.Type == "" {
		return Event{}, fmt.Errorf("event %s: type is missing", id)
	}
	plan, err := ulid.ParseStrict(f.Plan)
	if err != nil {
		return Event{}, fmt.Errorf("event %s: plan %q: %w", id, f.Plan, err)
	}
	if f.Author.Email == "" {
		return Event{}, fmt.Errorf("event %s: author.email is missing", id)
	}
	if f.At.IsZero() {
		return Event{}, fmt.Errorf("event %s: at is missing", id)
	}
	return Event{
		ID: id, Type: f.Type, Plan: plan, Author: f.Author, At: f.At.UTC(), Agent: f.Agent,
		ShortID: f.ShortID, PlanBlob: f.PlanBlob, RequiredApprovals: f.RequiredApprovals, Message: f.Message,
	}, nil
}

// State is the Plan state, derived from Events and never stored.
type State string

// Plan states (CONTEXT.md). StateInvalid is not a lifecycle state: it marks
// a plan without a valid created event, which bfc doctor reports.
const (
	StateDraft      State = "draft"
	StateInReview   State = "in-review"
	StateApproved   State = "approved"
	StateInProgress State = "in-progress"
	StateDone       State = "done"
	StateClosed     State = "closed"
	StateInvalid    State = "invalid"
)

// Reserved Event types (ADR 0002 section 4): a format version 1 binary never
// writes them.
const (
	Claimed EventType = "claimed"
	Checked EventType = "checked"
	Done    EventType = "done"
)

// objectIDPattern matches a git object ID: SHA-1 or SHA-256 in hex.
var objectIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// ValidateEvent checks the event file name in the events directory of plan
// against the data format (ADR 0002 sections 1, 3 and 4): a name of an
// upper-case ULID and ".yaml", the common fields, a plan field equal to the
// plan, a type of format version 1 and the fields that type requires.
// Unknown fields are allowed. It does not check whether the event is allowed
// for its author or fits the plan state; SkippedEvents reports those.
func ValidateEvent(plan ulid.ULID, name string, data []byte) error {
	base, ok := strings.CutSuffix(name, ".yaml")
	id, err := ulid.ParseStrict(base)
	if !ok || err != nil || base != id.String() {
		return fmt.Errorf("file name %q is not an upper-case ULID followed by .yaml", name)
	}
	e, err := ParseEvent(id, data)
	if err != nil {
		return err
	}
	if e.Plan != plan {
		return fmt.Errorf("event %s: plan %s is not the plan directory %s", id, e.Plan, plan)
	}
	switch e.Type {
	case Created:
		own := randomPart(plan)
		n := len(e.ShortID) - len(ShortIDPrefix)
		if n < minShortIDLen || n > maxShortIDLen || e.ShortID != ShortIDPrefix+own[:n] {
			return fmt.Errorf("event %s: short_id %q is not %s and the first 4 to 16 characters of %s", id, e.ShortID, ShortIDPrefix, own)
		}
	case Submitted, Approved, ChangesRequested:
		if !objectIDPattern.MatchString(e.PlanBlob) {
			return fmt.Errorf("event %s: plan_blob %q is not a git object ID", id, e.PlanBlob)
		}
	case Withdrawn, Closed:
	case Claimed, Checked, Done:
		return fmt.Errorf("event %s: type %q is reserved for a later format version", id, e.Type)
	default:
		return fmt.Errorf("event %s: unknown type %q", id, e.Type)
	}
	if e.Message != "" || e.Type == ChangesRequested {
		if _, err := ulid.ParseStrict(e.Message); err != nil {
			return fmt.Errorf("event %s: message %q is not the ULID of a Message", id, e.Message)
		}
	}
	return nil
}
