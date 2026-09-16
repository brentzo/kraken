package haul

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		haul    *Haul
		wantErr string
	}{
		{"valid", base(), ""},
		{
			"outcome_detail forbidden on completed",
			base().with(func(h *Haul) {
				h.Claims.OutcomeDetail = &OutcomeDetail{ReasonCode: ReasonTurnLimit}
			}),
			"must be absent when outcome is completed",
		},
		{
			"outcome_detail required when not completed",
			base().with(func(h *Haul) { h.Claims.Outcome = OutcomeFailed }),
			"is required when outcome is",
		},
		{
			"reason_code scoped to outcome",
			base().with(func(h *Haul) {
				h.Claims.Outcome = OutcomeNoOp
				h.Claims.OutcomeDetail = &OutcomeDetail{ReasonCode: ReasonUnsafe}
			}),
			"is not permitted for outcome",
		},
		{
			"blocked must name what blocked it",
			base().with(func(h *Haul) {
				h.Claims.Outcome = OutcomeBlocked
				h.Claims.OutcomeDetail = &OutcomeDetail{ReasonCode: ReasonToolUnavailable}
				h.Claims.NotDone = []NotDoneItem{{What: "x"}}
			}),
			"blocking_resource is required",
		},
		{
			"not_done required for refused",
			base().with(func(h *Haul) {
				h.Claims.Outcome = OutcomeRefused
				h.Claims.OutcomeDetail = &OutcomeDetail{ReasonCode: ReasonPolicy}
			}),
			"not_done must be non-empty",
		},
		{
			"path must not escape the worktree",
			base().with(func(h *Haul) {
				h.Claims.Changes.Files = []FileChange{{Path: "../../etc/passwd", Change: ChangeModified}}
			}),
			"escapes the worktree root",
		},
		{
			"absolute path rejected",
			base().with(func(h *Haul) {
				h.Claims.Changes.Files = []FileChange{{Path: "/etc/passwd", Change: ChangeModified}}
			}),
			"must be relative to the worktree root",
		},
		{
			"ext must be namespaced",
			base().with(func(h *Haul) { h.Claims.Ext = map[string]any{"stuff": 1} }),
			"must be namespaced",
		},
		{
			"ext kraken prefix is reserved",
			base().with(func(h *Haul) { h.Claims.Ext = map[string]any{"kraken.internal": 1} }),
			"reserved",
		},
		{
			"timestamps need millisecond precision",
			base().with(func(h *Haul) { h.Agent.Invocation.StartedAt = "2026-09-16T09:31:47Z" }),
			"millisecond precision",
		},
		{
			"base_commit is required",
			base().with(func(h *Haul) { h.Assignment.BaseCommit = "" }),
			"base_commit is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.haul.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateAsTentacle_RejectsHeadOnlyFields(t *testing.T) {
	// KRK-002-R11: a tentacle may never write crashed or unknown.
	for _, o := range []Outcome{OutcomeCrashed, OutcomeUnknown} {
		h := base().with(func(h *Haul) {
			h.Claims.Outcome = o
			h.Claims.OutcomeDetail = &OutcomeDetail{ReasonCode: ReasonProcessDied}
		})
		if err := h.ValidateAsTentacle(); err == nil {
			t.Errorf("outcome %q: a tentacle must not be able to write it", o)
		}
	}
	// KRK-002-R02: nor the verification half.
	h := base().with(func(h *Haul) {
		h.Verification = &Verification{Status: StatusVerified, Disposition: DispWorkDone}
	})
	if err := h.ValidateAsTentacle(); err == nil || !strings.Contains(err.Error(), "only be written by the head") {
		t.Errorf("a tentacle must not be able to write verification, got %v", err)
	}
}

// --- envelope, versioning, and the reading states ---------------------------
