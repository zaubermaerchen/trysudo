package main

// This file runs sudo's permission inquiry without choosing an execution path.

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type preflightKind uint8

const (
	preflightSuccess preflightKind = iota
	preflightOrdinaryFailure
	preflightLaunchFailure
	preflightChildSignaled
	preflightInternalError
)

type preflightResult struct {
	kind     preflightKind
	exitCode int
	signal   syscall.Signal
	err      error
}

func runPreflight(sudoPath string, command []string, nonInteractive bool) preflightResult {
	args := []string{sudoPath}
	if nonInteractive {
		args = append(args, "-n")
	}
	args = append(args, "-l", "-u", "root", "--")
	args = append(args, command...)
	// Construct Cmd with the retained path so even a changed PATH cannot cause
	// another sudo lookup. Nil stdin/stdout gives the child /dev/null, leaving
	// the caller's descriptors intact for the eventual target command.
	cmd := &exec.Cmd{Path: sudoPath, Args: args, Stderr: os.Stderr}
	if err := cmd.Start(); err != nil {
		return preflightStartError(err)
	}
	return preflightWaitResult(cmd.Wait())
}

func preflightWaitResult(err error) preflightResult {
	if err == nil {
		return preflightResult{kind: preflightSuccess}
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return preflightResult{kind: preflightInternalError, err: err}
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if ok && status.Signaled() {
		return preflightResult{kind: preflightChildSignaled, signal: status.Signal()}
	}
	if ok && status.Exited() {
		// A normal exit such as 130 is not evidence of signal termination.
		return preflightResult{kind: preflightOrdinaryFailure, exitCode: status.ExitStatus()}
	}
	return preflightResult{kind: preflightInternalError, err: err}
}

func preflightStartError(err error) preflightResult {
	// Errors setting up the child's descriptors and resource shortages must
	// not be treated as a failed sudo permission inquiry.
	var pathErr *os.PathError
	var errno syscall.Errno
	if (errors.As(err, &pathErr) && pathErr.Op != "fork/exec") ||
		!errors.As(err, &errno) || errors.Is(err, syscall.EMFILE) ||
		errors.Is(err, syscall.ENFILE) || errors.Is(err, syscall.ENOMEM) ||
		errors.Is(err, syscall.EAGAIN) {
		return preflightResult{kind: preflightInternalError, err: err}
	}
	return preflightResult{kind: preflightLaunchFailure, err: err}
}
