package haul

import (
	"path/filepath"
	"testing"
)

func TestClassify_FourOutcomes(t *testing.T) {
	changed := Evidence{Recomputed: true, ChangedFiles: []string{"src/a.go"}}
	nothing := Evidence{Recomputed: true}

	tests := []struct {
		name string
		haul *Haul
		ev   Evidence
		want Disposition
	}{
		{
			name: "did the work",
			haul: base(),
			ev:   changed,
			want: DispWorkDone,
		},
		{
			name: "did nothing, and said so",
			haul: base().with(func(h *Haul) {
				h.Claims.Outcome = OutcomeNoOp
				h.Claims.Changes.Files = nil
				h.Claims.OutcomeDetail = &OutcomeDetail{ReasonCode: ReasonAlreadySatisfied, Message: "already there"}
			}),
			ev:   nothing,
			want: DispNoChange,
		},
		{
			// THE case. Exit 0, claims the work, changed nothing.
			// One git diff catches it.
			name: "did the wrong thing: completed with an empty diff",
			haul: base(),
			ev:   nothing,
			want: DispDivergent,
		},
		{
			name: "refused",
			haul: base().with(func(h *Haul) {
				h.Claims.Outcome = OutcomeRefused
				h.Claims.OutcomeDetail = &OutcomeDetail{ReasonCode: ReasonUnsafe, Message: "would delete prod data"}
				h.Claims.NotDone = []NotDoneItem{{What: "the deletion", Why: "unsafe"}}
			}),
			ev:   nothing,
			want: DispNotProceeded,
		},
		{
			name: "blocked",
			haul: base().with(func(h *Haul) {
				h.Claims.Outcome = OutcomeBlocked
				h.Claims.OutcomeDetail = &OutcomeDetail{
					ReasonCode:       ReasonCredentialMissing,
					Message:          "no token",
					BlockingResource: "GITHUB_TOKEN",
				}
				h.Claims.NotDone = []NotDoneItem{{What: "the push"}}
			}),
			ev:   nothing,
			want: DispNotProceeded,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.haul.Validate(); err != nil {
				t.Fatalf("fixture is invalid: %v", err)
			}
			got, _, _ := Classify(Result{Haul: tc.haul, State: StateValid}, tc.ev, ExitClean)
			if got != tc.want {
				t.Errorf("disposition = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassify_NoOpThatActuallyChangedThings(t *testing.T) {
	h := base().with(func(h *Haul) {
		h.Claims.Outcome = OutcomeNoOp
		h.Claims.Changes.Files = nil
		h.Claims.OutcomeDetail = &OutcomeDetail{ReasonCode: ReasonNoChangeNeeded, Message: "nothing to do"}
	})
	ev := Evidence{Recomputed: true, ChangedFiles: []string{"src/a.go"}}
	got, status, _ := Classify(Result{Haul: h, State: StateValid}, ev, ExitClean)
	if got != DispDivergent || status != StatusContradicted {
		t.Errorf("claimed no_op but changed files: got %q/%q, want divergent/contradicted", got, status)
	}
}

func TestClassify_ClaimsAloneNeverAdmit(t *testing.T) {
	// No recomputed evidence means a claim and nothing else, which is
	// explicitly not enough. KRK-002-R03.
	got, status, _ := Classify(Result{Haul: base(), State: StateValid}, Evidence{}, ExitClean)
	if got != DispIndeterminate || status != StatusNotAttempted {
		t.Errorf("got %q/%q, want indeterminate/not_attempted", got, status)
	}
}

func TestClassify_AgentAttributionIsFatal(t *testing.T) {
	ev := Evidence{
		Recomputed:       true,
		ChangedFiles:     []string{"src/a.go"},
		AgentAttribution: []string{"a1b2c3d Co-Authored-By: Claude"},
	}
	got, status, _ := Classify(Result{Haul: base(), State: StateValid}, ev, ExitClean)
	if got != DispDivergent || status != StatusContradicted {
		t.Errorf("attributed commit: got %q/%q, want divergent/contradicted", got, status)
	}
}

func TestClassify_IndeterminateIsNeverFailed(t *testing.T) {
	// "Cannot read the result" must stay distinct from "the work failed".
	for _, st := range []State{StateAbsent, StateStub, StateInvalid, StateUnreadable} {
		got, _, _ := Classify(Result{State: st}, Evidence{Recomputed: true}, ExitClean)
		if got != DispIndeterminate {
			t.Errorf("state %q: got %q, want indeterminate", st, got)
		}
		if st.Admissible() {
			t.Errorf("state %q must not be admissible to the beak", st)
		}
	}
}

func TestClassify_StubDistinguishesSignalFromCleanExit(t *testing.T) {
	// KRK-002-R41.
	_, _, killed := Classify(Result{State: StateStub}, Evidence{}, ExitSignal)
	_, _, clean := Classify(Result{State: StateStub}, Evidence{}, ExitClean)
	if killed != ReasonProcessDied {
		t.Errorf("signal-killed stub reason = %q, want %q", killed, ReasonProcessDied)
	}
	if clean != ReasonNoHaulWritten {
		t.Errorf("clean-exit stub reason = %q, want %q", clean, ReasonNoHaulWritten)
	}
	if killed == clean {
		t.Error("a killed process and a silent one must not collapse to the same reason")
	}
}

// --- validation -------------------------------------------------------------

func TestClassify_UntouchedStubIsIndeterminate(t *testing.T) {
	// The stub the head pre-wrote is still on disk, so the tentacle never
	// reported. That is not a failure, it is an absence of a result.
	path := filepath.Join(t.TempDir(), "haul.json")
	if err := WriteStub(path, base().Assignment, "claude-code"); err != nil {
		t.Fatal(err)
	}
	d, _, _ := Classify(Read(path), Evidence{Recomputed: true}, ExitClean)
	if d != DispIndeterminate {
		t.Errorf("an untouched stub is %q, want indeterminate", d)
	}
}
