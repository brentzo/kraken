package haul

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// Result is everything one read produced. It carries the state alongside the
// document so that "no haul" cannot quietly become "failed" three call sites
// later.
type Result struct {
	Haul   *Haul
	State  State
	Digest string     // sha256 of the exact bytes read. KRK-002-R08.
	Bytes  []byte     // preserved for the rejected copy. KRK-002-R42.
	Reason ReasonCode // set when State is not valid
	Err    error      // why it is not valid, for the verification record
	// Drift is set when the document declared a newer MINOR or PATCH than
	// this build. The read still succeeded. KRK-002-R24.
	Drift string
}

// Read loads the haul at path.
//
// It deliberately returns a Result rather than (*Haul, error). Folding absent
// into an error return is how "no haul" quietly becomes "failed", which is the
// single failure this contract exists to prevent. The error field inside the
// Result describes why a document is unusable; it is never the channel for
// "the work failed". KRK-002-R31.
func Read(path string) Result {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Result{State: StateAbsent, Reason: ReasonNoHaulWritten, Err: err}
		}
		return Result{State: StateInvalid, Reason: ReasonHaulInvalid, Err: err}
	}
	return Parse(b)
}

// Parse classifies and decodes haul bytes. It is the whole reading contract,
// separated from the filesystem so it is testable directly.
func Parse(b []byte) Result {
	sum := sha256.Sum256(b)
	res := Result{Bytes: b, Digest: "sha256:" + hex.EncodeToString(sum[:])}

	// Size cap before anything else, so a hostile or runaway document cannot
	// be parsed at all. KRK-002-R35.
	if len(b) > MaxBytes {
		res.State, res.Reason = StateInvalid, ReasonHaulInvalid
		res.Err = fmt.Errorf("haul is %d bytes, over the %d byte cap; logs and diffs are referenced by path, never inlined", len(b), MaxBytes)
		return res
	}

	// The first key must be the magic literal. This is what distinguishes a
	// haul from a half-written file or another tool's output at the same
	// path, and it is checked on the token stream because key order is not
	// preserved by a map decode. KRK-002-R21.
	if err := checkMagicFirstKey(b); err != nil {
		res.State, res.Reason, res.Err = StateInvalid, ReasonHaulInvalid, err
		return res
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		res.State, res.Reason, res.Err = StateInvalid, ReasonHaulInvalid, err
		return res
	}

	// Version gate before field decoding, so an unreadable MAJOR is never
	// partially interpreted. KRK-002-R23.
	var rawVer string
	if err := json.Unmarshal(top["schema_version"], &rawVer); err != nil {
		res.State, res.Reason = StateInvalid, ReasonHaulInvalid
		res.Err = errors.New("schema_version is missing or not a string")
		return res
	}
	v, err := ParseVersion(rawVer)
	if err != nil {
		res.State, res.Reason, res.Err = StateInvalid, ReasonHaulInvalid, err
		return res
	}
	compat := Compatible(v)
	if compat == CompatUnknownMajor {
		res.State, res.Reason = StateUnreadable, ReasonSchemaMajorUnknown
		res.Err = fmt.Errorf("schema_version %s has MAJOR %d, this build reads %d.x; bytes preserved unread", v, v.Major, Current.Major)
		return res
	}
	if compat == CompatAhead {
		res.Drift = fmt.Sprintf("document declares %s, this build is %s; parsing known fields only", v, Current)
	}

	// Unknown keys are an error only at or below our own MINOR. Above it
	// they are the forward-compatible additions R27 guarantees are optional.
	// See Compatible for why R24 and R25 are reconciled this way.
	if compat == CompatSame {
		if err := rejectUnknownKeys(top); err != nil {
			res.State, res.Reason, res.Err = StateInvalid, ReasonHaulInvalid, err
			return res
		}
	}

	var h Haul
	if err := json.Unmarshal(b, &h); err != nil {
		res.State, res.Reason, res.Err = StateInvalid, ReasonHaulInvalid, err
		return res
	}
	if err := h.Validate(); err != nil {
		res.State, res.Reason, res.Err = StateInvalid, ReasonHaulInvalid, err
		return res
	}

	res.Haul = &h

	// A valid document still carrying the head's pre-spawn stub value means
	// the tentacle never wrote. That is a distinct state from a tentacle that
	// wrote a real result. KRK-002-R30, R40.
	if h.Claims.Outcome == OutcomeCrashed {
		res.State, res.Reason = StateStub, ReasonProcessDied
		return res
	}
	res.State = StateValid
	return res
}

// checkMagicFirstKey walks the token stream far enough to prove the document
// is an object whose first key is the magic key carrying the magic value.
func checkMagicFirstKey(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("not valid JSON: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("a haul must be a single JSON object")
	}
	tok, err = dec.Token()
	if err != nil {
		if err == io.EOF {
			return errors.New("a haul must not be empty")
		}
		return fmt.Errorf("truncated before the first key: %w", err)
	}
	key, ok := tok.(string)
	if !ok || key != MagicKey {
		return fmt.Errorf("first key must be %q, got %v; this file is not a haul", MagicKey, tok)
	}
	tok, err = dec.Token()
	if err != nil {
		return fmt.Errorf("truncated after the first key: %w", err)
	}
	if s, ok := tok.(string); !ok || s != Magic {
		return fmt.Errorf("%s must be %q, got %v", MagicKey, Magic, tok)
	}
	return nil
}

// topLevelKeys and claimsKeys are the closed sets R25 enforces.
var topLevelKeys = map[string]bool{
	"kraken_haul": true, "schema_version": true, "assignment": true,
	"agent": true, "claims": true, "verification": true,
}

var claimsKeys = map[string]bool{
	"outcome": true, "summary": true, "outcome_detail": true, "changes": true,
	"checks": true, "not_done": true, "artifacts": true, "ext": true,
}

// rejectUnknownKeys enforces strict rejection at the top level and inside
// claims. Permissive parsing is exactly how a newer tentacle misreports to an
// older head: the head drops the new field that says "I only did half the job"
// and reads the rest as success. KRK-002-R25.
func rejectUnknownKeys(top map[string]json.RawMessage) error {
	var bad []string
	for k := range top {
		if !topLevelKeys[k] {
			bad = append(bad, k)
		}
	}
	if raw, ok := top["claims"]; ok {
		var claims map[string]json.RawMessage
		if err := json.Unmarshal(raw, &claims); err == nil {
			for k := range claims {
				if !claimsKeys[k] {
					bad = append(bad, "claims."+k)
				}
			}
		}
	}
	if len(bad) > 0 {
		sortStrings(bad)
		return fmt.Errorf("unknown key(s) %v at this schema version; an unknown key is rejected rather than ignored, because silently dropping one is how a newer producer misreports to an older reader", bad)
	}
	return nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
