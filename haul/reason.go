package haul

// ReasonCode is a closed vocabulary scoped to the reported outcome.
// KRK-002-R15. The accompanying message is free text and is never parsed.
type ReasonCode string

// Reason codes, grouped by the outcome that permits them.
const (
	// partial
	ReasonTurnLimit            ReasonCode = "turn_limit"
	ReasonTokenBudget          ReasonCode = "token_budget"
	ReasonTimeLimit            ReasonCode = "time_limit"
	ReasonCostBudget           ReasonCode = "cost_budget"
	ReasonScopeTooLarge        ReasonCode = "scope_too_large"
	ReasonDependencyIncomplete ReasonCode = "dependency_incomplete"

	// no_op
	ReasonAlreadySatisfied ReasonCode = "already_satisfied"
	ReasonNotApplicable    ReasonCode = "not_applicable"
	ReasonNoChangeNeeded   ReasonCode = "no_change_needed"

	// refused
	ReasonUnsafe               ReasonCode = "unsafe"
	ReasonOutOfScope           ReasonCode = "out_of_scope"
	ReasonAmbiguousInstruction ReasonCode = "ambiguous_instruction"
	ReasonPolicy               ReasonCode = "policy"
	ReasonInsufficientContext  ReasonCode = "insufficient_context"

	// blocked
	ReasonPermissionRequired ReasonCode = "permission_required"
	ReasonCredentialMissing  ReasonCode = "credential_missing"
	ReasonNetworkDenied      ReasonCode = "network_denied"
	ReasonToolUnavailable    ReasonCode = "tool_unavailable"
	ReasonHumanInputRequired ReasonCode = "human_input_required"

	// failed
	ReasonCheckFailed  ReasonCode = "check_failed"
	ReasonBuildFailed  ReasonCode = "build_failed"
	ReasonConflict     ReasonCode = "conflict"
	ReasonToolError    ReasonCode = "tool_error"
	ReasonInternalErro ReasonCode = "internal_error"

	// crashed, head-written
	ReasonProcessDied ReasonCode = "process_died"

	// unknown, head-written
	ReasonNoHaulWritten      ReasonCode = "no_haul_written"
	ReasonHaulInvalid        ReasonCode = "haul_invalid"
	ReasonSchemaMajorUnknown ReasonCode = "schema_major_unknown"
)

// reasonsByOutcome is the scoping table. A reason code valid for one outcome
// is a validation error under another, which is what keeps the pair meaningful
// rather than decorative.
var reasonsByOutcome = map[Outcome]map[ReasonCode]bool{
	OutcomePartial: {
		ReasonTurnLimit: true, ReasonTokenBudget: true, ReasonTimeLimit: true,
		ReasonCostBudget: true, ReasonScopeTooLarge: true,
		ReasonDependencyIncomplete: true,
	},
	OutcomeNoOp: {
		ReasonAlreadySatisfied: true, ReasonNotApplicable: true,
		ReasonNoChangeNeeded: true,
	},
	OutcomeRefused: {
		ReasonUnsafe: true, ReasonOutOfScope: true,
		ReasonAmbiguousInstruction: true, ReasonPolicy: true,
		ReasonInsufficientContext: true,
	},
	OutcomeBlocked: {
		ReasonPermissionRequired: true, ReasonCredentialMissing: true,
		ReasonNetworkDenied: true, ReasonToolUnavailable: true,
		ReasonHumanInputRequired: true, ReasonTurnLimit: true,
		ReasonTokenBudget: true, ReasonTimeLimit: true, ReasonCostBudget: true,
	},
	OutcomeFailed: {
		ReasonCheckFailed: true, ReasonBuildFailed: true, ReasonConflict: true,
		ReasonToolError: true, ReasonInternalErro: true,
	},
	OutcomeCrashed: {
		ReasonProcessDied: true,
	},
	OutcomeUnknown: {
		ReasonNoHaulWritten: true, ReasonHaulInvalid: true,
		ReasonSchemaMajorUnknown: true,
	},
}

// PermittedFor reports whether r is in the closed set for outcome o.
// KRK-002-R15.
func (r ReasonCode) PermittedFor(o Outcome) bool {
	return reasonsByOutcome[o][r]
}

// ReasonsFor returns the permitted reason codes for o, for error messages and
// for the JSON Schema the tentacle is handed at spawn.
func ReasonsFor(o Outcome) []ReasonCode {
	set := reasonsByOutcome[o]
	out := make([]ReasonCode, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sortReasons(out)
	return out
}

func sortReasons(rs []ReasonCode) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j] < rs[j-1]; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}
