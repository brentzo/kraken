package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brentzo/kraken/haul"
	"github.com/brentzo/kraken/internal/git"
	"github.com/brentzo/kraken/verify"
)

// envelope is the subset of `claude -p --output-format json` kraken depends on.
// Verified against Claude Code 2.1.273.
type envelope struct {
	StructuredOutput  json.RawMessage `json:"structured_output"`
	Result            string          `json:"result"`
	SessionID         string          `json:"session_id"`
	TotalCostUSD      float64         `json:"total_cost_usd"`
	NumTurns          int             `json:"num_turns"`
	IsError           bool            `json:"is_error"`
	Subtype           string          `json:"subtype"`
	StopReason        string          `json:"stop_reason"`
	TerminalReason    string          `json:"terminal_reason"`
	PermissionDenials []any           `json:"permission_denials"`
}

func runDive(args []string) (int, error) {
	f := fs("dive")
	dir := f.String("C", "", "repository to work in (default: the current directory)")
	model := f.String("model", "", "model for the tentacle (default: the agent's own)")
	budget := f.Float64("budget-usd", 5.00, "maximum spend for this dive")
	timeout := f.Duration("timeout", 45*time.Minute, "give up on the tentacle after this long")
	permMode := f.String("permission-mode", "acceptEdits", "permission mode for the tentacle")
	asJSON := f.Bool("json", false, "print the verification record as JSON")
	keep := f.Bool("keep", true, "keep the worktree after the dive")
	f.Usage = func() {
		fmt.Fprint(os.Stderr, `usage: kraken dive [flags] "<task>"

Runs one tentacle on a task in its own git worktree, then verifies what it
actually did against what it claimed.

The worktree, the branch and the agent process are the vendor's: kraken passes
--worktree and lets the claude CLI own isolation rather than rebuilding it.
What kraken owns is the contract and the verdict.

exit codes match kraken verify: 0 work_done, 1 divergent, 2 not_proceeded,
3 indeterminate.
`)
	}
	if err := f.Parse(args); err != nil {
		return exitUsage, err
	}
	if f.NArg() != 1 || strings.TrimSpace(f.Arg(0)) == "" {
		f.Usage()
		return exitUsage, fmt.Errorf("a task is required")
	}
	task := f.Arg(0)

	repo, err := abs(*dir)
	if err != nil {
		return exitInternal, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	r := git.Open(repo)
	// The base commit is captured BEFORE the tentacle runs. Reading it
	// afterwards would measure the diff against whatever the tentacle left,
	// which is exactly the value it could manipulate.
	base, err := r.RevParse(ctx, "HEAD")
	if err != nil {
		return exitInternal, fmt.Errorf("reading the base commit: %w", err)
	}

	taskID, err := newTaskID()
	if err != nil {
		return exitInternal, err
	}
	name := "kraken-" + taskID
	fmt.Fprintf(os.Stderr, "dive %s  base %s\n", taskID, base[:12])

	schema, err := haul.ClaimsSchema()
	if err != nil {
		return exitInternal, err
	}

	started := time.Now().UTC()
	env, runErr := runTentacle(ctx, repo, name, task, string(schema), *model, *permMode, *budget)
	ended := time.Now().UTC()

	// The worktree the vendor created. Verified layout, Claude Code 2.1.273.
	wt := filepath.Join(repo, ".claude", "worktrees", name)
	divePath := filepath.Join(wt, ".kraken", "dives", "1")
	if err := os.MkdirAll(divePath, 0o755); err != nil {
		return exitInternal, err
	}
	haulPath := filepath.Join(divePath, "haul.json")

	assignment := haul.Assignment{
		TaskID: taskID, Dive: 1, Repo: repo,
		BaseCommit: base, Branch: "worktree-" + name, Worktree: wt,
	}

	// The truthful default goes down first. If anything below fails, the
	// document on disk already says the tentacle did not report.
	if err := haul.WriteStub(haulPath, assignment, "claude-code"); err != nil {
		return exitInternal, err
	}
	excludeKrakenDir(ctx, wt)

	exitKind := haul.ExitClean
	if runErr != nil {
		exitKind = haul.ExitSignal
		fmt.Fprintf(os.Stderr, "tentacle: %v\n", runErr)
	}

	if env != nil && len(env.StructuredOutput) > 0 {
		h, err := assemble(assignment, env, started, ended)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tentacle emitted claims that fail the contract: %v\n", err)
			// The stub stays. A malformed report is not a report.
			_ = haul.PreserveRejected(haulPath, env.StructuredOutput)
		} else if err := haul.WriteAtomic(haulPath, h); err != nil {
			return exitInternal, err
		}
	} else if env != nil {
		fmt.Fprintf(os.Stderr, "tentacle produced no structured output (is_error=%v subtype=%s)\n",
			env.IsError, env.Subtype)
	}

	res := haul.Read(haulPath)
	rec, err := verify.New(wt).Verify(ctx, res, exitKind)
	if err != nil {
		return exitInternal, err
	}
	if env != nil {
		fmt.Fprintf(os.Stderr, "cost $%.4f  turns %d  session %s\n", env.TotalCostUSD, env.NumTurns, env.SessionID)
	}
	if !*keep {
		fmt.Fprintf(os.Stderr, "worktree kept at %s (removal is not automatic: the branch is the artifact)\n", wt)
	}
	return report(rec, res, *asJSON), nil
}

// runTentacle invokes the agent. No shell: the task text and the schema are
// passed as argv entries, so neither can word-split, glob, or inject.
func runTentacle(ctx context.Context, repo, name, task, schema, model, permMode string, budget float64) (*envelope, error) {
	args := []string{
		"-p",
		"--output-format", "json",
		"--json-schema", schema,
		"--worktree", name,
		"--permission-mode", permMode,
		"--max-budget-usd", fmt.Sprintf("%.2f", budget),
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, task)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = repo
	// Without this the CLI waits three seconds for stdin that never comes.
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return nil, err
	}
	defer devnull.Close()
	cmd.Stdin = devnull
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if len(out) == 0 {
		return nil, fmt.Errorf("the agent produced no output: %w", err)
	}
	var env envelope
	if jerr := json.Unmarshal(out, &env); jerr != nil {
		return nil, fmt.Errorf("the agent's envelope did not parse: %w", jerr)
	}
	// A non-zero exit with a parsed envelope is still information, so the
	// envelope is returned alongside the error rather than instead of it.
	return &env, err
}

// assemble builds the haul from the tentacle's claims and the head's own
// observations. The tentacle supplies claims and nothing else: assignment and
// agent are kraken's, and verification is written later by the verifier.
func assemble(a haul.Assignment, env *envelope, started, ended time.Time) (*haul.Haul, error) {
	var claims haul.Claims
	if err := json.Unmarshal(env.StructuredOutput, &claims); err != nil {
		return nil, fmt.Errorf("decoding claims: %w", err)
	}
	h := &haul.Haul{
		KrakenHaul:    haul.Magic,
		SchemaVersion: haul.SchemaVersion,
		Assignment:    a,
		Agent: haul.Agent{
			Name: "claude-code",
			Invocation: haul.Invocation{
				SessionID:  env.SessionID,
				StartedAt:  stamp(started),
				EndedAt:    stamp(ended),
				CostUSD:    env.TotalCostUSD,
				NumTurns:   env.NumTurns,
				StopReason: env.StopReason,
			},
		},
		Claims: claims,
	}
	// Validated as a tentacle: it may not write the head's outcome values and
	// it may not write the verification half.
	if err := h.ValidateAsTentacle(); err != nil {
		return nil, err
	}
	return h, nil
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func newTaskID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// excludeKrakenDir keeps the haul out of the tentacle's own commits. A
// committed haul gets merged, conflicts, and then describes the wrong branch.
// KRK-002-R36. Best effort: a failure here must not fail the dive.
func excludeKrakenDir(ctx context.Context, wt string) {
	out, err := exec.CommandContext(ctx, "git", "-C", wt, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return
	}
	p := strings.TrimSpace(string(out))
	if !filepath.IsAbs(p) {
		p = filepath.Join(wt, p)
	}
	if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), ".kraken/") {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString("\n# kraken's own bookkeeping; never committed onto a tentacle branch\n.kraken/\n")
}
