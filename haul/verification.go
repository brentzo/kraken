package haul

// VerifyStatus is the head's confidence in the claims it checked.
type VerifyStatus string

const (
	// StatusVerified: claims and recomputed evidence agree.
	StatusVerified VerifyStatus = "verified"
	// StatusContradicted: a claim and the evidence disagree. Blocks harder
	// than failed and never auto-retries, because a failing tentacle costs
	// one retry while a misreporting one costs trust in every other haul in
	// the reach. KRK-002-R09.
	StatusContradicted VerifyStatus = "contradicted"
	// StatusUnverifiable: the head could not recompute the evidence.
	StatusUnverifiable VerifyStatus = "unverifiable"
	// StatusNotAttempted: verification has not run yet.
	StatusNotAttempted VerifyStatus = "not_attempted"
)

// Verification is written only by the head, derived from git and from
// re-running checks. The beak reads this half and only this half.
// KRK-002-R03.
type Verification struct {
	VerifiedBy  string       `json:"verified_by"`
	VerifiedAt  string       `json:"verified_at"`
	HaulState   State        `json:"haul_state"`
	HaulDigest  string       `json:"haul_digest"`
	Status      VerifyStatus `json:"status"`
	Disposition Disposition  `json:"disposition"`
	Observed    *Observed    `json:"observed,omitempty"`
	Checks      []Check      `json:"checks,omitempty"`
	Error       *VerifyError `json:"error,omitempty"`
}

// Observed is what the head recomputed from the repository, as opposed to
// what the tentacle said. KRK-002-R04.
type Observed struct {
	Files      []FileChange `json:"files"`
	HeadCommit string       `json:"head_commit,omitempty"`
	BaseCommit string       `json:"base_commit,omitempty"`
	// Uncommitted is work in the worktree that never reached the branch.
	// The beak cannot integrate it, and it is not the same fact as nothing
	// having been done.
	Uncommitted []string `json:"uncommitted,omitempty"`
	// AgentAttribution lists commits carrying agent co-authorship or tool
	// attribution. Any entry fails verification and bars the branch from the
	// beak. KRK-002-R53.
	AgentAttribution []string `json:"agent_attribution,omitempty"`
}

// VerifyError records why verification could not complete, or what failed.
type VerifyError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
