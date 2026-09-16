package git

import "testing"

// The name-status stream is the only hand-written parser in this package, and
// it is where path handling quietly breaks on the one repo in a hundred that
// has a space or a quote in a filename. It is pure, so it is tested directly
// rather than only through a real repository.
func TestParseNameStatusZ(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Change
	}{
		{"empty", "", nil},
		{
			"single add",
			"A\x00src/a.go\x00",
			[]Change{{Status: 'A', Path: "src/a.go"}},
		},
		{
			"several entries",
			"A\x00a.go\x00M\x00b.go\x00D\x00c.go\x00",
			[]Change{
				{Status: 'A', Path: "a.go"},
				{Status: 'M', Path: "b.go"},
				{Status: 'D', Path: "c.go"},
			},
		},
		{
			"rename carries three fields and a score",
			"R100\x00old.txt\x00new.txt\x00",
			[]Change{{Status: 'R', Path: "new.txt", From: "old.txt", Score: 100}},
		},
		{
			"copy behaves like a rename",
			"C75\x00src.txt\x00dst.txt\x00",
			[]Change{{Status: 'C', Path: "dst.txt", From: "src.txt", Score: 75}},
		},
		{
			"rename beside ordinary entries",
			"M\x00a.go\x00R90\x00x.go\x00y.go\x00A\x00z.go\x00",
			[]Change{
				{Status: 'M', Path: "a.go"},
				{Status: 'R', Path: "y.go", From: "x.go", Score: 90},
				{Status: 'A', Path: "z.go"},
			},
		},
		{
			// The whole reason for -z. In the human-readable form git would
			// quote and escape these, and a parser would have to unescape.
			"paths with spaces and quotes survive verbatim",
			"A\x00a file with spaces.txt\x00M\x00quote\"inside.txt\x00",
			[]Change{
				{Status: 'A', Path: "a file with spaces.txt"},
				{Status: 'M', Path: `quote"inside.txt`},
			},
		},
		{
			"a newline in a path is not a record boundary",
			"A\x00weird\nname.txt\x00",
			[]Change{{Status: 'A', Path: "weird\nname.txt"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseNameStatusZ(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d changes, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] got %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseNameStatusZ_Truncated(t *testing.T) {
	// A truncated stream is an error, not a silently shorter change set.
	// Silently dropping the last entry is how a diff quietly under-reports.
	for _, in := range []string{
		"A\x00",               // status with no path
		"R100\x00old.txt\x00", // rename missing its destination
	} {
		if _, err := parseNameStatusZ(in); err == nil {
			t.Errorf("parseNameStatusZ(%q) must report truncation, got nil", in)
		}
	}
}

func TestCommitMessage(t *testing.T) {
	// The trailer scan reads subject and body together, so a trailer in the
	// body is visible to it.
	c := Commit{Subject: "feat: thing", Body: "Co-Authored-By: X <x@y.z>"}
	want := "feat: thing\n\nCo-Authored-By: X <x@y.z>"
	if c.Message() != want {
		t.Errorf("Message() = %q, want %q", c.Message(), want)
	}
	if (Commit{Subject: "only"}).Message() != "only" {
		t.Error("a bodyless commit must not gain trailing blank lines")
	}
}
