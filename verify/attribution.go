package verify

import (
	"regexp"
	"strings"

	"github.com/brentzo/kraken/internal/git"
)

// agentMarkers are the identities and phrases that mark a commit as
// attributed to a coding agent.
//
// The rule being enforced is narrow on purpose: a Co-Authored-By naming a
// PERSON is legitimate collaboration and must not be flagged. Only an agent
// or model identity is a violation, so this matches identities rather than
// the trailer itself.
var agentMarkers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bnoreply@anthropic\.com\b`),
	regexp.MustCompile(`(?i)\bclaude\b`),
	regexp.MustCompile(`(?i)\bchatgpt\b|\bopenai\b|\bgpt-[0-9]`),
	regexp.MustCompile(`(?i)\bcopilot\b`),
	regexp.MustCompile(`(?i)\bcursor\b`),
	regexp.MustCompile(`(?i)\bcodex\b`),
	regexp.MustCompile(`(?i)\bdevin\b`),
	regexp.MustCompile(`(?i)\bgemini\b`),
	regexp.MustCompile(`(?i)\baider\b`),
}

// generatedWith matches the tool-attribution footer agents add unprompted.
var generatedWith = regexp.MustCompile(`(?im)^\s*(🤖\s*)?generated with\b`)

// coAuthoredBy matches the trailer, so the agent check can be scoped to it
// rather than to the whole message, where a commit legitimately discussing
// Claude in prose would otherwise be flagged.
var coAuthoredBy = regexp.MustCompile(`(?im)^\s*co-authored-by:\s*(.+)$`)

// Attribution is one commit found carrying agent attribution.
type Attribution struct {
	SHA    string
	Reason string
}

// String renders an attribution for the verification record.
func (a Attribution) String() string { return a.SHA[:min(12, len(a.SHA))] + " " + a.Reason }

// scanAttribution finds commits carrying agent co-authorship or tool
// attribution. KRK-002-R53.
//
// This is deliberately evidence rather than a claim. Instructing a model not
// to sign its work is a request; a model reverts to habit after a compaction
// or a long conversation. Reading the trailers is the part that cannot be
// talked out of.
func scanAttribution(commits []git.Commit) []Attribution {
	var found []Attribution
	for _, c := range commits {
		msg := c.Message()

		for _, m := range coAuthoredBy.FindAllStringSubmatch(msg, -1) {
			who := strings.TrimSpace(m[1])
			if isAgentIdentity(who) {
				found = append(found, Attribution{
					SHA:    c.SHA,
					Reason: "Co-Authored-By names an agent: " + who,
				})
			}
		}
		if generatedWith.MatchString(msg) {
			found = append(found, Attribution{
				SHA:    c.SHA,
				Reason: "carries a \"Generated with\" attribution line",
			})
		}
		// The author itself can be the agent, which no trailer would show.
		if isAgentIdentity(c.Author) {
			found = append(found, Attribution{
				SHA:    c.SHA,
				Reason: "authored by an agent identity: " + c.Author,
			})
		}
	}
	return found
}

func isAgentIdentity(s string) bool {
	for _, m := range agentMarkers {
		if m.MatchString(s) {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
