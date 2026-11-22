package core

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/oklog/ulid/v2"
	"golang.org/x/text/unicode/norm"
)

// CurrentSource says where the Current plan came from (ADR 0009).
type CurrentSource int

const (
	// NoCurrent means there is no Current plan.
	NoCurrent CurrentSource = iota
	// FromBranch means the current branch name carries its Short ID.
	FromBranch
	// FromUse means it was set with bfc use.
	FromUse
)

// Current is the Current plan and where it came from. Source is NoCurrent
// when there is none.
type Current struct {
	ID     ulid.ULID
	Source CurrentSource
}

// BranchPlanError is a Short ID in the branch name that names no plan, or
// several. It wraps ErrUnknownID or an *AmbiguousIDError.
type BranchPlanError struct {
	Branch  string
	ShortID string
	Err     error
}

func (e *BranchPlanError) Error() string {
	return fmt.Sprintf("branch %s names %s: %v", e.Branch, e.ShortID, e.Err)
}

func (e *BranchPlanError) Unwrap() error { return e.Err }

// branchShortID finds a Short ID as a whole word: "bfc-" and 4 to 16
// letters or digits, with no letter or digit right before or after. A
// shorter word such as bfc-v2 is no Short ID.
var branchShortID = regexp.MustCompile(`(?i)(?:^|[^0-9a-z])(bfc-[0-9a-z]{4,16})(?:$|[^0-9a-z])`)

// ShortIDInBranch returns the first Short ID in a branch name, as written.
func ShortIDInBranch(branch string) (string, bool) {
	m := branchShortID.FindStringSubmatch(branch)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// CurrentPlan returns the Current plan (ADR 0009). branch is the current
// branch name, empty for a detached HEAD; used is the plan set with bfc use,
// the zero ULID when none is. The first Short ID in the branch name wins and
// is resolved like any plan ID, so a longer prefix settles an ambiguous one.
// A Short ID that names no plan, or several, is a *BranchPlanError, never a
// silent fall back to used. A used plan that does not exist is ErrUnknownID.
func CurrentPlan(branch string, used ulid.ULID, plans []PlanHandle) (Current, error) {
	if short, ok := ShortIDInBranch(branch); ok {
		id, err := ResolveID(short, plans)
		if err != nil {
			return Current{}, &BranchPlanError{Branch: branch, ShortID: short, Err: err}
		}
		return Current{ID: id, Source: FromBranch}, nil
	}
	if used == (ulid.ULID{}) {
		return Current{}, nil
	}
	for _, p := range plans {
		if p.ID == used {
			return Current{ID: used, Source: FromUse}, nil
		}
	}
	return Current{}, fmt.Errorf("%w: %s, set with bfc use", ErrUnknownID, used)
}

// planLine is a line "Plan: bfc-…" in the description of a pull request.
var planLine = regexp.MustCompile(`(?im)^\s*plan:\s*(bfc-[0-9a-z]{4,16})\s*$`)

// PullRequestPlan returns the plan a pull request belongs to (ADR 0014):
// the Short ID in its branch name, resolved like the Current plan's, else
// the one in a "Plan: bfc-…" line of its description. found is false when
// neither names a plan; a Short ID that names no plan, or several, is an
// error.
func PullRequestPlan(branch, body string, plans []PlanHandle) (id ulid.ULID, found bool, err error) {
	if short, ok := ShortIDInBranch(branch); ok {
		id, err := ResolveID(short, plans)
		if err != nil {
			return ulid.ULID{}, false, &BranchPlanError{Branch: branch, ShortID: short, Err: err}
		}
		return id, true, nil
	}
	m := planLine.FindStringSubmatch(strings.ReplaceAll(body, "\r\n", "\n"))
	if m == nil {
		return ulid.ULID{}, false, nil
	}
	id, err = ResolveID(m[1], plans)
	if err != nil {
		return ulid.ULID{}, false, fmt.Errorf("the description says Plan: %s: %w", m[1], err)
	}
	return id, true, nil
}

// maxSlugLen bounds the slug of the title in a branch name.
const maxSlugLen = 48

// BranchName returns the branch bfc start makes for a plan:
// "<Short ID>-<slug of the title>", for example
// bfc-a1b2-payments-via-stripe. The slug has lower-case ASCII letters and
// digits only, diacritics removed, words joined by "-", cut at a word
// boundary; a title with nothing left gives the Short ID alone.
func BranchName(shortID, title string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(strings.ToLower(title)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	slug := b.String()
	if len(slug) > maxSlugLen {
		slug = slug[:maxSlugLen]
		if i := strings.LastIndexByte(slug, '-'); i > 0 {
			slug = slug[:i]
		}
	}
	if slug == "" {
		return shortID
	}
	return shortID + "-" + slug
}
