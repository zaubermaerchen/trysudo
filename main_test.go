package main

// This file verifies the CLI's output and argument contracts.

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"-n", "--help", "--unknown"}} {
		var stdout, stderr bytes.Buffer
		if got := run(args, &stdout, &stderr); got != 0 {
			t.Fatalf("run(%q) = %d, want 0", args, got)
		}
		for _, text := range []string{"trysudo [-n|--non-interactive] [--] command [args...]", "-h", "--help", "--version", "--non-interactive", "sudo not implemented"} {
			if !strings.Contains(stdout.String(), text) {
				t.Errorf("help output %q does not contain %q", stdout.String(), text)
			}
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr = %q, want empty", stderr.String())
		}
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{"version", []string{"--version"}, 0, "trysudo devel\n", ""},
		{"version stops parsing", []string{"--version", "--unknown"}, 0, "trysudo devel\n", ""},
		{"help wins first", []string{"--help", "--version"}, 0, helpText, ""},
		{"version wins first", []string{"--version", "--help"}, 0, "trysudo devel\n", ""},
		{"no command", nil, 2, "", "trysudo: command is required\n"},
		{"non interactive without command", []string{"-n"}, 2, "", "trysudo: command is required\n"},
		{"separator without command", []string{"--"}, 2, "", "trysudo: command is required\n"},
		{"empty command", []string{""}, 2, "", "trysudo: command must not be empty\n"},
		{"empty command after separator", []string{"--", ""}, 2, "", "trysudo: command must not be empty\n"},
		{"unknown before help", []string{"--unknown", "--help"}, 2, "", "trysudo: unknown option: --unknown\n"},
		{"unknown before version", []string{"-x", "--version"}, 2, "", "trysudo: unknown option: -x\n"},
		{"unsupported user option", []string{"-u", "root", "echo"}, 2, "", "trysudo: unknown option: -u\n"},
		{"combined options rejected", []string{"-nh"}, 2, "", "trysudo: unknown option: -nh\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(tt.args, &stdout, &stderr); got != tt.code {
				t.Errorf("exit code = %d, want %d", got, tt.code)
			}
			if got := stdout.String(); got != tt.stdout {
				t.Errorf("stdout = %q, want %q", got, tt.stdout)
			}
			if got := stderr.String(); got != tt.stderr {
				t.Errorf("stderr = %q, want %q", got, tt.stderr)
			}
		})
	}
}
