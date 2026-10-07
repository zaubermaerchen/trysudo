package main

// This file tests sudo preflight using isolated helpers instead of real policy or authentication.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"syscall"
	"testing"
)

func TestPreflight(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "sudo.go")
	writeTestFile(t, source, []byte(`package main
import ("encoding/json"; "fmt"; "io"; "os"; "os/signal"; "strconv"; "syscall")
func main() {
 if path := os.Getenv("TRYSUDO_PREFLIGHT_RECORD"); path != "" {
  input, err := io.ReadAll(os.Stdin); if err != nil { panic(err) }
  f, err := os.Create(path); if err != nil { panic(err) }
  null, err := os.Stat(os.DevNull); if err != nil { panic(err) }
  stdin, err := os.Stdin.Stat(); if err != nil { panic(err) }
  stdout, err := os.Stdout.Stat(); if err != nil { panic(err) }
  json.NewEncoder(f).Encode(struct { Args []string; Input, Env string; StdinNull, StdoutNull bool }{os.Args, string(input), os.Getenv("TRYSUDO_PREFLIGHT_VALUE"), os.SameFile(null, stdin), os.SameFile(null, stdout)}); f.Close()
  fmt.Fprintln(os.Stdout, "sudo stdout"); fmt.Fprintln(os.Stderr, "sudo diagnostic")
 }
 if n, _ := strconv.Atoi(os.Getenv("TRYSUDO_PREFLIGHT_SIGNAL")); n != 0 {
  sig := syscall.Signal(n); signal.Reset(sig); syscall.Kill(os.Getpid(), sig); select {}
 }
 n, _ := strconv.Atoi(os.Getenv("TRYSUDO_PREFLIGHT_EXIT")); os.Exit(n)
}
`), 0600)
	sudo := filepath.Join(dir, "sudo")
	buildTestBinary(t, sudo, source)
	t.Run("argv and standard descriptors", func(t *testing.T) {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := []string{"missing-target", "", "a b", "日本語", "-n", "--", "$HOME", "a|b", "$(exit 9)", "; exit 9"}
		commandJSON, err := json.Marshal(command)
		if err != nil {
			t.Fatal(err)
		}
		for _, nonInteractive := range []bool{false, true} {
			record := filepath.Join(dir, "record.json")
			stdout, stderr, code, _ := runTestCLI(t, exe, dir, testEnvironment(map[string]string{
				"TRYSUDO_PREFLIGHT_HARNESS": "1", "TRYSUDO_PREFLIGHT_PATH": sudo,
				"TRYSUDO_PREFLIGHT_COMMAND": string(commandJSON), "TRYSUDO_PREFLIGHT_NONINTERACTIVE": strconv.FormatBool(nonInteractive),
				"TRYSUDO_PREFLIGHT_RECORD": record, "TRYSUDO_PREFLIGHT_VALUE": "inherited 日本語", "PATH": "",
			}), "target stdin 日本語\n", "-test.run=^TestPreflightHarness$")
			if code != 0 {
				t.Fatalf("harness exit %d: %s", code, stderr)
			}
			var got struct {
				Kind  preflightKind
				Input string
			}
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("stdout %q: %v", stdout, err)
			}
			if got.Kind != preflightSuccess || got.Input != "target stdin 日本語\n" {
				t.Errorf("harness result %+v", got)
			}
			if stderr != "sudo diagnostic\n" {
				t.Errorf("stderr = %q", stderr)
			}
			data, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			var observed struct {
				Args                  []string
				Input, Env            string
				StdinNull, StdoutNull bool
			}
			if err := json.Unmarshal(data, &observed); err != nil {
				t.Fatal(err)
			}
			args := []string{sudo}
			if nonInteractive {
				args = append(args, "-n")
			}
			args = append(args, "-l", "-u", "root", "--")
			args = append(args, command...)
			if !reflect.DeepEqual(observed.Args, args) || observed.Input != "" || observed.Env != "inherited 日本語" || !observed.StdinNull || !observed.StdoutNull {
				t.Errorf("sudo observed %+v; want argv %q, EOF stdin and inherited environment", observed, args)
			}
		}
	})
	t.Run("normal exit status", func(t *testing.T) {
		for _, code := range []int{0, 1, 17, 130} {
			t.Run(strconv.Itoa(code), func(t *testing.T) {
				t.Setenv("TRYSUDO_PREFLIGHT_EXIT", strconv.Itoa(code))
				got := runPreflight(sudo, []string{"missing-target"}, false)
				want := preflightOrdinaryFailure
				if code == 0 {
					want = preflightSuccess
				}
				if got.kind != want || got.exitCode != code || got.signal != 0 || got.err != nil {
					t.Errorf("result %+v", got)
				}
			})
		}
	})
	t.Run("child signal termination", func(t *testing.T) {
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
			t.Run(sig.String(), func(t *testing.T) {
				t.Setenv("TRYSUDO_PREFLIGHT_SIGNAL", strconv.Itoa(int(sig)))
				got := runPreflight(sudo, []string{"missing-target"}, false)
				if got.kind != preflightChildSignaled || got.signal != sig || got.err != nil {
					t.Errorf("result %+v", got)
				}
			})
		}
	})
	t.Run("launch failure", func(t *testing.T) {
		blocked := filepath.Join(dir, "blocked")
		writeTestFile(t, blocked, []byte("unexecutable"), 0600)
		text := filepath.Join(dir, "text")
		writeTestFile(t, text, []byte("exit 0\n"), 0700)
		for _, path := range []string{filepath.Join(dir, "missing"), blocked, text, filepath.Join(sudo, "child")} {
			got := runPreflight(path, []string{"missing-target"}, false)
			if got.kind != preflightLaunchFailure || got.err == nil {
				t.Errorf("%s: result %+v", path, got)
			}
		}
	})
}

func TestPreflightHarness(t *testing.T) {
	if os.Getenv("TRYSUDO_PREFLIGHT_HARNESS") != "1" {
		return
	}
	var command []string
	if err := json.Unmarshal([]byte(os.Getenv("TRYSUDO_PREFLIGHT_COMMAND")), &command); err != nil {
		panic(err)
	}
	nonInteractive, _ := strconv.ParseBool(os.Getenv("TRYSUDO_PREFLIGHT_NONINTERACTIVE"))
	result := runPreflight(os.Getenv("TRYSUDO_PREFLIGHT_PATH"), command, nonInteractive)
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		panic(err)
	}
	if result.err != nil {
		fmt.Fprintln(os.Stderr, result.err)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(struct {
		Kind  preflightKind
		Input string
	}{result.kind, string(input)})
	os.Exit(0)
}

func TestPreflightStartError(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EMFILE, syscall.ENFILE, syscall.ENOMEM, syscall.EAGAIN} {
		err := &os.PathError{Op: "fork/exec", Path: "sudo", Err: errno}
		got := preflightStartError(err)
		if got.kind != preflightInternalError || !errors.Is(got.err, errno) {
			t.Errorf("%v: %+v", errno, got)
		}
	}
}

func TestPreflightInternalError(t *testing.T) {
	waitErr := errors.New("unexpected wait failure")
	got := preflightWaitResult(waitErr)
	if got.kind != preflightInternalError || !errors.Is(got.err, waitErr) {
		t.Errorf("wait result %+v", got)
	}
	setupErr := &os.PathError{Op: "open", Path: os.DevNull, Err: syscall.EACCES}
	got = preflightStartError(setupErr)
	if got.kind != preflightInternalError || !errors.Is(got.err, setupErr) {
		t.Errorf("descriptor setup result %+v", got)
	}
}
