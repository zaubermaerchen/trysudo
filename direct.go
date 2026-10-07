package main

// This file resolves direct commands and hands off to Unix process replacement.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func runDirect(command []string, stderr io.Writer) int {
	path, err := resolveDirect(command[0])
	if err != nil {
		fmt.Fprintf(stderr, "trysudo: %v\n", err)
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENOTDIR) {
			return 127
		}
		return 126
	}
	// Once a target is selected, even ENOENT may mean a missing interpreter or
	// loader. Do not probe again or retry through a shell or another path.
	err = syscall.Exec(path, command, os.Environ())
	fmt.Fprintf(stderr, "trysudo: %v\n", err)
	return 126
}

func resolveDirect(command string) (string, error) {
	path, err := exec.LookPath(command)
	if strings.Contains(command, "/") {
		return path, err
	}
	if err == nil && !filepath.IsAbs(path) {
		// GODEBUG can suppress ErrDot, but cannot relax our PATH policy.
		return "", &exec.Error{Name: command, Err: exec.ErrDot}
	}
	if !errors.Is(err, exec.ErrNotFound) {
		return path, err
	}
	// LookPath skips unexecutable candidates and collapses their errors into
	// ErrNotFound. Preserve its search order, then distinguish absent targets
	// from found or inaccessible targets only after the search has failed.
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		_, statErr := os.Stat(filepath.Join(dir, command))
		if statErr == nil {
			return "", &exec.Error{Name: command, Err: syscall.EACCES}
		}
		if !errors.Is(statErr, syscall.ENOENT) && !errors.Is(statErr, syscall.ENOTDIR) {
			return "", &exec.Error{Name: command, Err: statErr}
		}
	}
	return "", err
}
