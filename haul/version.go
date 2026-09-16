package haul

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a pinned SemVer literal. Never a range, never a constraint
// expression. KRK-002-R22.
type Version struct {
	Major, Minor, Patch int
}

// Current is the schema version this build writes and validates against.
var Current = mustParseVersion(SchemaVersion)

// ParseVersion parses a strict three-part SemVer literal. Anything else, a
// range or a constraint included, is an error rather than a best guess.
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("schema_version %q is not a pinned MAJOR.MINOR.PATCH literal", s)
	}
	var v Version
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return Version{}, fmt.Errorf("schema_version %q has a malformed component %q", s, p)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("schema_version %q has a non-numeric component %q", s, p)
		}
		switch i {
		case 0:
			v.Major = n
		case 1:
			v.Minor = n
		case 2:
			v.Patch = n
		}
	}
	return v, nil
}

func mustParseVersion(s string) Version {
	v, err := ParseVersion(s)
	if err != nil {
		panic("haul: " + err.Error())
	}
	return v
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compatibility is what this build can do with a document's declared version.
type Compatibility int

const (
	// CompatSame: the document is at or below this build's MINOR. Unknown
	// keys are a real error here, because there is no forward-compatible
	// reason for one to exist. KRK-002-R25.
	CompatSame Compatibility = iota
	// CompatAhead: a known MAJOR with a MINOR or PATCH ahead of ours. Parse
	// the fields we know, proceed, and log the drift. KRK-002-R24.
	CompatAhead
	// CompatUnknownMajor: refuse to interpret, record indeterminate with
	// schema_major_unknown, and preserve the bytes. KRK-002-R23.
	CompatUnknownMajor
)

// Compatible classifies a document version against this build.
//
// This is where KRK-002-R24 and KRK-002-R25 are reconciled. R25 requires
// strict rejection of unknown keys; R24 requires a newer MINOR to parse and
// proceed. Taken literally they contradict, because a 1.1.0 document read by a
// 1.0.0 build has unknown keys by definition.
//
// The resolution: strict at or below our own MINOR, tolerant above it. That
// preserves both intents. A typo or a foreign document at the same version is
// caught loudly, and a forward-compatible addition from a newer producer is
// tolerated, which is exactly what R27's "every added field must be optional"
// is for.
func Compatible(v Version) Compatibility {
	switch {
	case v.Major != Current.Major:
		return CompatUnknownMajor
	case v.Minor > Current.Minor:
		return CompatAhead
	case v.Minor == Current.Minor && v.Patch > Current.Patch:
		return CompatAhead
	default:
		return CompatSame
	}
}
