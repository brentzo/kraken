package main

import (
	"fmt"
	"os"

	"github.com/brentzo/kraken/haul"
)

func runSchema(args []string) error {
	f := fs("schema")
	out := f.String("o", "", "write to a file instead of stdout")
	f.Usage = func() {
		fmt.Fprint(os.Stderr, `usage: kraken schema [-o file]

Prints the JSON Schema a tentacle's claims must satisfy.

Hand it to an agent that supports structured output, so the vendor validates
the shape before kraken ever sees it:

  claude -p --output-format json --json-schema "$(kraken schema)" "<task>"

The schema carries shape and vocabulary only. The cross-field rules, which
reason codes a given outcome permits and when outcome_detail is required, are
enforced when the haul is read: the Anthropic API refuses oneOf, allOf and
anyOf at the top level of a tool input schema, so they cannot be expressed here.
`)
	}
	if err := f.Parse(args); err != nil {
		return err
	}

	b, err := haul.ClaimsSchema()
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if *out != "" {
		return os.WriteFile(*out, b, 0o644)
	}
	_, err = os.Stdout.Write(b)
	return err
}
