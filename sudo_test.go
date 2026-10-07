package main

// This file verifies secure sudo discovery and reuse of the selected executable.

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDiscoverSudo(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	bin := filepath.Join(work, "bin")
	blocked := filepath.Join(work, "blocked")
	for _, dir := range []string{bin, blocked} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{filepath.Join(work, "sudo"), filepath.Join(bin, "sudo")} {
		writeTestFile(t, path, []byte("#!/bin/sh\nexit 0\n"), 0700)
	}
	writeTestFile(t, filepath.Join(blocked, "sudo"), []byte("unexecutable"), 0600)
	loop := filepath.Join(work, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path, debug, want string
		wantErr                 error
	}{
		{"absolute PATH", bin, "", filepath.Join(bin, "sudo"), nil},
		{"missing PATH", filepath.Join(work, "missing"), "", "", nil},
		{"empty PATH", "", "", "", nil},
		{"non executable", blocked, "", "", nil},
		{"later executable wins", blocked + string(os.PathListSeparator) + bin, "", filepath.Join(bin, "sudo"), nil},
		{"not directory", filepath.Join(blocked, "sudo"), "", "", nil},
		{"relative PATH", "bin", "", "", nil},
		{"relative PATH ErrDot disabled", "bin", "execerrdot=0", "", nil},
		{"dot PATH", ".", "", "", nil},
		{"dot PATH ErrDot disabled", ".", "execerrdot=0", "", nil},
		{"empty component", string(os.PathListSeparator) + bin, "execerrdot=0", "", nil},
		{"symlink loop", loop, "", "", syscall.ELOOP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", tt.path)
			t.Setenv("GODEBUG", tt.debug)
			path, err := discoverSudo()
			if path != tt.want || !errors.Is(err, tt.wantErr) {
				t.Fatalf("discoverSudo() = (%q, %v), want (%q, %v)", path, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestDiscoveredSudoRetainsPath(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	marker := filepath.Join(dir, "selected")
	for _, path := range []string{first, second} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Fixed arguments and an absent target ensure only sudo is looked up;
	// inquiry does not require resolving or launching the target command.
	writeTestFile(t, filepath.Join(first, "sudo"), []byte("#!/bin/sh\nprintf first > \"$TRYSUDO_DISCOVERY_MARKER\"\n"), 0700)
	writeTestFile(t, filepath.Join(second, "sudo"), []byte("#!/bin/sh\nprintf second > \"$TRYSUDO_DISCOVERY_MARKER\"\n"), 0700)
	t.Setenv("TRYSUDO_DISCOVERY_MARKER", marker)
	t.Setenv("PATH", first)
	path, err := discoverSudo()
	if err != nil || path != filepath.Join(first, "sudo") {
		t.Fatalf("discoverSudo() = (%q, %v)", path, err)
	}
	t.Setenv("PATH", second)
	result := runPreflight(path, []string{"trysudo-missing-target", "argument"}, false)
	if result.kind != preflightSuccess {
		t.Fatalf("preflight = %+v, want success", result)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("executed sudo = %q, want first", got)
	}
}
