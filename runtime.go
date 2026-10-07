package main

// This file selects one execution path after credential checks and sudo preflight.

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

type credentials struct {
	uid, euid, gid, egid int
}

const fallbackNotice = "trysudo: sudo preflight unsuccessful; running directly\n"

func runCommand(options cliOptions, creds credentials, stderr io.Writer) int {
	if creds.uid != creds.euid || creds.gid != creds.egid {
		writeDiagnostic(stderr, "trysudo: unsupported credentials: real and effective UID/GID must match\n")
		return 1
	}
	// Root needs no elevation; this shortcut applies only to the fixed root target.
	if creds.euid == 0 {
		return runDirect(options.command, stderr)
	}
	sudoPath, err := discoverSudo()
	if err != nil {
		writeDiagnostic(stderr, fmt.Sprintf("trysudo: sudo discovery: %v\n", err))
		return 1
	}
	if sudoPath == "" {
		return runDirectFallback(options.command, stderr)
	}
	// runPreflight has stopped and drained notifications before returning.
	return runAfterPreflight(options, sudoPath, runPreflight(sudoPath, options.command, options.nonInteractive), stderr)
}

func runAfterPreflight(options cliOptions, sudoPath string, result preflightResult, stderr io.Writer) int {
	switch result.kind {
	case preflightInterrupted, preflightChildSignaled:
		// Numeric status also handles arbitrary child signals without signalling ourselves.
		return 128 + int(result.signal)
	case preflightInternalError:
		writeDiagnostic(stderr, fmt.Sprintf("trysudo: sudo preflight: %v\n", result.err))
		return 1
	case preflightOrdinaryFailure, preflightLaunchFailure:
		return runDirectFallback(options.command, stderr)
	case preflightSuccess:
		args := []string{sudoPath}
		if options.nonInteractive {
			args = append(args, "-n")
		}
		args = append(args, "-u", "root", "--")
		args = append(args, options.command...)
		// Reuse the discovered sudo path and original target argv. Commitment ends
		// fallback: even an exec failure must not start the target a second way.
		err := syscall.Exec(sudoPath, args, os.Environ())
		writeDiagnostic(stderr, fmt.Sprintf("trysudo: sudo exec: %v\n", err))
		return 126
	default:
		writeDiagnostic(stderr, "trysudo: unknown sudo preflight result\n")
		return 1
	}
}

func runDirectFallback(command []string, stderr io.Writer) int {
	// A raw write avoids os.File.Write's SIGPIPE handling on stderr. Do not retry
	// errors or short writes, or alter dispositions inherited by the target.
	_, _ = syscall.Write(2, []byte(fallbackNotice))
	return runDirect(command, stderr)
}

func writeDiagnostic(stderr io.Writer, message string) {
	// Diagnostic delivery must not replace the selected error status with
	// SIGPIPE termination. Keep the caller's signal dispositions untouched.
	if file, ok := stderr.(*os.File); ok {
		_, _ = syscall.Write(int(file.Fd()), []byte(message))
		return
	}
	_, _ = io.WriteString(stderr, message)
}
