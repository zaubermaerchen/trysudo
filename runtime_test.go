package main

// This file checks credential rejection and terminal preflight results before execution.

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestRuntimeRejectsCredentials(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, which := range []string{"uid", "gid", "root uid", "root gid"} {
		t.Run(which, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "target-ran")
			// A lookup error would distinguish checking credentials too late.
			if err := os.Symlink("sudo", filepath.Join(dir, "sudo")); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, code, _ := runTestCLI(t, exe, dir, testEnvironment(map[string]string{
				"TRYSUDO_CREDENTIAL_TEST": which, "TRYSUDO_CREDENTIAL_MARKER": marker, "PATH": dir,
			}), "", "-test.run=^TestRuntimeCredentialsHarness$")
			if code != 1 || stdout != "" || !strings.Contains(stderr, "unsupported credentials") {
				t.Errorf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Errorf("target ran: stat=%v", err)
			}
		})
	}
}

func TestRuntimeCredentialsHarness(t *testing.T) {
	which := os.Getenv("TRYSUDO_CREDENTIAL_TEST")
	if which == "" {
		return
	}
	creds := credentials{uid: 1000, euid: 1000, gid: 1000, egid: 1000}
	switch which {
	case "uid":
		creds.uid++
	case "gid":
		creds.gid++
	case "root uid":
		creds.euid = 0
	case "root gid":
		creds.uid = 0
		creds.euid = 0
		creds.gid++
	default:
		panic("unknown credential fixture")
	}
	os.Exit(runWithCredentials([]string{"/bin/sh", "-c", `printf ran > "$1"`, "sh", os.Getenv("TRYSUDO_CREDENTIAL_MARKER")}, os.Stdout, os.Stderr, creds))
}

func TestRuntimeTerminalPreflightResults(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		result     preflightResult
		code       int
		diagnostic string
	}{
		{"parent interrupted", preflightResult{kind: preflightInterrupted, signal: syscall.SIGINT}, 130, ""},
		{"child signaled", preflightResult{kind: preflightChildSignaled, signal: syscall.SIGKILL}, 137, ""},
		{"internal error", preflightResult{kind: preflightInternalError, err: errors.New("wait failed")}, 1, "trysudo: sudo preflight: wait failed\n"},
		{"unknown result", preflightResult{kind: preflightKind(255)}, 1, "trysudo: unknown sudo preflight result\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "target-ran")
			detail := ""
			if tt.result.err != nil {
				detail = tt.result.err.Error()
			}
			stdout, stderr, code, _ := runTestCLI(t, exe, dir, testEnvironment(map[string]string{
				"TRYSUDO_RESULT_TEST": "1", "TRYSUDO_RESULT_KIND": strconv.Itoa(int(tt.result.kind)),
				"TRYSUDO_RESULT_SIGNAL": strconv.Itoa(int(tt.result.signal)), "TRYSUDO_RESULT_ERROR": detail,
				"TRYSUDO_RESULT_MARKER": marker,
			}), "", "-test.run=^TestRuntimePreflightHarness$")
			if code != tt.code || stdout != "" || stderr != tt.diagnostic {
				t.Errorf("code=%d stdout=%q stderr=%q; want %d, %q", code, stdout, stderr, tt.code, tt.diagnostic)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Errorf("target ran: stat=%v", err)
			}
		})
	}
}

func TestRuntimePreflightHarness(t *testing.T) {
	if os.Getenv("TRYSUDO_RESULT_TEST") != "1" {
		return
	}
	kind, _ := strconv.Atoi(os.Getenv("TRYSUDO_RESULT_KIND"))
	sig, _ := strconv.Atoi(os.Getenv("TRYSUDO_RESULT_SIGNAL"))
	result := preflightResult{kind: preflightKind(kind), signal: syscall.Signal(sig)}
	if detail := os.Getenv("TRYSUDO_RESULT_ERROR"); detail != "" {
		result.err = errors.New(detail)
	}
	options := cliOptions{command: []string{"/bin/sh", "-c", `printf ran > "$1"`, "sh", os.Getenv("TRYSUDO_RESULT_MARKER")}}
	os.Exit(runAfterPreflight(options, "/unused/sudo", result, os.Stderr))
}
