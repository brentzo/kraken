package haul

import "testing"

func TestParseVersion(t *testing.T) {
	// A pinned literal only. A range or a constraint expression is an error
	// rather than a best guess, because there is nobody left to negotiate
	// with by the time the head reads the file. KRK-002-R22.
	good := map[string]Version{
		"1.0.0":    {1, 0, 0},
		"1.9.3":    {1, 9, 3},
		"10.20.30": {10, 20, 30},
		"0.1.0":    {0, 1, 0},
	}
	for s, want := range good {
		got, err := ParseVersion(s)
		if err != nil || got != want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", s, got, err, want)
		}
	}
	for _, bad := range []string{"", "1", "1.0", "1.0.0.0", "^1.0.0", ">=1.0.0",
		"1.x", "v1.0.0", "1.0.0-rc1", "01.0.0", "1.-1.0"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) must fail, it is not a pinned literal", bad)
		}
	}
}

func TestCompatible(t *testing.T) {
	// This is where KRK-002-R24 and R25 are reconciled. Taken literally they
	// contradict: R25 rejects unknown keys, R24 requires a newer MINOR to
	// parse and proceed, and a newer MINOR has unknown keys by definition.
	// Strict at or below our own MINOR, tolerant above it.
	cases := []struct {
		ver  string
		want Compatibility
	}{
		{"1.0.0", CompatSame},
		{"0.9.0", CompatUnknownMajor}, // a different MAJOR in either direction
		{"1.0.1", CompatAhead},
		{"1.1.0", CompatAhead},
		{"1.9.9", CompatAhead},
		{"2.0.0", CompatUnknownMajor},
		{"99.0.0", CompatUnknownMajor},
	}
	for _, c := range cases {
		v, err := ParseVersion(c.ver)
		if err != nil {
			t.Fatalf("fixture %q: %v", c.ver, err)
		}
		if got := Compatible(v); got != c.want {
			t.Errorf("Compatible(%s) = %v, want %v", c.ver, got, c.want)
		}
	}
}

func TestCurrentIsPinned(t *testing.T) {
	if Current.String() != SchemaVersion {
		t.Errorf("Current = %s, SchemaVersion = %s; they must not drift", Current, SchemaVersion)
	}
}
