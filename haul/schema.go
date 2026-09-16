package haul

import "encoding/json"

// outcomeOrder fixes the order of the generated branches so the schema bytes
// are deterministic. A schema that reorders itself between runs churns prompt
// caches and makes diffs unreadable.
var outcomeOrder = []Outcome{
	OutcomeCompleted, OutcomePartial, OutcomeNoOp,
	OutcomeRefused, OutcomeBlocked, OutcomeFailed,
}

// ClaimsSchema returns the JSON Schema for the claims half of a haul.
//
// This is the schema handed to an agent that supports structured output, for
// example `claude -p --output-format json --json-schema "$(kraken schema)"`,
// so the vendor validates the shape before kraken ever sees it. The contract
// stops being a request the model may forget and becomes a constraint on what
// it is able to emit.
//
// Only the claims half is exposed. assignment, agent and verification belong
// to the head, and a tentacle must not be able to write any of them.
//
// The schema carries SHAPE and VOCABULARY. It cannot carry coherence.
//
// Verified 2026-09-16 against Claude Code 2.1.273: the Anthropic API refuses a
// tool input schema that uses oneOf, allOf or anyOf at the top level, with
//
//	400 tools.N.custom.input_schema: input_schema does not support oneOf,
//	allOf, or anyOf at the top level
//
// so the cross-field rules cannot be expressed here: which reason codes a given
// outcome permits, that outcome_detail is required unless the outcome is
// completed, that blocked must name a blocking resource. Those live in Validate
// and are enforced when the haul is read.
//
// That split is not a workaround, it is the same trust boundary the rest of the
// package rests on. The vendor gives a best-effort shape; kraken decides whether
// the document is actually coherent. The enums are still generated from the
// reason table, so the vocabulary cannot drift from the validator.
func ClaimsSchema() ([]byte, error) {
	return json.MarshalIndent(claimsSchema(), "", "  ")
}

// MustClaimsSchema is ClaimsSchema for callers that treat a schema failure as
// a programming error, which it is: the input is a compile-time table.
func MustClaimsSchema() []byte {
	b, err := ClaimsSchema()
	if err != nil {
		panic("haul: " + err.Error())
	}
	return b
}

type obj = map[string]any

func claimsSchema() obj {
	// No $schema key. Verified 2026-09-16 against Claude Code 2.1.273:
	// --json-schema rejects the document outright with
	//   "no schema with key or ref https://json-schema.org/draft/2020-12/schema"
	// Its validator resolves $schema as a reference rather than as a dialect
	// declaration, so naming a dialect makes the schema unusable by the one
	// consumer it exists for. The dialect is inferred instead.
	return obj{
		"title":                "Kraken tentacle claims",
		"description":          "What a tentacle reports about its own dive. Self-reported and never a gate input: kraken recomputes the diff from git and believes that instead.",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"outcome", "summary", "changes", "checks", "not_done"},
		"properties": obj{
			"outcome": obj{
				"type":        "string",
				"enum":        tentacleOutcomeList(),
				"description": "completed: did the whole assignment. partial: did some and knows which part it did not do. no_op: ran to completion and deliberately changed nothing. refused: declined on your own judgement. blocked: wanted to continue and could not. failed: tried and it did not succeed.",
			},
			"summary": obj{
				"type":        "string",
				"maxLength":   2000,
				"description": "One paragraph for a human. Never parsed.",
			},
			"outcome_detail": obj{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"reason_code", "message"},
				"description":          "Required unless outcome is completed, where it must be absent.",
				"properties": obj{
					"reason_code": obj{
						"type":        "string",
						"enum":        allReasonCodes(),
						"description": reasonScopingText(),
					},
					"message": obj{"type": "string", "maxLength": 2000},
					"blocking_resource": obj{
						"type":        "string",
						"description": "The path, tool name, or environment variable that blocked you. Required when outcome is blocked.",
					},
				},
			},
			"changes": obj{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"files"},
				"description":          "What you believe you changed. Kraken recomputes this from git, so an inaccurate list is caught rather than believed.",
				"properties": obj{
					"files": obj{
						"type": "array",
						"items": obj{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []any{"path", "change"},
							"properties": obj{
								"path": obj{
									"type":        "string",
									"description": "Relative to the worktree root. Never absolute, never containing '..'.",
								},
								"change": obj{"type": "string", "enum": []any{"added", "modified", "deleted", "renamed"}},
								"from":   obj{"type": "string", "description": "The previous path. Required for a rename."},
							},
						},
					},
					"head_commit": obj{"type": "string"},
				},
			},
			"checks": obj{
				"type":        "array",
				"description": "Commands you ran. Kraken re-runs anything it relies on, so reporting a pass you did not observe is caught, not trusted.",
				"items": obj{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []any{"name", "status"},
					"properties": obj{
						"name":    obj{"type": "string"},
						"command": obj{"type": "string"},
						"status":  obj{"type": "string", "enum": []any{"passed", "failed", "skipped"}},
						"log":     obj{"type": "string", "description": "Path to a log file, relative to the worktree. Never the log contents."},
					},
				},
			},
			"artifacts": obj{
				"type":                 "object",
				"additionalProperties": false,
				"description":          "Paths to things you produced. Existence is checked; contents are never parsed.",
				"properties": obj{
					"trajectory": obj{
						"type":        "string",
						"description": "Path to your own transcript, relative to the worktree.",
					},
					"logs": obj{
						"type":        "array",
						"items":       obj{"type": "string"},
						"description": "Paths to log files, relative to the worktree. Never log contents.",
					},
				},
			},
			"not_done": obj{
				"type":        "array",
				"description": "Work in scope that you did not do. Nothing else in the system can know this, so it is the one thing only you can report.",
				"items": obj{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []any{"what"},
					"properties": obj{
						"what": obj{"type": "string"},
						"why":  obj{"type": "string"},
					},
				},
			},
		},
	}
}

func reasonEnum(o Outcome) []any {
	rs := ReasonsFor(o)
	out := make([]any, 0, len(rs))
	for _, r := range rs {
		out = append(out, string(r))
	}
	return out
}

func tentacleOutcomeList() []any {
	out := make([]any, 0, len(outcomeOrder))
	for _, o := range outcomeOrder {
		out = append(out, string(o))
	}
	return out
}

// allReasonCodes is every reason code a tentacle may write, as one flat enum.
//
// The schema cannot scope these per outcome, so the enum is the union and
// Validate enforces the pairing. A wrong pairing is therefore caught when the
// haul is read rather than when it is produced, which is later but not weaker:
// nothing reaches the beak without passing Validate.
func allReasonCodes() []any {
	seen := map[ReasonCode]bool{}
	var out []any
	for _, o := range outcomeOrder {
		for _, r := range ReasonsFor(o) {
			if !seen[r] {
				seen[r] = true
				out = append(out, string(r))
			}
		}
	}
	sortAny(out)
	return out
}

// reasonScopingText states the pairing rules in prose, since the schema cannot
// state them structurally. A model reads the description; the validator is what
// actually holds the line.
func reasonScopingText() string {
	s := "Must match the outcome. "
	for i, o := range outcomeOrder {
		if o == OutcomeCompleted {
			continue
		}
		if i > 1 {
			s += " "
		}
		s += string(o) + ": "
		for j, r := range ReasonsFor(o) {
			if j > 0 {
				s += ", "
			}
			s += string(r)
		}
		s += "."
	}
	return s
}

func sortAny(a []any) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j].(string) < a[j-1].(string); j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
