package verify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brentzo/kraken/haul"
)

// repo builds a throwaway git repository with one initial commit and returns
// its directory and the base commit sha.
func repo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "README.md", "start\n")
	run(t, dir, "add", "-A")
	commit(t, dir, "initial")
	base := strings.TrimSpace(out(t, dir, "rev-parse", "HEAD"))
	return dir, base
}

// run executes git with signing and hooks disabled, so a developer's global
// config cannot make these tests pass or fail for the wrong reason.
func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := runErr(dir, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

func runErr(dir string, args ...string) (string, error) {
	base := []string{
		"-c", "user.name=Test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null",
	}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func out(t *testing.T, dir string, args ...string) string {
	t.Helper()
	s, err := runErr(dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, s)
	}
	return s
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	run(t, dir, "commit", "-q", "-m", msg)
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func claimsHaul(base string, o haul.Outcome, detail *haul.OutcomeDetail, notDone []haul.NotDoneItem) *haul.Haul {
	return &haul.Haul{
		KrakenHaul:    haul.Magic,
		SchemaVersion: haul.SchemaVersion,
		Assignment: haul.Assignment{
			TaskID: "01JBQ7F3X2K9M4V8N6P0R5T2W1", Dive: 1,
			BaseCommit: base, Branch: "main",
		},
		Agent: haul.Agent{Name: "claude-code"},
		Claims: haul.Claims{
			Outcome: o, Summary: "s", OutcomeDetail: detail,
			Changes: haul.Changes{Files: []haul.FileChange{}},
			Checks:  []haul.Check{}, NotDone: notDone,
		},
	}
}

func verifier(dir string) *Verifier {
	v := New(dir)
	v.Now = func() time.Time { return time.Date(2026, 9, 16, 9, 31, 47, 902e6, time.UTC) }
	return v
}

func result(h *haul.Haul) haul.Result {
	return haul.Result{Haul: h, State: haul.StateValid, Digest: "sha256:test"}
}

// --- the case the whole contract exists for ---------------------------------

func TestVerify_ClaimedCompletedButChangedNothing(t *testing.T) {
	dir, base := repo(t)
	// The tentacle exits 0, says it did the work, and touched nothing.
	h := claimsHaul(base, haul.OutcomeCompleted, nil, nil)

	rec, err := verifier(dir).Verify(context.Background(), result(h), haul.ExitClean)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Disposition != haul.DispDivergent {
		t.Errorf("disposition = %q, want divergent", rec.Disposition)
	}
	if rec.Status != haul.StatusContradicted {
		t.Errorf("status = %q, want contradicted", rec.Status)
	}
	if rec.Error == nil || !strings.Contains(rec.Error.Message, "empty") {
		t.Errorf("want a human-readable contradiction, got %+v", rec.Error)
	}
}

func TestVerify_RealWorkIsVerified(t *testing.T) {
	dir, base := repo(t)
	write(t, dir, "src/a.go", "package a\n")
	run(t, dir, "add", "-A")
	commit(t, dir, "add a")

	rec, err := verifier(dir).Verify(context.Background(),
		result(claimsHaul(base, haul.OutcomeCompleted, nil, nil)), haul.ExitClean)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Disposition != haul.DispWorkDone || rec.Status != haul.StatusVerified {
		t.Fatalf("got %q/%q, want work_done/verified (err %+v)", rec.Disposition, rec.Status, rec.Error)
	}
	if len(rec.Observed.Files) != 1 || rec.Observed.Files[0].Path != "src/a.go" {
		t.Errorf("observed files = %+v, want exactly src/a.go", rec.Observed.Files)
	}
	if rec.Observed.Files[0].Change != haul.ChangeAdded {
		t.Errorf("change kind = %q, want added", rec.Observed.Files[0].Change)
	}
}

func TestVerify_ClaimedNoOpButChangedFiles(t *testing.T) {
	dir, base := repo(t)
	write(t, dir, "sneaky.txt", "x\n")
	run(t, dir, "add", "-A")
	commit(t, dir, "sneak")

	h := claimsHaul(base, haul.OutcomeNoOp,
		&haul.OutcomeDetail{ReasonCode: haul.ReasonAlreadySatisfied, Message: "nothing to do"}, nil)
	rec, _ := verifier(dir).Verify(context.Background(), result(h), haul.ExitClean)
	if rec.Disposition != haul.DispDivergent {
		t.Errorf("disposition = %q, want divergent", rec.Disposition)
	}
	if rec.Error == nil || !strings.Contains(rec.Error.Message, "sneaky.txt") {
		t.Errorf("the contradiction should name the file, got %+v", rec.Error)
	}
}

func TestVerify_EvidenceOverridesTheClaim(t *testing.T) {
	// The tentacle claims it edited one file. It actually edited another.
	// Verification reports what git says, never what the haul says.
	dir, base := repo(t)
	write(t, dir, "actually/this.go", "package x\n")
	run(t, dir, "add", "-A")
	commit(t, dir, "real change")

	h := claimsHaul(base, haul.OutcomeCompleted, nil, nil)
	h.Claims.Changes.Files = []haul.FileChange{{Path: "claimed/other.go", Change: haul.ChangeModified}}

	rec, _ := verifier(dir).Verify(context.Background(), result(h), haul.ExitClean)
	if rec.Observed == nil {
		t.Fatalf("no evidence was recomputed: %+v", rec.Error)
	}
	paths := []string{}
	for _, f := range rec.Observed.Files {
		paths = append(paths, f.Path)
	}
	if len(paths) != 1 || paths[0] != "actually/this.go" {
		t.Errorf("observed = %v, want the recomputed path, not the claimed one", paths)
	}
}

// --- attribution, and the case that must NOT fire ---------------------------

func TestVerify_AgentAttributionIsCaught(t *testing.T) {
	for _, msg := range []string{
		"feat: thing\n\nCo-Authored-By: Claude <noreply@anthropic.com>",
		"feat: thing\n\n🤖 Generated with Claude Code",
		"feat: thing\n\nCo-Authored-By: Copilot <copilot@github.com>",
	} {
		t.Run(strings.SplitN(msg, "\n", 3)[2], func(t *testing.T) {
			dir, base := repo(t)
			write(t, dir, "a.go", "package a\n")
			run(t, dir, "add", "-A")
			commit(t, dir, msg)

			rec, _ := verifier(dir).Verify(context.Background(),
				result(claimsHaul(base, haul.OutcomeCompleted, nil, nil)), haul.ExitClean)
			if rec.Disposition != haul.DispDivergent {
				t.Errorf("disposition = %q, want divergent", rec.Disposition)
			}
			if len(rec.Observed.AgentAttribution) == 0 {
				t.Error("the offending commit should be named in the record")
			}
		})
	}
}

func TestVerify_HumanCoAuthorIsNotAttribution(t *testing.T) {
	// The rule is narrow on purpose. Flagging real collaboration would make
	// the check useless and people would turn it off.
	dir, base := repo(t)
	write(t, dir, "a.go", "package a\n")
	run(t, dir, "add", "-A")
	commit(t, dir, "feat: thing\n\nCo-Authored-By: Jane Roe <jane@example.com>")

	rec, _ := verifier(dir).Verify(context.Background(),
		result(claimsHaul(base, haul.OutcomeCompleted, nil, nil)), haul.ExitClean)
	if len(rec.Observed.AgentAttribution) != 0 {
		t.Errorf("a human co-author must not be flagged, got %v", rec.Observed.AgentAttribution)
	}
	if rec.Disposition != haul.DispWorkDone {
		t.Errorf("disposition = %q, want work_done", rec.Disposition)
	}
}

// --- states that are not verdicts about the work ----------------------------

func TestVerify_AbsentHaulIsIndeterminateNotFailed(t *testing.T) {
	dir, _ := repo(t)
	res := haul.Read(filepath.Join(dir, "nope.json"))

	rec, err := verifier(dir).Verify(context.Background(), res, haul.ExitClean)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Disposition != haul.DispIndeterminate {
		t.Errorf("disposition = %q, want indeterminate", rec.Disposition)
	}
	if rec.Status != haul.StatusUnverifiable {
		t.Errorf("status = %q, want unverifiable", rec.Status)
	}
	if rec.HaulState != haul.StateAbsent {
		t.Errorf("haul_state = %q, want absent", rec.HaulState)
	}
}

func TestVerify_UnreadableRepoIsUnverifiableNotContradicted(t *testing.T) {
	// A base commit that does not exist means we do not know, which is a
	// different fact from catching a lie.
	dir, _ := repo(t)
	h := claimsHaul("0000000000000000000000000000000000000000", haul.OutcomeCompleted, nil, nil)

	rec, err := verifier(dir).Verify(context.Background(), result(h), haul.ExitClean)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != haul.StatusUnverifiable {
		t.Errorf("status = %q, want unverifiable", rec.Status)
	}
	if rec.Disposition != haul.DispIndeterminate {
		t.Errorf("disposition = %q, want indeterminate", rec.Disposition)
	}
	if rec.Error == nil || rec.Error.Code != "repo_unreadable" {
		t.Errorf("want a repo_unreadable error, got %+v", rec.Error)
	}
}

func TestVerify_RefusalIsTerminalNotAnError(t *testing.T) {
	dir, base := repo(t)
	h := claimsHaul(base, haul.OutcomeRefused,
		&haul.OutcomeDetail{ReasonCode: haul.ReasonUnsafe, Message: "would drop prod"},
		[]haul.NotDoneItem{{What: "the migration"}})

	rec, _ := verifier(dir).Verify(context.Background(), result(h), haul.ExitClean)
	if rec.Disposition != haul.DispNotProceeded {
		t.Errorf("disposition = %q, want not_proceeded", rec.Disposition)
	}
	if rec.Status != haul.StatusVerified {
		t.Errorf("status = %q, want verified: a refusal is a successful transaction", rec.Status)
	}
	if rec.Error != nil {
		t.Errorf("a refusal must not be recorded as an error, got %+v", rec.Error)
	}
}

// --- path handling, which is where this quietly breaks ----------------------

func TestVerify_AwkwardPaths(t *testing.T) {
	dir, base := repo(t)
	for _, name := range []string{
		"a file with spaces.txt",
		"quote\"inside.txt",
		"café/ünïcode.txt",
	} {
		write(t, dir, name, "x\n")
	}
	run(t, dir, "add", "-A")
	commit(t, dir, "awkward names")

	rec, _ := verifier(dir).Verify(context.Background(),
		result(claimsHaul(base, haul.OutcomeCompleted, nil, nil)), haul.ExitClean)
	if len(rec.Observed.Files) != 3 {
		t.Fatalf("got %d files, want 3: %+v", len(rec.Observed.Files), rec.Observed.Files)
	}
	for _, f := range rec.Observed.Files {
		if strings.HasPrefix(f.Path, `"`) {
			t.Errorf("path %q is git-quoted; the -z form should return it verbatim", f.Path)
		}
	}
}

func TestVerify_RenameIsReportedAsRename(t *testing.T) {
	dir, base := repo(t)
	write(t, dir, "old.txt", strings.Repeat("stable content\n", 20))
	run(t, dir, "add", "-A")
	commit(t, dir, "add")
	baseAfter := strings.TrimSpace(out(t, dir, "rev-parse", "HEAD"))
	run(t, dir, "mv", "old.txt", "new.txt")
	commit(t, dir, "rename")

	h := claimsHaul(baseAfter, haul.OutcomeCompleted, nil, nil)
	rec, _ := verifier(dir).Verify(context.Background(), result(h), haul.ExitClean)
	if len(rec.Observed.Files) != 1 {
		t.Fatalf("got %+v, want one rename entry", rec.Observed.Files)
	}
	f := rec.Observed.Files[0]
	if f.Change != haul.ChangeRenamed || f.From != "old.txt" || f.Path != "new.txt" {
		t.Errorf("got %+v, want a rename old.txt -> new.txt", f)
	}
	_ = base
}

func TestVerify_RecordIsReproducible(t *testing.T) {
	dir, base := repo(t)
	write(t, dir, "a.go", "package a\n")
	run(t, dir, "add", "-A")
	commit(t, dir, "add")

	res := result(claimsHaul(base, haul.OutcomeCompleted, nil, nil))
	a, _ := verifier(dir).Verify(context.Background(), res, haul.ExitClean)
	b, _ := verifier(dir).Verify(context.Background(), res, haul.ExitClean)
	if a.VerifiedAt != b.VerifiedAt || a.Disposition != b.Disposition || a.HaulDigest != b.HaulDigest {
		t.Error("the same inputs must produce the same record")
	}
	if a.HaulDigest != "sha256:test" {
		t.Errorf("digest = %q, want the exact bytes read", a.HaulDigest)
	}
}

// Found by a real dive on 2026-09-16: the tentacle edited a file, was denied
// `git add` by the permission layer, and honestly reported partial. Reading
// only committed history called that zero changed files, which would have made
// an honest report look like a contradiction.
func TestVerify_UncommittedWorkIsNotNothing(t *testing.T) {
	dir, base := repo(t)
	write(t, dir, "greet.py", "def greet(name):\n    \"\"\"Greet someone.\"\"\"\n    return name\n")
	// Deliberately NOT committed.

	h := claimsHaul(base, haul.OutcomePartial,
		&haul.OutcomeDetail{ReasonCode: haul.ReasonPermissionRequired, Message: "git add was denied"},
		[]haul.NotDoneItem{{What: "the commit", Why: "permission denied"}})

	rec, err := verifier(dir).Verify(context.Background(), result(h), haul.ExitClean)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Disposition != haul.DispNotProceeded {
		t.Errorf("disposition = %q, want not_proceeded: the work is real but not integrable", rec.Disposition)
	}
	if rec.Status == haul.StatusContradicted {
		t.Error("an honest partial must not be recorded as a contradiction")
	}
	if len(rec.Observed.Uncommitted) == 0 {
		t.Error("the modified file must be reported as uncommitted, not as nothing")
	}
	if len(rec.Observed.Files) != 0 {
		t.Error("uncommitted work must not appear as a committed change")
	}
}

// The same dirty worktree under a completed claim is still a contradiction:
// uncommitted work is not the work being claimed.
func TestVerify_CompletedWithOnlyUncommittedWorkIsStillDivergent(t *testing.T) {
	dir, base := repo(t)
	write(t, dir, "a.go", "package a\n")

	rec, _ := verifier(dir).Verify(context.Background(),
		result(claimsHaul(base, haul.OutcomeCompleted, nil, nil)), haul.ExitClean)
	if rec.Disposition != haul.DispDivergent {
		t.Errorf("disposition = %q, want divergent", rec.Disposition)
	}
	if rec.Error == nil || !strings.Contains(rec.Error.Message, "nothing was committed") {
		t.Errorf("the reason should name the real problem, got %+v", rec.Error)
	}
}

func TestVerify_UntrackedFilesCountAsUncommitted(t *testing.T) {
	dir, base := repo(t)
	write(t, dir, "brand/new.txt", "x\n")

	rec, _ := verifier(dir).Verify(context.Background(),
		result(claimsHaul(base, haul.OutcomeCompleted, nil, nil)), haul.ExitClean)
	var found bool
	for _, u := range rec.Observed.Uncommitted {
		if u == "brand/new.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("an untracked file is uncommitted work, got %v", rec.Observed.Uncommitted)
	}
}
