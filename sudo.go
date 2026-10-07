package main

// This file discovers sudo without accepting relative PATH candidates.

import (
	"errors"
	"os/exec"
	"syscall"
)

func discoverSudo() (string, error) {
	path, err := resolveDirect("sudo")
	if err == nil {
		return path, nil
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, exec.ErrDot) ||
		errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENOTDIR) {
		return "", nil
	}
	// Unexpected lookup failures must stop selection rather than silently
	// treating a broken filesystem as sudo being absent.
	return "", err
}
