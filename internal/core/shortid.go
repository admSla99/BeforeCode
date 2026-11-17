package core

import (
	"strings"

	"github.com/oklog/ulid/v2"
)

// ShortIDPrefix starts every Short ID.
const ShortIDPrefix = "bfc-"

// Short ID length limits from ADR 0002 section 8.
const (
	minShortIDLen = 4
	maxShortIDLen = 16
)

// ShortID returns the Short ID for a new plan with the given ULID: "bfc-"
// and the first n characters of the ULID's random part in lower case. n
// starts at 4 and grows while a plan in existing shares the first n
// characters, up to 16 (ADR 0002 section 8).
func ShortID(id ulid.ULID, existing []ulid.ULID) string {
	own := randomPart(id)
	n := minShortIDLen
	for ; n < maxShortIDLen; n++ {
		clash := false
		for _, other := range existing {
			if other != id && strings.HasPrefix(randomPart(other), own[:n]) {
				clash = true
				break
			}
		}
		if !clash {
			break
		}
	}
	return ShortIDPrefix + own[:n]
}

// randomPart returns characters 11 to 26 of the ULID in lower case.
func randomPart(id ulid.ULID) string {
	return strings.ToLower(id.String()[10:])
}
