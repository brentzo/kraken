// Command kraken is the control plane's front door.
//
// Every subcommand that reads state supports --json, because an orchestrator
// that cannot be scripted against repeats the exact mistake it exists to fix.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const usage = `kraken - many tentacles, one beak

usage: kraken <command> [flags]

commands:
  dive      run one tentacle on a task and report the verdict
  verify    recompute evidence for an existing dive and report the verdict
  schema    print the JSON Schema a tentacle's claims must satisfy
  version   print the version

Run "kraken <command> -h" for a command's flags.
`

// Exit codes are part of the interface. A caller scripting against kraken
// needs to distinguish "the work landed" from "the agent refused" from
// "something lied", and a single non-zero code collapses all three.
const (
	exitOK            = 0 // work_done
	exitDivergent     = 1 // claims and evidence disagree, or attribution found
	exitNotProceeded  = 2 // refused or blocked: a human decides
	exitIndeterminate = 3 // no readable result; not a verdict about the work
	exitUsage         = 64
	exitInternal      = 70
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(exitUsage)
	}
	var err error
	code := exitOK
	switch os.Args[1] {
	case "dive":
		code, err = runDive(os.Args[2:])
	case "verify":
		code, err = runVerify(os.Args[2:])
	case "schema":
		err = runSchema(os.Args[2:])
	case "version":
		fmt.Println(version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "kraken: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(exitUsage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kraken:", err)
		if code == exitOK {
			code = exitInternal
		}
	}
	os.Exit(code)
}

var version = "0.0.0-dev"

// fs builds a flag set that prints its own usage to stderr, so -h never
// pollutes stdout that a caller may be parsing.
func fs(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ExitOnError)
	f.SetOutput(os.Stderr)
	return f
}

func abs(p string) (string, error) {
	if p == "" {
		return os.Getwd()
	}
	return filepath.Abs(p)
}

// indent is used for human output, where a block of detail sits under a line.
func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}
