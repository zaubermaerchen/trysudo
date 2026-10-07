package main

// This file runs sudo's permission inquiry without choosing an execution path.

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

type preflightKind uint8

const (
	preflightSuccess preflightKind = iota
	preflightOrdinaryFailure
	preflightLaunchFailure
	preflightChildSignaled
	preflightInternalError
	preflightInterrupted
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
	monitored := []os.Signal{}
	for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT} {
		if !signal.Ignored(sig) {
			monitored = append(monitored, sig)
		}
	}
	notifications := make(chan os.Signal, max(1, len(monitored)))
	// Notify with no signals would register every signal, rather than none.
	if len(monitored) != 0 {
		signal.Notify(notifications, monitored...)
	}
	return runPreflightProcess(preflightProcess{
		start: cmd.Start, wait: cmd.Wait,
		signal: func(sig os.Signal) error { return cmd.Process.Signal(sig) },
		kill:   func() error { return cmd.Process.Kill() },
	}, notifications, func() { signal.Stop(notifications) }, preflightGrace, preflightReap)
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

const (
	preflightGrace = 100 * time.Millisecond
	preflightReap  = 100 * time.Millisecond
)

// These operations let tests deterministically cover otherwise unrepeatable
// process failures and notification delivery at the teardown boundary.
type preflightProcess struct {
	start  func() error
	wait   func() error
	signal func(os.Signal) error
	kill   func() error
}

func runPreflightProcess(process preflightProcess, notifications <-chan os.Signal, stop func(), grace, reap time.Duration) (result preflightResult) {
	// The control loop alone owns the interruption state. Stop waits
	// for delivery to finish, so the final drain also covers late notifications.
	var first syscall.Signal
	defer func() {
		stop()
		for {
			select {
			case sig := <-notifications:
				if first == 0 {
					first = sig.(syscall.Signal)
				}
			default:
				if first != 0 {
					result = preflightResult{kind: preflightInterrupted, signal: first}
				}
				return
			}
		}
	}()
	if err := process.start(); err != nil {
		return preflightStartError(err)
	}
	// A blocked OS Wait may outlive bounded cleanup. Its one send must still
	// complete if Wait eventually returns after this control loop has left.
	waited := make(chan error, 1)
	go func() { waited <- process.wait() }()
	select {
	case err := <-waited:
		return preflightWaitResult(err)
	case sig := <-notifications:
		first = sig.(syscall.Signal)
	}
	// The child alone receives the signal: resending to the shared process
	// group could interrupt the caller or re-enter our own notification loop.
	_ = process.signal(first)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-waited:
		return preflightResult{}
	case <-timer.C:
	}
	_ = process.kill()
	timer.Reset(reap)
	select {
	case <-waited:
	case <-timer.C:
	}
	// Forwarding and Kill may fail; neither can undo an observed interruption.
	return preflightResult{}
}
