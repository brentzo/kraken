// Package verify produces the evidence half of a haul.
//
// It is the only thing permitted to write haul.Verification, and it is what
// turns the trust boundary from a struct field into a mechanism. Without it
// haul.Classify can never return an admissible disposition, because a claim
// on its own is not evidence.
//
// Nothing here executes agent code. It reads git and, in a later slice, re-runs
// the repository's own check commands. That is a far smaller blast radius than
// the spawning half of a control plane and it is why this package can be
// trusted by a gate that does not trust its producers.
package verify

import (
	"context"
	"fmt"
	"time"

	"github.com/brentzo/kraken/haul"
	"github.com/brentzo/kraken/internal/git"
)

// Verifier recomputes evidence for one dive.
type Verifier struct {
	Repo *git.Repo
	// Now is injectable so a verification record is reproducible in tests.
	Now func() time.Time
	// VerifiedBy names the build that produced the record.
	VerifiedBy string
}

// New returns a Verifier rooted at the worktree dir.
func New(dir string) *Verifier {
	return &Verifier{
		Repo:       git.Open(dir),
		Now:        time.Now,
		VerifiedBy: "kraken",
	}
}

// Verify recomputes the evidence for a dive and returns the verification half.
//
// It never trusts claims.changes.files: the changed-file set is recomputed from
// git against the assignment's base commit, which is what catches a tentacle
// that exits 0 having done nothing while reporting that it did the work.
// KRK-002-R04.
//
// A haul that could not be read is still verified, to the extent that
// "indeterminate" is a verdict. An unreadable result is a fact about the
// result, not about the work, and it never becomes "failed". KRK-002-R20.
func (v *Verifier) Verify(ctx context.Context, res haul.Result, exit haul.ExitKind) (*haul.Verification, error) {
	rec := &haul.Verification{
		VerifiedBy: v.VerifiedBy,
		VerifiedAt: v.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		HaulState:  res.State,
		HaulDigest: res.Digest,
	}

	// Nothing readable means nothing to recompute against. Record the verdict
	// and the reason, and do not guess.
	if res.Haul == nil {
		d, status, reason := haul.Classify(res, haul.Evidence{}, exit)
		rec.Status, rec.Disposition = status, d
		rec.Error = &haul.VerifyError{
			Code:    string(reason),
			Message: errText(res.Err),
		}
		return rec, nil
	}

	ev, observed, err := v.evidence(ctx, res.Haul)
	if err != nil {
		// The repository could not be read. That is unverifiable, which is
		// distinct from contradicted: we do not know, rather than we caught
		// a lie.
		rec.Status = haul.StatusUnverifiable
		rec.Disposition = haul.DispIndeterminate
		rec.Error = &haul.VerifyError{Code: "repo_unreadable", Message: err.Error()}
		return rec, nil
	}

	rec.Observed = observed
	d, status, reason := haul.Classify(res, ev, exit)
	rec.Status, rec.Disposition = status, d

	if status == haul.StatusContradicted {
		rec.Error = &haul.VerifyError{
			Code:    string(reason),
			Message: contradiction(res.Haul, ev),
		}
	}
	return rec, nil
}

// evidence recomputes what actually happened in the repository.
func (v *Verifier) evidence(ctx context.Context, h *haul.Haul) (haul.Evidence, *haul.Observed, error) {
	base, err := v.Repo.RevParse(ctx, h.Assignment.BaseCommit)
	if err != nil {
		return haul.Evidence{}, nil, fmt.Errorf("resolving base commit: %w", err)
	}
	head, err := v.Repo.RevParse(ctx, "HEAD")
	if err != nil {
		return haul.Evidence{}, nil, fmt.Errorf("resolving HEAD: %w", err)
	}

	changes, err := v.Repo.NameStatus(ctx, base, head)
	if err != nil {
		return haul.Evidence{}, nil, fmt.Errorf("recomputing the diff: %w", err)
	}

	commits, err := v.Repo.Commits(ctx, base, head)
	if err != nil {
		return haul.Evidence{}, nil, fmt.Errorf("reading commits: %w", err)
	}

	dirty, err := v.Repo.Dirty(ctx)
	if err != nil {
		return haul.Evidence{}, nil, fmt.Errorf("reading worktree state: %w", err)
	}
	uncommitted := make([]string, 0, len(dirty))
	for _, d := range dirty {
		uncommitted = append(uncommitted, d.Path)
	}

	observed := &haul.Observed{
		HeadCommit: head,
		BaseCommit: base,
		Files:      make([]haul.FileChange, 0, len(changes)),
	}
	paths := make([]string, 0, len(changes))
	for _, c := range changes {
		observed.Files = append(observed.Files, haul.FileChange{
			Path:   c.Path,
			Change: changeKind(c.Status),
			From:   c.From,
		})
		paths = append(paths, c.Path)
	}

	for _, a := range scanAttribution(commits) {
		observed.AgentAttribution = append(observed.AgentAttribution, a.String())
	}

	observed.Uncommitted = uncommitted

	return haul.Evidence{
		Recomputed:       true,
		ChangedFiles:     paths,
		Uncommitted:      uncommitted,
		AgentAttribution: observed.AgentAttribution,
		// Check re-running is a later slice. Claiming ChecksRun here would
		// assert an inspection that never happened, which is the exact
		// dishonesty this package exists to catch.
		ChecksRun:    false,
		ChecksPassed: false,
	}, observed, nil
}

func changeKind(s git.Status) haul.ChangeKind {
	switch s {
	case 'A':
		return haul.ChangeAdded
	case 'D':
		return haul.ChangeDeleted
	case 'R', 'C':
		return haul.ChangeRenamed
	default:
		return haul.ChangeModified
	}
}

// contradiction says, in one sentence a human can act on, what disagreed.
func contradiction(h *haul.Haul, ev haul.Evidence) string {
	if len(ev.AgentAttribution) > 0 {
		return fmt.Sprintf("%d commit(s) carry agent attribution: %v",
			len(ev.AgentAttribution), ev.AgentAttribution)
	}
	switch h.Claims.Outcome {
	case haul.OutcomeCompleted, haul.OutcomePartial:
		if len(ev.ChangedFiles) == 0 {
			if len(ev.Uncommitted) > 0 {
				return fmt.Sprintf("claimed %q and %d file(s) are modified in the worktree, but nothing was committed, so there is nothing for the beak to integrate: %v",
					h.Claims.Outcome, len(ev.Uncommitted), ev.Uncommitted)
			}
			return fmt.Sprintf("claimed %q but the recomputed diff against %s is empty",
				h.Claims.Outcome, short(h.Assignment.BaseCommit))
		}
	case haul.OutcomeNoOp:
		return fmt.Sprintf("claimed no_op but %d file(s) changed: %v",
			len(ev.ChangedFiles), ev.ChangedFiles)
	}
	return "claims and recomputed evidence disagree"
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
