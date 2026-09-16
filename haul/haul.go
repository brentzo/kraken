package haul

const (
	// Magic is the literal value of the first key in every haul document.
	// It exists so the head can reject a file that is valid JSON but is not
	// a haul: a half-written file, or another tool's output at the same path.
	// KRK-002-R21.
	Magic = "https://kraken.dev/haul/v1"

	// MagicKey is the required first key of the document. KRK-002-R21.
	MagicKey = "kraken_haul"

	// SchemaVersion is this build's pinned SemVer literal, never a range.
	// KRK-002-R22.
	SchemaVersion = "1.0.0"

	// MaxBytes caps a haul at 1 MiB. Logs, transcripts and diffs are
	// referenced by path rather than inlined. KRK-002-R35.
	MaxBytes = 1 << 20

	// ExtReservedPrefix is reserved for the head's own namespaced extensions.
	// KRK-002-R26.
	ExtReservedPrefix = "kraken."
)

// Haul is one dive's result document.
type Haul struct {
	KrakenHaul    string        `json:"kraken_haul"`
	SchemaVersion string        `json:"schema_version"`
	Assignment    Assignment    `json:"assignment"`
	Agent         Agent         `json:"agent"`
	Claims        Claims        `json:"claims"`
	Verification  *Verification `json:"verification,omitempty"`
}

// Assignment is injected by the head at spawn and echoed back by the tentacle
// for binding. It is compared, never trusted.
type Assignment struct {
	TaskID       string `json:"task_id"`
	Dive         int    `json:"dive"`
	PromptDigest string `json:"prompt_digest"`
	Repo         string `json:"repo"`
	BaseCommit   string `json:"base_commit"`
	Branch       string `json:"branch"`
	Worktree     string `json:"worktree,omitempty"`
}

// Agent is what the head observed about the process, not the tentacle's word.
type Agent struct {
	Name       string     `json:"name"`
	Version    string     `json:"version,omitempty"`
	Invocation Invocation `json:"invocation"`
}

// Invocation records how the tentacle was run and what it cost.
type Invocation struct {
	SessionID  string  `json:"session_id,omitempty"`
	StartedAt  string  `json:"started_at,omitempty"`
	EndedAt    string  `json:"ended_at,omitempty"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
	NumTurns   int     `json:"num_turns,omitempty"`
	StopReason string  `json:"stop_reason,omitempty"`
	ExitCode   *int    `json:"exit_code,omitempty"`
}

// Claims is written by the tentacle. Self-reported, unverified, and never a
// gate input. KRK-002-R02 forbids the head from modifying anything here.
type Claims struct {
	Outcome       Outcome        `json:"outcome"`
	Summary       string         `json:"summary"`
	OutcomeDetail *OutcomeDetail `json:"outcome_detail,omitempty"`
	Changes       Changes        `json:"changes"`
	Checks        []Check        `json:"checks"`
	NotDone       []NotDoneItem  `json:"not_done"`
	Artifacts     *Artifacts     `json:"artifacts,omitempty"`
	Ext           map[string]any `json:"ext,omitempty"`
}

// OutcomeDetail is required whenever Outcome is not completed, and forbidden
// when it is. KRK-002-R14. Forbidding it on completed keeps it from becoming a
// dumping ground, and makes "refused with no reason" a validation error rather
// than something a human discovers three days later.
type OutcomeDetail struct {
	ReasonCode       ReasonCode `json:"reason_code"`
	Message          string     `json:"message"`
	BlockingResource string     `json:"blocking_resource,omitempty"`
}

// Changes is the tentacle's account of what it touched. Every field here is
// recomputed by the head from git. KRK-002-R04.
type Changes struct {
	Files      []FileChange `json:"files"`
	HeadCommit string       `json:"head_commit,omitempty"`
	Commits    []string     `json:"commits,omitempty"`
}

// ChangeKind is how a file was touched.
type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
	ChangeDeleted  ChangeKind = "deleted"
	ChangeRenamed  ChangeKind = "renamed"
)

// FileChange is one claimed file edit. Paths are relative to the worktree root
// and must not escape it. KRK-002-R38.
type FileChange struct {
	Path    string     `json:"path"`
	Change  ChangeKind `json:"change"`
	From    string     `json:"from,omitempty"`
	Added   int        `json:"added,omitempty"`
	Removed int        `json:"removed,omitempty"`
}

// Check is a claimed check result. The head re-runs every check it intends to
// rely on and never treats Status as evidence. KRK-002-R05.
type Check struct {
	Name    string `json:"name"`
	Command string `json:"command,omitempty"`
	Status  string `json:"status"`
	Log     string `json:"log,omitempty"`
}

// NotDoneItem is irreducibly self-reported. Nothing else in the system can
// know what the tentacle decided not to do, which is why it is an escalation
// input rather than a verdict.
type NotDoneItem struct {
	What string `json:"what"`
	Why  string `json:"why,omitempty"`
}

// Artifacts are paths only. Existence is checked, contents are never parsed.
type Artifacts struct {
	Trajectory string   `json:"trajectory,omitempty"`
	Logs       []string `json:"logs,omitempty"`
}
