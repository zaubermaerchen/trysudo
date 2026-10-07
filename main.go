package main

// This file provides the CLI entry point and argument handling.

import (
	"fmt"
	"io"
	"os"
	"strings"
)

var version = "devel"

const helpText = `Usage:
  trysudo [-n|--non-interactive] [--] command [args...]
  trysudo -h|--help
  trysudo --version

Options:
  -n, --non-interactive  Accept non-interactive mode (sudo not yet connected).
  -h, --help             Show this help.
  --version              Show the version.
  --                     End trysudo option parsing.
`

type cliOptions struct {
	nonInteractive bool
	command        []string
	help           bool
	version        bool
}

func parseCLI(args []string) (cliOptions, error) {
	var options cliOptions
	i := 0
parseOptions:
	for ; i < len(args); i++ {
		switch args[i] {
		case "-n", "--non-interactive":
			options.nonInteractive = true
		case "-h", "--help":
			options.help = true
			return options, nil
		case "--version":
			options.version = true
			return options, nil
		case "--":
			i++
			break parseOptions
		default:
			if strings.HasPrefix(args[i], "-") && args[i] != "-" {
				return options, fmt.Errorf("unknown option: %s", args[i])
			}
			break parseOptions
		}
	}
	options.command = args[i:]
	if len(options.command) == 0 {
		return options, fmt.Errorf("command is required")
	}
	if options.command[0] == "" {
		return options, fmt.Errorf("command must not be empty")
	}
	return options, nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	options, err := parseCLI(args)
	if err != nil {
		fmt.Fprintf(stderr, "trysudo: %v\n", err)
		return 2
	}
	if options.help {
		fmt.Fprint(stdout, helpText)
		return 0
	}
	if options.version {
		fmt.Fprintf(stdout, "trysudo %s\n", version)
		return 0
	}
	return runDirect(options.command, stderr)
}
