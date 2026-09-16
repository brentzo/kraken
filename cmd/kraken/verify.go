package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/brentzo/kraken/haul"
	"github.com/brentzo/kraken/verify"
)

func runVerify(args []string) (int, error) {
	f := fs("verify")
	dir := f.String("C", "", "worktree to verify (default: the current directory)")
	haulPath := f.String("haul", "", "path to the haul (default: <worktree>/.kraken/dives/1/haul.json)")
	asJSON := f.Bool("json", false, "print the verification record as JSON")
	killed := f.Bool("killed", false, "the tentacle was killed by a signal, so a missing haul is explained")
	f.Usage = func() {
		fmt.Fprint(os.Stderr, `usage: kraken verify [-C dir] [-haul path] [-json] [-killed]

Recomputes the evidence for a dive and reports a verdict.

It never reads claims.changes.files. The changed-file set is recomputed from
git against the haul's own base commit, which is what catches a tentacle that
exits 0 having done nothing while reporting that it did the work.

exit codes:
  0  work_done        the work landed and the evidence agrees
  1  divergent        claims and evidence disagree, or a commit carries
                      agent attribution
  2  not_proceeded    refused or blocked; a human decides
  3  indeterminate    no readable result. This is a fact about the result,
                      never about the work
`)
	}
	if err := f.Parse(args); err != nil {
		return exitUsage, err
	}

	wt, err := abs(*dir)
	if err != nil {
		return exitInternal, err
	}
	p := *haulPath
	if p == "" {
		p = filepath.Join(wt, ".kraken", "dives", "1", "haul.json")
	}

	res := haul.Read(p)
	exit := haul.ExitClean
	if *killed {
		exit = haul.ExitSignal
	}

	rec, err := verify.New(wt).Verify(context.Background(), res, exit)
	if err != nil {
		return exitInternal, err
	}
	return report(rec, res, *asJSON), nil
}

// report prints the verdict and returns the exit code for it.
func report(rec *haul.Verification, res haul.Result, asJSON bool) int {
	if asJSON {
		b, _ := json.MarshalIndent(rec, "", "  ")
		fmt.Println(string(b))
		return codeFor(rec.Disposition)
	}

	fmt.Printf("%s  (%s)\n", rec.Disposition, rec.Status)
	fmt.Printf("  haul       %s\n", rec.HaulState)
	if res.Haul != nil {
		fmt.Printf("  claimed    %s\n", res.Haul.Claims.Outcome)
	}
	if rec.Observed != nil {
		fmt.Printf("  changed    %d file(s)\n", len(rec.Observed.Files))
		for _, f := range rec.Observed.Files {
			if f.From != "" {
				fmt.Printf("               %s %s -> %s\n", f.Change, f.From, f.Path)
			} else {
				fmt.Printf("               %s %s\n", f.Change, f.Path)
			}
		}
		// Reporting only the committed diff would say "0 files" for a
		// tentacle that did the work and could not land it, which is the
		// same confusion the classifier was just taught to avoid.
		if n := len(rec.Observed.Uncommitted); n > 0 {
			fmt.Printf("  uncommitted %d file(s), not on the branch and not integrable\n", n)
			for _, u := range rec.Observed.Uncommitted {
				fmt.Printf("               %s\n", u)
			}
		}
		for _, a := range rec.Observed.AgentAttribution {
			fmt.Printf("  ATTRIBUTED %s\n", a)
		}
	}
	if rec.Error != nil && rec.Error.Message != "" {
		fmt.Printf("  why        %s\n", rec.Error.Message)
	}
	if res.Drift != "" {
		fmt.Printf("  drift      %s\n", res.Drift)
	}
	return codeFor(rec.Disposition)
}

func codeFor(d haul.Disposition) int {
	switch d {
	case haul.DispWorkDone, haul.DispNoChange:
		return exitOK
	case haul.DispDivergent:
		return exitDivergent
	case haul.DispNotProceeded:
		return exitNotProceeded
	default:
		return exitIndeterminate
	}
}
