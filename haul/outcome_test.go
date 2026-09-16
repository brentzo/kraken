package haul

import "testing"

func TestOutcome_Vocabularies(t *testing.T) {
	// Two vocabularies, deliberately not the same enum. A tentacle may write
	// six values; the head owns two more it can never write. KRK-002-R11.
	for _, o := range []Outcome{OutcomeCompleted, OutcomePartial, OutcomeNoOp,
		OutcomeRefused, OutcomeBlocked, OutcomeFailed} {
		if !o.Valid() || !o.WritableByTentacle() {
			t.Errorf("%q must be writable by a tentacle", o)
		}
	}
	for _, o := range []Outcome{OutcomeCrashed, OutcomeUnknown} {
		if !o.Valid() {
			t.Errorf("%q must be a known outcome", o)
		}
		if o.WritableByTentacle() {
			t.Errorf("%q is the head's to write, never a tentacle's", o)
		}
	}
	if Outcome("made_up").Valid() {
		t.Error("the enum must be closed")
	}
}

func TestOutcome_RequiresNotDone(t *testing.T) {
	// An outcome that admits work was left undone must say which work.
	// KRK-002-R17.
	for _, o := range []Outcome{OutcomePartial, OutcomeRefused, OutcomeBlocked} {
		if !o.RequiresNotDone() {
			t.Errorf("%q must require not_done", o)
		}
	}
	for _, o := range []Outcome{OutcomeCompleted, OutcomeNoOp, OutcomeFailed} {
		if o.RequiresNotDone() {
			t.Errorf("%q must not require not_done", o)
		}
	}
}

func TestReasonCode_ScopedToOutcome(t *testing.T) {
	// A reason code valid under one outcome is an error under another, which
	// is what keeps the pair meaningful rather than decorative. KRK-002-R15.
	cases := []struct {
		reason  ReasonCode
		outcome Outcome
		want    bool
	}{
		{ReasonUnsafe, OutcomeRefused, true},
		{ReasonUnsafe, OutcomeNoOp, false},
		{ReasonAlreadySatisfied, OutcomeNoOp, true},
		{ReasonAlreadySatisfied, OutcomeFailed, false},
		{ReasonCredentialMissing, OutcomeBlocked, true},
		{ReasonCredentialMissing, OutcomeRefused, false},
		{ReasonProcessDied, OutcomeCrashed, true},
		{ReasonProcessDied, OutcomeFailed, false},
		// turn_limit is deliberately shared: running out of turns can leave
		// real work behind (partial) or stop it starting (blocked).
		{ReasonTurnLimit, OutcomePartial, true},
		{ReasonTurnLimit, OutcomeBlocked, true},
		{ReasonTurnLimit, OutcomeNoOp, false},
	}
	for _, c := range cases {
		if got := c.reason.PermittedFor(c.outcome); got != c.want {
			t.Errorf("%q under %q = %v, want %v", c.reason, c.outcome, got, c.want)
		}
	}
}

func TestState_OnlyValidIsAdmissible(t *testing.T) {
	// Nothing but a valid haul may enter the beak. KRK-002-R43.
	if !StateValid.Admissible() {
		t.Error("valid must be admissible")
	}
	for _, s := range []State{StateAbsent, StateStub, StateInvalid, StateUnreadable} {
		if s.Admissible() {
			t.Errorf("%q must never be admissible", s)
		}
	}
}

// A permission wall can leave work behind, so partial must be able to say so.
// The table originally forbade this and a real dive caught it.
func TestReasonCode_PartialAcceptsBlockedReasons(t *testing.T) {
	for _, r := range []ReasonCode{
		ReasonPermissionRequired, ReasonCredentialMissing, ReasonNetworkDenied,
		ReasonToolUnavailable, ReasonHumanInputRequired,
	} {
		if !r.PermittedFor(OutcomePartial) {
			t.Errorf("%q must be permittable under partial: a tentacle can do half the work and then hit a wall", r)
		}
		if !r.PermittedFor(OutcomeBlocked) {
			t.Errorf("%q must remain valid under blocked", r)
		}
	}
	// The distinction still has to mean something.
	if ReasonAlreadySatisfied.PermittedFor(OutcomePartial) {
		t.Error("no_op reasons must not leak into partial")
	}
}
