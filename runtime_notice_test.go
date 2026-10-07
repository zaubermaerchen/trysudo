package main

// This file checks best-effort runtime writes and unchanged SIGPIPE inheritance.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestFallbackNoticeWriteFailure(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range []string{"broken pipe", "closed"} {
		for _, fixture := range []struct{ ignored, probe bool }{{false, false}, {false, true}, {true, true}} {
			ignored, probe := fixture.ignored, fixture.probe
			t.Run(descriptor+" SIGPIPE ignored="+strconv.FormatBool(ignored)+" probe="+strconv.FormatBool(probe), func(t *testing.T) {
				marker := filepath.Join(t.TempDir(), "target-ran")
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, exe, "-test.run=^TestRuntimeNoticeHarness$")
				cmd.Env = testEnvironment(map[string]string{
					"PATH": "", "TRYSUDO_NOTICE_HARNESS": "1", "TRYSUDO_NOTICE_DESCRIPTOR": descriptor,
					"TRYSUDO_NOTICE_IGNORED": strconv.FormatBool(ignored), "TRYSUDO_NOTICE_MARKER": marker,
					"TRYSUDO_NOTICE_PROBE": strconv.FormatBool(probe),
				})
				var stdout bytes.Buffer
				cmd.Stdout = &stdout
				if descriptor == "broken pipe" {
					reader, writer, err := os.Pipe()
					if err != nil {
						t.Fatal(err)
					}
					if err := reader.Close(); err != nil {
						t.Fatal(err)
					}
					defer writer.Close()
					cmd.Stderr = writer
				}
				err := cmd.Run()
				if ctx.Err() != nil {
					t.Fatalf("fallback timed out: %v", ctx.Err())
				}
				if _, ok := err.(*exec.ExitError); !ok {
					t.Fatalf("fallback exit = %v, want target termination; stdout %q", err, stdout.String())
				}
				if probe && !ignored {
					status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
					if !ok || !status.Signaled() || status.Signal() != syscall.SIGPIPE {
						t.Fatalf("target status = %v, want SIGPIPE termination", cmd.ProcessState)
					}
				} else if cmd.ProcessState.ExitCode() != 37 {
					t.Fatalf("fallback exit = %v, want target exit 37; stdout %q", err, stdout.String())
				}
				if probe && ignored {
					if stdout.String() != "continued after SIGPIPE\n" {
						t.Errorf("POSIX target did not preserve ignored SIGPIPE: %q", stdout.String())
					}
				} else if !probe {
					var got struct{ Ignored bool }
					if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
						t.Fatalf("target report %q: %v", stdout.String(), err)
					}
					if got.Ignored {
						t.Error("target inherited an unexpected ignored SIGPIPE")
					}
				}
				if data, err := os.ReadFile(marker); err != nil || string(data) != "executed" {
					t.Errorf("target marker = %q, %v", data, err)
				}
			})
		}
	}
}

func TestRuntimeNoticeHarness(t *testing.T) {
	if os.Getenv("TRYSUDO_NOTICE_HARNESS") != "1" {
		return
	}
	ignored, _ := strconv.ParseBool(os.Getenv("TRYSUDO_NOTICE_IGNORED"))
	if ignored {
		signal.Ignore(syscall.SIGPIPE)
	}
	if signal.Ignored(syscall.SIGPIPE) != ignored {
		os.Exit(91)
	}
	if os.Getenv("TRYSUDO_NOTICE_DESCRIPTOR") == "closed" {
		if err := syscall.Close(2); err != nil {
			os.Exit(92)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(93)
	}
	command := []string{exe, "-test.run=^TestRuntimeNoticeTarget$"}
	probe, _ := strconv.ParseBool(os.Getenv("TRYSUDO_NOTICE_PROBE"))
	if probe {
		// A new Go runtime installs its own SIGPIPE handler, obscuring the
		// inherited disposition. A POSIX target proves the disposition by
		// terminating or surviving its own SIGPIPE, according to its input.
		command = []string{"/bin/sh", "-c", `printf executed > "$TRYSUDO_NOTICE_MARKER"; kill -PIPE "$$"; printf 'continued after SIGPIPE\n'; exit 37`}
	}
	os.Exit(runWithCredentials(command, os.Stdout, os.Stderr,
		credentials{uid: 1000, euid: 1000, gid: 1000, egid: 1000}))
}

func TestRuntimeNoticeTarget(t *testing.T) {
	marker := os.Getenv("TRYSUDO_NOTICE_MARKER")
	if marker == "" {
		return
	}
	if err := os.WriteFile(marker, []byte("executed"), 0600); err != nil {
		os.Exit(94)
	}
	json.NewEncoder(os.Stdout).Encode(struct{ Ignored bool }{signal.Ignored(syscall.SIGPIPE)})
	os.Exit(37)
}

func TestRuntimeDiagnosticWriteFailure(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range []string{"broken pipe", "closed", "open"} {
		for _, fixture := range []struct {
			name       string
			code       int
			diagnostic string
		}{
			{name: "credentials", code: 1}, {name: "discovery", code: 1}, {name: "preflight internal", code: 1},
			{name: "unknown result", code: 1}, {name: "final sudo exec", code: 126},
			{name: "direct missing", code: 127}, {name: "direct ENOEXEC", code: 126},
			{name: "usage no command", code: 2, diagnostic: "trysudo: command is required\n"},
			{name: "usage invalid option", code: 2, diagnostic: "trysudo: unknown option: --invalid\n"},
		} {
			t.Run(descriptor+" "+fixture.name, func(t *testing.T) {
				dir := t.TempDir()
				marker := filepath.Join(dir, "target-ran")
				if err := os.Symlink("sudo", filepath.Join(dir, "sudo")); err != nil {
					t.Fatal(err)
				}
				if fixture.name == "direct ENOEXEC" {
					writeTestFile(t, filepath.Join(dir, "unexecutable-script"), []byte(`printf executed > "$TRYSUDO_DIAGNOSTIC_MARKER"`), 0700)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, exe, "-test.run=^TestRuntimeDiagnosticHarness$")
				cmd.Env = testEnvironment(map[string]string{
					"PATH": dir, "TRYSUDO_DIAGNOSTIC_TEST": fixture.name,
					"TRYSUDO_DIAGNOSTIC_DESCRIPTOR": descriptor, "TRYSUDO_DIAGNOSTIC_MARKER": marker,
				})
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				if descriptor == "broken pipe" {
					reader, writer, err := os.Pipe()
					if err != nil {
						t.Fatal(err)
					}
					if err := reader.Close(); err != nil {
						writer.Close()
						t.Fatal(err)
					}
					defer writer.Close()
					cmd.Stderr = writer
				}
				err := cmd.Run()
				if ctx.Err() != nil {
					t.Fatalf("runtime timed out: %v", ctx.Err())
				}
				if _, ok := err.(*exec.ExitError); !ok {
					t.Fatalf("runtime error=%v; want exit %d", err, fixture.code)
				}
				if code := cmd.ProcessState.ExitCode(); code != fixture.code {
					t.Errorf("runtime status=%v; want numeric exit %d", cmd.ProcessState, fixture.code)
				}
				if stdout.Len() != 0 {
					t.Errorf("unexpected stdout %q", stdout.String())
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Errorf("target ran: stat=%v", err)
				}
				if fixture.name != "direct missing" && fixture.name != "direct ENOEXEC" && bytes.Contains(stderr.Bytes(), []byte(fallbackNotice)) {
					t.Errorf("unexpected fallback notice: %q", stderr.String())
				}
				if descriptor == "open" {
					if stderr.Len() == 0 {
						t.Error("runtime diagnostic missing")
					}
					if fixture.diagnostic != "" && stderr.String() != fixture.diagnostic {
						t.Errorf("usage diagnostic %q, want %q", stderr.String(), fixture.diagnostic)
					}
				}
			})
		}
	}
}

func TestRuntimeDiagnosticHarness(t *testing.T) {
	fixture := os.Getenv("TRYSUDO_DIAGNOSTIC_TEST")
	if fixture == "" {
		return
	}
	if signal.Ignored(syscall.SIGPIPE) {
		os.Exit(91)
	}
	if os.Getenv("TRYSUDO_DIAGNOSTIC_DESCRIPTOR") == "closed" {
		if err := syscall.Close(2); err != nil {
			os.Exit(92)
		}
	}
	options := cliOptions{command: []string{"/bin/sh", "-c", `printf executed > "$1"`, "sh", os.Getenv("TRYSUDO_DIAGNOSTIC_MARKER")}}
	creds := credentials{uid: 1000, euid: 1000, gid: 1000, egid: 1000}
	switch fixture {
	case "usage no command":
		os.Exit(runWithCredentials(nil, os.Stdout, os.Stderr, creds))
	case "usage invalid option":
		os.Exit(runWithCredentials(append([]string{"--invalid"}, options.command...), os.Stdout, os.Stderr, creds))
	case "credentials":
		creds.uid++
		os.Exit(runCommand(options, creds, os.Stderr))
	case "discovery":
		os.Exit(runCommand(options, creds, os.Stderr))
	case "preflight internal":
		os.Exit(runAfterPreflight(options, "/unused/sudo", preflightResult{kind: preflightInternalError, err: syscall.EIO}, os.Stderr))
	case "unknown result":
		os.Exit(runAfterPreflight(options, "/unused/sudo", preflightResult{kind: preflightKind(255)}, os.Stderr))
	case "direct missing", "direct ENOEXEC":
		// Select ordinary sudo absence so the required notice precedes the
		// direct launch diagnostic; neither write may replace its status.
		os.Setenv("PATH", "")
		name := "missing-target"
		if fixture == "direct ENOEXEC" {
			name = "unexecutable-script"
		}
		options.command = []string{filepath.Join(filepath.Dir(os.Getenv("TRYSUDO_DIAGNOSTIC_MARKER")), name)}
		os.Exit(runCommand(options, creds, os.Stderr))
	case "final sudo exec":
		path := filepath.Join(filepath.Dir(os.Getenv("TRYSUDO_DIAGNOSTIC_MARKER")), "missing-sudo")
		os.Exit(runAfterPreflight(options, path, preflightResult{kind: preflightSuccess}, os.Stderr))
	default:
		os.Exit(93)
	}
}
