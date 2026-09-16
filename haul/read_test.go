package haul

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_States(t *testing.T) {
	valid, err := json.Marshal(base())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		body   string
		want   State
		reason ReasonCode
	}{
		{"valid", string(valid), StateValid, ""},
		{"not json", `{not json`, StateInvalid, ReasonHaulInvalid},
		{"truncated mid object", string(valid[:len(valid)/2]), StateInvalid, ReasonHaulInvalid},
		{"empty object", `{}`, StateInvalid, ReasonHaulInvalid},
		{"json array", `[]`, StateInvalid, ReasonHaulInvalid},
		{
			"valid json, wrong first key: another tool's output",
			`{"version":"1.0.0","result":"ok"}`,
			StateInvalid, ReasonHaulInvalid,
		},
		{
			"right keys, wrong magic value",
			`{"kraken_haul":"https://example.com/other","schema_version":"1.0.0"}`,
			StateInvalid, ReasonHaulInvalid,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := Parse([]byte(tc.body))
			if r.State != tc.want {
				t.Errorf("state = %q, want %q (err: %v)", r.State, tc.want, r.Err)
			}
			if tc.reason != "" && r.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", r.Reason, tc.reason)
			}
		})
	}
}

func TestParse_MagicMustBeFirstKey(t *testing.T) {
	// Valid JSON with the magic present but not first. Key order is not
	// preserved by a map decode, so this is checked on the token stream.
	body := `{"schema_version":"1.0.0","kraken_haul":"` + Magic + `"}`
	r := Parse([]byte(body))
	if r.State != StateInvalid {
		t.Errorf("state = %q, want invalid: the magic must be the FIRST key", r.State)
	}
}

func TestParse_Versioning(t *testing.T) {
	body := func(ver string) []byte {
		h := base()
		h.SchemaVersion = ver
		b, _ := json.Marshal(h)
		return b
	}

	t.Run("unknown major is unreadable, never invalid", func(t *testing.T) {
		r := Parse(body("2.0.0"))
		if r.State != StateUnreadable {
			t.Errorf("state = %q, want unreadable", r.State)
		}
		if r.Reason != ReasonSchemaMajorUnknown {
			t.Errorf("reason = %q, want %q", r.Reason, ReasonSchemaMajorUnknown)
		}
		if len(r.Bytes) == 0 {
			t.Error("bytes must be preserved for an unreadable major")
		}
	})

	t.Run("newer minor parses and reports drift", func(t *testing.T) {
		r := Parse(body("1.9.0"))
		if r.State != StateValid {
			t.Fatalf("state = %q, want valid (err: %v)", r.State, r.Err)
		}
		if r.Drift == "" {
			t.Error("a newer minor must report drift")
		}
	})

	t.Run("range or constraint is rejected", func(t *testing.T) {
		for _, bad := range []string{"^1.0.0", "1.0", "1.x", ">=1.0.0", ""} {
			if r := Parse(body(bad)); r.State != StateInvalid {
				t.Errorf("schema_version %q: state = %q, want invalid", bad, r.State)
			}
		}
	})
}

func TestParse_UnknownKeys(t *testing.T) {
	// At our own version an unknown key is a real error, because silently
	// dropping one is how a newer producer misreports to an older reader.
	t.Run("rejected at the same minor", func(t *testing.T) {
		var m map[string]any
		b, _ := json.Marshal(base())
		_ = json.Unmarshal(b, &m)
		m["only_did_half"] = true
		b2, _ := json.Marshal(m)
		// re-emit with the magic first
		body := `{"kraken_haul":"` + Magic + `",` + strings.TrimPrefix(string(b2), "{")
		r := Parse([]byte(body))
		if r.State != StateInvalid {
			t.Errorf("state = %q, want invalid; err: %v", r.State, r.Err)
		}
	})

	// Above our minor the same key is a forward-compatible addition, which
	// R27 guarantees is optional. This is the R24/R25 reconciliation.
	t.Run("tolerated above our minor", func(t *testing.T) {
		h := base()
		h.SchemaVersion = "1.9.0"
		b, _ := json.Marshal(h)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		delete(m, "kraken_haul")
		m["future_field"] = true
		rest, _ := json.Marshal(m)
		body := `{"kraken_haul":"` + Magic + `",` + strings.TrimPrefix(string(rest), "{")
		r := Parse([]byte(body))
		if r.State != StateValid {
			t.Errorf("state = %q, want valid; a newer minor may add optional fields. err: %v", r.State, r.Err)
		}
	})
}

func TestParse_SizeCap(t *testing.T) {
	h := base()
	h.Claims.Summary = strings.Repeat("x", MaxBytes)
	b, _ := json.Marshal(h)
	r := Parse(b)
	if r.State != StateInvalid || !strings.Contains(r.Err.Error(), "cap") {
		t.Errorf("state = %q err = %v, want invalid over the size cap", r.State, r.Err)
	}
}

// --- on-disk protocol -------------------------------------------------------

func TestRead_AbsentIsAStateNotAnError(t *testing.T) {
	r := Read(filepath.Join(t.TempDir(), "haul.json"))
	if r.State != StateAbsent {
		t.Errorf("state = %q, want absent", r.State)
	}
	if r.Reason != ReasonNoHaulWritten {
		t.Errorf("reason = %q, want %q", r.Reason, ReasonNoHaulWritten)
	}
	if r.Haul != nil {
		t.Error("no document should be returned for an absent haul")
	}
}
