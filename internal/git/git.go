// Package git is a thin wrapper over the git command line.
//
// Every call goes through exec with an argv array and never through a shell,
// so branch names, paths and refs cannot word-split, glob, or inject. That
// matters here because a control plane runs unattended agents and its inputs
// are not all trustworthy.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Repo is a git repository or worktree on disk.
type Repo struct {
	Dir string
}

// Open returns a Repo rooted at dir. It does not validate; the first command
// reports a real error with git's own words.
func Open(dir string) *Repo { return &Repo{Dir: dir} }

// run executes git with an argv array. No shell is involved at any point.
func (r *Repo) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

// RevParse resolves a ref to a full object id.
func (r *Repo) RevParse(ctx context.Context, ref string) (string, error) {
	out, err := r.run(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(out)
	if s == "" {
		return "", fmt.Errorf("git rev-parse: %q does not name a commit", ref)
	}
	return s, nil
}

// Status is a single character from git's name-status output.
type Status byte

// Change is one file's difference between two commits.
type Change struct {
	Status Status // A, M, D, R, C, T
	Path   string // the path after the change
	From   string // the previous path, for a rename or copy
	Score  int    // similarity score for a rename or copy
}

// NameStatus returns the changed files between two commits.
//
// It uses -z, so paths containing spaces, quotes, newlines or non-UTF-8 bytes
// are returned verbatim rather than being quoted and escaped by git. Parsing
// the human-readable form is how path handling quietly breaks on one repo in
// a hundred.
func (r *Repo) NameStatus(ctx context.Context, base, head string) ([]Change, error) {
	out, err := r.run(ctx, "diff", "--name-status", "-z", "--no-renames=false",
		"--find-renames", base, head)
	if err != nil {
		// --no-renames=false is not accepted by every git; retry plainly.
		out, err = r.run(ctx, "diff", "--name-status", "-z", "--find-renames", base, head)
		if err != nil {
			return nil, err
		}
	}
	return parseNameStatusZ(out)
}

// parseNameStatusZ decodes the NUL-separated name-status stream.
//
// Ordinary entries are two fields: status then path. A rename or copy is three:
// status with a similarity score, then the old path, then the new one.
func parseNameStatusZ(s string) ([]Change, error) {
	fields := strings.Split(s, "\x00")
	var changes []Change
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		st := Status(f[0])
		c := Change{Status: st}
		// An empty path is truncation, not a real entry. git's -z output ends
		// with a trailing NUL, so the final split element is always empty;
		// the distinction that matters is a status field whose path is empty.
		// Accepting one would silently under-report the diff, which is the
		// failure this whole package exists to prevent.
		if st == 'R' || st == 'C' {
			if len(f) > 1 {
				fmt.Sscanf(f[1:], "%d", &c.Score)
			}
			if i+2 >= len(fields) || fields[i+1] == "" || fields[i+2] == "" {
				return nil, fmt.Errorf("git diff: truncated rename entry %q", f)
			}
			c.From, c.Path = fields[i+1], fields[i+2]
			i += 2
		} else {
			if i+1 >= len(fields) || fields[i+1] == "" {
				return nil, fmt.Errorf("git diff: truncated entry %q", f)
			}
			c.Path = fields[i+1]
			i++
		}
		changes = append(changes, c)
	}
	return changes, nil
}

// Commit is one commit's identity and full message.
type Commit struct {
	SHA     string
	Author  string
	Subject string
	Body    string
}

// Message is the subject and body rejoined, which is what a trailer scan reads.
func (c Commit) Message() string {
	if c.Body == "" {
		return c.Subject
	}
	return c.Subject + "\n\n" + c.Body
}

// Separators for the log format.
//
// The format ARGUMENT carries the literal text %x00, which git expands into a
// NUL in its output. A real NUL cannot be passed in argv at all, because argv
// is an array of NUL-terminated C strings, and exec rejects it with
// "invalid argument". Git's own placeholder is the only way to get a NUL
// delimiter out of git log.
const (
	fieldSepFmt  = "%x00"
	recordSepFmt = "%x01"
	fieldSep     = "\x00"
	recordSep    = "\x01"
)

// Commits returns every commit reachable from head but not from base, oldest
// first, which is the set a dive is responsible for.
func (r *Repo) Commits(ctx context.Context, base, head string) ([]Commit, error) {
	format := strings.Join([]string{"%H", "%an <%ae>", "%s", "%b"}, fieldSepFmt) + recordSepFmt
	out, err := r.run(ctx, "log", "--reverse", "--format="+format, base+".."+head)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(out, recordSep) {
		rec = strings.TrimLeft(rec, "\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		parts := strings.SplitN(rec, fieldSep, 4)
		if len(parts) < 4 {
			return nil, fmt.Errorf("git log: unexpected record %q", rec)
		}
		commits = append(commits, Commit{
			SHA:     parts[0],
			Author:  parts[1],
			Subject: parts[2],
			Body:    strings.TrimRight(parts[3], "\n"),
		})
	}
	return commits, nil
}

// IsAncestor reports whether a is an ancestor of b.
func (r *Repo) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", a, b)
	cmd.Dir = r.Dir
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok && ee.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git merge-base --is-ancestor: %w", err)
	}
	return true, nil
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}
