package haul

// Outcome is the tentacle's vocabulary for what happened. It is a routing
// hint, never a gate input.
//
// Only three of the design's four outcomes can be self-reported. An agent that
// knew it did the wrong thing would have said partial or failed. "Did the wrong
// thing" exists only as a delta between claims and verification, which is
// precisely why the haul has two halves.
type Outcome string

const (
	// OutcomeCompleted: believes it did the whole assignment.
	OutcomeCompleted Outcome = "completed"
	// OutcomePartial: did some of it and knows which part it did not do.
	OutcomePartial Outcome = "partial"
	// OutcomeNoOp: ran to completion and deliberately changed nothing.
	OutcomeNoOp Outcome = "no_op"
	// OutcomeRefused: declined on its own judgement. A terminal
	// success-shaped state, never an error. KRK-002-R12.
	OutcomeRefused Outcome = "refused"
	// OutcomeBlocked: wanted to continue and could not. Distinct from
	// refused, and retryable once the blocking resource is supplied.
	// KRK-002-R13.
	OutcomeBlocked Outcome = "blocked"
	// OutcomeFailed: tried, and the work did not succeed.
	OutcomeFailed Outcome = "failed"

	// OutcomeCrashed is written by the head as the pre-spawn stub value.
	// A tentacle may never write it. KRK-002-R11.
	OutcomeCrashed Outcome = "crashed"
	// OutcomeUnknown is written by the head after reading the envelope.
	// A tentacle may never write it. KRK-002-R11.
	OutcomeUnknown Outcome = "unknown"
)

// tentacleOutcomes is the closed set a tentacle may write. KRK-002-R11.
var tentacleOutcomes = map[Outcome]bool{
	OutcomeCompleted: true,
	OutcomePartial:   true,
	OutcomeNoOp:      true,
	OutcomeRefused:   true,
	OutcomeBlocked:   true,
	OutcomeFailed:    true,
}

// headOutcomes is the closed set only the head may write.
var headOutcomes = map[Outcome]bool{
	OutcomeCrashed: true,
	OutcomeUnknown: true,
}

// Valid reports whether o is any known outcome, from either vocabulary.
func (o Outcome) Valid() bool { return tentacleOutcomes[o] || headOutcomes[o] }

// WritableByTentacle reports whether a tentacle is permitted to write o.
// KRK-002-R11.
func (o Outcome) WritableByTentacle() bool { return tentacleOutcomes[o] }

// RequiresNotDone reports whether claims.not_done must be non-empty for this
// outcome. KRK-002-R17.
func (o Outcome) RequiresNotDone() bool {
	switch o {
	case OutcomePartial, OutcomeRefused, OutcomeBlocked:
		return true
	}
	return false
}

// State is what the head found on disk. Absence is a defined state, not an
// unhandled case. KRK-002-R40.
type State string

const (
	// StateAbsent: the file is missing entirely.
	StateAbsent State = "absent"
	// StateStub: the pre-spawn stub was never overwritten.
	StateStub State = "stub"
	// StateInvalid: not valid JSON, truncated, wrong magic, over the size
	// cap, or failed schema validation.
	StateInvalid State = "invalid"
	// StateUnreadable: valid JSON carrying a MAJOR this build cannot read.
	StateUnreadable State = "unreadable"
	// StateValid: valid and validated.
	StateValid State = "valid"
)

// Admissible reports whether a haul in this state may enter the beak.
// Only valid may. KRK-002-R43.
func (s State) Admissible() bool { return s == StateValid }

// Disposition is the head's vocabulary. This is where the design's four
// outcomes actually live, plus the honest fifth.
type Disposition string

const (
	// DispWorkDone: did the work.
	DispWorkDone Disposition = "work_done"
	// DispNoChange: did nothing, and said so.
	DispNoChange Disposition = "no_change"
	// DispDivergent: did the wrong thing. Claims and evidence disagree.
	DispDivergent Disposition = "divergent"
	// DispNotProceeded: refused or blocked.
	DispNotProceeded Disposition = "not_proceeded"
	// DispIndeterminate: cannot tell. Never mapped onto failed, because
	// "cannot read the result" is a different fact from "the work failed".
	// KRK-002-R20.
	DispIndeterminate Disposition = "indeterminate"
)
