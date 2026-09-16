// Package haul defines the tentacle result contract: what a tentacle brings
// back from a dive, and how the head decides whether to believe it.
//
// The contract has two structurally separate halves written by two different
// writers. Claims are self-reported by the tentacle and are never a gate
// input. Verification is recomputed by the head from git and from re-running
// checks, and it is the only half the beak reads.
//
// The rule that falls out: the tentacle may report intent, scope and refusals,
// and only the head may report verdicts. An agent reporting "tests passed"
// is hearsay. It both chose the command and interpreted the output, and it can
// do either wrong without lying.
//
// This package has no dependencies outside the standard library, and it
// imports nothing else from kraken. The spawner produces hauls, the beak
// consumes them, and neither imports the other.
//
// Specification: docs/specs/KRK-SPEC-002-tentacle-result-haul.md
package haul
