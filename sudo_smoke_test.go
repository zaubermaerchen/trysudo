package main

// This file optionally checks inquiry against installed sudo without executing a target or changing policy.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestRealSudoPreflightSmoke(t *testing.T) {
	sudo, err := discoverSudo()
	if err != nil {
		t.Skipf("sudo discovery is unavailable for smoke testing: %v", err)
	}
	if sudo == "" {
		t.Skip("sudo is not installed in an absolute PATH directory")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Even non-interactive PAM configuration can stall; bound the isolated
	// smoke fixture without imposing a timeout on normal interactive preflight.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestRealSudoSmokeHarness$")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = testEnvironment(map[string]string{"TRYSUDO_SMOKE_SUDO": sudo})
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			t.Skipf("installed sudo inquiry did not finish within fixture timeout: %v", ctx.Err())
		}
		t.Fatalf("sudo smoke harness: %v; stderr %q", err, stderr.String())
	}
	var got struct {
		Kind preflightKind
		Err  string
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("sudo smoke report %q: %v", stdout.String(), err)
	}
	if (got.Kind != preflightSuccess && got.Kind != preflightOrdinaryFailure) || got.Err != "" {
		t.Fatalf("sudo inquiry result %+v; diagnostics %q", got, stderr.String())
	}
}

func TestRealSudoSmokeHarness(t *testing.T) {
	sudo := os.Getenv("TRYSUDO_SMOKE_SUDO")
	if sudo == "" {
		return
	}
	result := runPreflight(sudo, []string{"true"}, true)
	var message string
	if result.err != nil {
		message = result.err.Error()
	}
	json.NewEncoder(os.Stdout).Encode(struct {
		Kind preflightKind
		Err  string
	}{result.kind, message})
	os.Exit(0)
}
