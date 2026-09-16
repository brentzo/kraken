package haul

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// rfc3339Milli matches an RFC 3339 UTC timestamp with millisecond precision,
// for example 2026-09-16T09:31:47.902Z. KRK-002-R39.
var rfc3339Milli = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

// ValidationError collects every problem found, so a tentacle fixing its
// output sees all of them at once rather than one per round trip.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 1 {
		return "haul: " + e.Problems[0]
	}
	return fmt.Sprintf("haul: %d problems: %s", len(e.Problems), strings.Join(e.Problems, "; "))
}

// Validate checks a decoded haul against the contract. It does not touch the
// filesystem and does not consult git, so it is the same check on the writing
// side and the reading side.
func (h *Haul) Validate() error {
	var p []string
	add := func(f string, a ...any) { p = append(p, fmt.Sprintf(f, a...)) }

	// Envelope. KRK-002-R21, R22.
	if h.KrakenHaul != Magic {
		add("kraken_haul must be %q, got %q", Magic, h.KrakenHaul)
	}
	v, err := ParseVersion(h.SchemaVersion)
	if err != nil {
		add("%s", err.Error())
	} else if Compatible(v) == CompatUnknownMajor {
		add("schema_version %s has an unknown MAJOR, this build reads %d.x", v, Current.Major)
	}

	// Assignment binding.
	if h.Assignment.TaskID == "" {
		add("assignment.task_id is required")
	}
	if h.Assignment.Dive < 1 {
		add("assignment.dive must be 1 or greater, got %d", h.Assignment.Dive)
	}
	if h.Assignment.BaseCommit == "" {
		add("assignment.base_commit is required, the beak and the evidence half both depend on it")
	}

	// Outcome vocabulary. KRK-002-R11.
	if !h.Claims.Outcome.Valid() {
		add("claims.outcome %q is not in the closed enum", h.Claims.Outcome)
	}

	// outcome_detail present iff outcome is not completed. KRK-002-R14.
	switch {
	case h.Claims.Outcome == OutcomeCompleted && h.Claims.OutcomeDetail != nil:
		add("claims.outcome_detail must be absent when outcome is completed")
	case h.Claims.Outcome != OutcomeCompleted && h.Claims.Outcome.Valid() && h.Claims.OutcomeDetail == nil:
		add("claims.outcome_detail is required when outcome is %q", h.Claims.Outcome)
	}

	if d := h.Claims.OutcomeDetail; d != nil {
		// reason_code scoped to the outcome. KRK-002-R15.
		if !d.ReasonCode.PermittedFor(h.Claims.Outcome) {
			add("claims.outcome_detail.reason_code %q is not permitted for outcome %q, permitted: %v",
				d.ReasonCode, h.Claims.Outcome, ReasonsFor(h.Claims.Outcome))
		}
		// blocked must name what blocked it. KRK-002-R16.
		if h.Claims.Outcome == OutcomeBlocked && d.BlockingResource == "" {
			add("claims.outcome_detail.blocking_resource is required when outcome is blocked, and must name a path, tool name, or environment variable")
		}
	}

	// not_done must be non-empty for partial, refused, blocked. KRK-002-R17.
	if h.Claims.Outcome.RequiresNotDone() && len(h.Claims.NotDone) == 0 {
		add("claims.not_done must be non-empty when outcome is %q", h.Claims.Outcome)
	}
	for i, nd := range h.Claims.NotDone {
		if nd.What == "" {
			add("claims.not_done[%d].what is required", i)
		}
	}

	// Paths are relative to the worktree root and must not escape it.
	// KRK-002-R38.
	for i, f := range h.Claims.Changes.Files {
		if err := checkContainedPath(f.Path); err != nil {
			add("claims.changes.files[%d].path: %s", i, err)
		}
		switch f.Change {
		case ChangeAdded, ChangeModified, ChangeDeleted, ChangeRenamed:
		default:
			add("claims.changes.files[%d].change %q is not a known change kind", i, f.Change)
		}
		if f.Change == ChangeRenamed && f.From == "" {
			add("claims.changes.files[%d].from is required for a rename", i)
		}
	}
	if h.Claims.Artifacts != nil {
		for i, l := range h.Claims.Artifacts.Logs {
			if err := checkContainedPath(l); err != nil {
				add("claims.artifacts.logs[%d]: %s", i, err)
			}
		}
		if t := h.Claims.Artifacts.Trajectory; t != "" {
			if err := checkContainedPath(t); err != nil {
				add("claims.artifacts.trajectory: %s", err)
			}
		}
	}
	for i, c := range h.Claims.Checks {
		if c.Name == "" {
			add("claims.checks[%d].name is required", i)
		}
		if c.Log != "" {
			if err := checkContainedPath(c.Log); err != nil {
				add("claims.checks[%d].log: %s", i, err)
			}
		}
	}

	// ext namespacing, with the kraken. prefix reserved. KRK-002-R26.
	for k := range h.Claims.Ext {
		if strings.HasPrefix(k, ExtReservedPrefix) {
			add("claims.ext[%q] uses the reserved %q prefix", k, ExtReservedPrefix)
		} else if !strings.Contains(k, ".") {
			add("claims.ext[%q] must be namespaced as <vendor>.<agent>", k)
		}
	}

	// Timestamps. KRK-002-R39.
	for _, ts := range []struct{ field, val string }{
		{"agent.invocation.started_at", h.Agent.Invocation.StartedAt},
		{"agent.invocation.ended_at", h.Agent.Invocation.EndedAt},
	} {
		if ts.val != "" && !rfc3339Milli.MatchString(ts.val) {
			add("%s %q must be RFC 3339 UTC with millisecond precision, for example 2026-09-16T09:31:47.902Z", ts.field, ts.val)
		}
	}

	if len(p) > 0 {
		return &ValidationError{Problems: p}
	}
	return nil
}

// ValidateAsTentacle applies Validate plus the rules that bind only a
// tentacle: it may not write the head's own outcome values, and it may not
// write the verification half. KRK-002-R11, KRK-002-R02.
func (h *Haul) ValidateAsTentacle() error {
	err := h.Validate()
	var p []string
	if !h.Claims.Outcome.WritableByTentacle() {
		p = append(p, fmt.Sprintf("claims.outcome %q may only be written by the head", h.Claims.Outcome))
	}
	if h.Verification != nil {
		p = append(p, "verification may only be written by the head")
	}
	if len(p) == 0 {
		return err
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		ve.Problems = append(ve.Problems, p...)
		return ve
	}
	return &ValidationError{Problems: p}
}

// checkContainedPath rejects absolute paths and anything that escapes the
// worktree root. KRK-002-R38.
func checkContainedPath(p string) error {
	switch {
	case p == "":
		return errors.New("path is required")
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("%q must be relative to the worktree root", p)
	case len(p) > 1 && p[1] == ':':
		return fmt.Errorf("%q must be relative to the worktree root", p)
	}
	for _, seg := range strings.Split(filepathSlash(p), "/") {
		if seg == ".." {
			return fmt.Errorf("%q escapes the worktree root", p)
		}
	}
	return nil
}

func filepathSlash(p string) string { return strings.ReplaceAll(p, `\`, "/") }
