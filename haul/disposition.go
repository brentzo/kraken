package haul

// ExitKind is how the tentacle process ended. The head observes this; the
// tentacle cannot report it. It exists only to separate a stub left by a
// signal-killed process from one left by a process that exited cleanly and
// simply never wrote. KRK-002-R41.
type ExitKind int

const (
	// ExitUnknown: the head could not determine how the process ended.
	ExitUnknown ExitKind = iota
	// ExitClean: the process exited of its own accord.
	ExitClean
	// ExitSignal: the process was killed by a signal, so a Stop hook almost
	// certainly never ran and the absence of a haul is explained.
	ExitSignal
)

// Evidence is what the head recomputed, independent of anything claimed.
// A zero Evidence with Recomputed false means verification did not run.
type Evidence struct {
	Recomputed bool
	// ChangedFiles is the COMMITTED diff against the base commit. It is the
	// only thing the beak can integrate, because the beak merges branches.
	ChangedFiles []string
	// Uncommitted is work present in the worktree but not on the branch.
	// It is evidence that work happened and evidence that it is not
	// integrable, which are two different facts and both matter.
	Uncommitted      []string
	ChecksPassed     bool
	ChecksRun        bool
	AgentAttribution []string
}

// Classify derives the head's disposition. This is the function that makes
// "did the wrong thing" detectable, and it is deliberately the only place the
// four design outcomes are assigned. KRK-002-R18.
//
// The sharp case: completed with an empty recomputed diff is divergent, not
// no_change. An agent that exits 0 having done nothing while claiming it did
// the work is the exact failure this contract exists to catch, and one git
// diff catches it. KRK-002-R19.
func Classify(r Result, ev Evidence, exit ExitKind) (Disposition, VerifyStatus, ReasonCode) {
	// Anything the head could not read is indeterminate, and indeterminate is
	// never mapped onto failed. "Cannot read the result" is a different fact
	// from "the work failed". KRK-002-R20.
	switch r.State {
	case StateAbsent:
		if exit == ExitSignal {
			return DispIndeterminate, StatusUnverifiable, ReasonProcessDied
		}
		return DispIndeterminate, StatusUnverifiable, ReasonNoHaulWritten
	case StateStub:
		if exit == ExitSignal {
			return DispIndeterminate, StatusUnverifiable, ReasonProcessDied
		}
		return DispIndeterminate, StatusUnverifiable, ReasonNoHaulWritten
	case StateInvalid:
		return DispIndeterminate, StatusUnverifiable, ReasonHaulInvalid
	case StateUnreadable:
		return DispIndeterminate, StatusUnverifiable, ReasonSchemaMajorUnknown
	}

	claimed := r.Haul.Claims.Outcome

	// Agent attribution is a hard bar, checked as evidence and never as a
	// claim. KRK-002-R53.
	if len(ev.AgentAttribution) > 0 {
		return DispDivergent, StatusContradicted, ReasonPolicy
	}

	// Refused and blocked are terminal states, not errors, and they are not
	// gated on evidence because nothing was supposed to change. KRK-002-R12.
	if claimed == OutcomeRefused || claimed == OutcomeBlocked {
		return DispNotProceeded, StatusVerified, r.Haul.Claims.OutcomeDetail.ReasonCode
	}

	// Without recomputed evidence the head has a claim and nothing else,
	// which is explicitly not enough to admit a branch. KRK-002-R03.
	if !ev.Recomputed {
		return DispIndeterminate, StatusNotAttempted, ReasonHaulInvalid
	}

	empty := len(ev.ChangedFiles) == 0

	switch claimed {
	case OutcomeCompleted, OutcomePartial:
		if empty && len(ev.Uncommitted) > 0 && claimed == OutcomePartial {
			// Work exists in the worktree but never reached the branch. A
			// partial claim describing that is honest, not a contradiction,
			// and it is still not integrable. Found by a real dive whose
			// tentacle was denied `git add`.
			return DispNotProceeded, StatusVerified, ReasonPermissionRequired
		}
		if empty {
			// The whole reason the haul has two halves. A completed claim is
			// contradicted even when the worktree is dirty: uncommitted work
			// is not the work being claimed.
			return DispDivergent, StatusContradicted, ReasonCheckFailed
		}
		if ev.ChecksRun && !ev.ChecksPassed {
			return DispDivergent, StatusContradicted, ReasonCheckFailed
		}
		return DispWorkDone, StatusVerified, ""
	case OutcomeNoOp:
		if !empty {
			// Claimed to change nothing and changed something.
			return DispDivergent, StatusContradicted, ReasonCheckFailed
		}
		return DispNoChange, StatusVerified, ""
	case OutcomeFailed:
		if !empty {
			// A failed dive that left changes behind is still not admissible,
			// but it is honest rather than contradicted.
			return DispNotProceeded, StatusVerified, r.Haul.Claims.OutcomeDetail.ReasonCode
		}
		return DispNotProceeded, StatusVerified, r.Haul.Claims.OutcomeDetail.ReasonCode
	}
	return DispIndeterminate, StatusUnverifiable, ReasonHaulInvalid
}
