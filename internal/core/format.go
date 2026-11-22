package core

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatVersion is the format version of the plans ref this binary reads
// and writes (ADR 0002 section 2).
const FormatVersion = 2

// MigratesFrom is the only older format version bfc migrate upgrades to
// FormatVersion (ADR 0013 section 1).
const MigratesFrom = 1

// ParseFormatVersion reads the content of the format-version file.
func ParseFormatVersion(data []byte) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("format-version %q is not a number", strings.TrimSpace(string(data)))
	}
	return v, nil
}
