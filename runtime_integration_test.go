package main

// This file tests runtime path selection and process replacement with fake sudo and targets.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

type runtimeEvent struct {
	Stage                 string
	Args                  []string
	PID                   int
	StdinNull, StdoutNull bool
}

type runtimeTargetOutput struct {
	Args         []string
	Input, Value string
	PID          int
}

func TestRuntimePathSelection(t *testing.T) {
	dir := t.TempDir()
	targetSource := filepath.Join(dir, "target.go")
	writeTestFile(t, targetSource, []byte(`package main
import("encoding/json"; "fmt"; "io"; "os"; "strconv")
func main() {
 f,e:=os.OpenFile(os.Getenv("TRYSUDO_RUNTIME_RECORD"),os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600); if e!=nil {panic(e)}
 json.NewEncoder(f).Encode(struct{Stage string; Args []string; PID int}{"target",os.Args,os.Getpid()}); f.Close()
 input,e:=io.ReadAll(os.Stdin); if e!=nil {panic(e)}
 fmt.Fprintln(os.Stderr,"target stderr")
 json.NewEncoder(os.Stdout).Encode(struct{Args []string; Input,Value string; PID int}{os.Args,string(input),os.Getenv("TRYSUDO_RUNTIME_VALUE"),os.Getpid()})
 code,_:=strconv.Atoi(os.Getenv("TRYSUDO_RUNTIME_EXIT")); os.Exit(code)
}
`), 0600)
	target := filepath.Join(dir, "target")
	buildTestBinary(t, target, targetSource)
	sudoSource := filepath.Join(dir, "sudo.go")
	writeTestFile(t, sudoSource, []byte(`package main
import("encoding/json"; "fmt"; "os"; "os/signal"; "syscall")
func main() {
 stage:="final"; index:=0
 for i,arg:=range os.Args { if arg=="-l" {stage="preflight"}; if arg=="--" {index=i+1; break} }
 null,_:=os.Stat(os.DevNull); stdin,_:=os.Stdin.Stat(); stdout,_:=os.Stdout.Stat()
 f,e:=os.OpenFile(os.Getenv("TRYSUDO_RUNTIME_RECORD"),os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600); if e!=nil {panic(e)}
 json.NewEncoder(f).Encode(struct{Stage string; Args []string; PID int; StdinNull,StdoutNull bool}{stage,os.Args,os.Getpid(),os.SameFile(null,stdin),os.SameFile(null,stdout)}); f.Close()
 mode:=os.Getenv("TRYSUDO_RUNTIME_BEHAVIOR")
 if stage=="preflight" {
  switch mode {
  case "failure1":os.Exit(1)
  case "failure130":os.Exit(130)
  case "remove":if e:=os.Remove(os.Args[0]); e!=nil {panic(e)}
  case "signal":syscall.Kill(os.Getpid(),syscall.SIGKILL); select{}
  case "hold":
   c:=make(chan os.Signal,4); signal.Notify(c,syscall.SIGINT,syscall.SIGTERM,syscall.SIGHUP,syscall.SIGQUIT)
   fmt.Fprintf(os.Stderr,"ready %d\n",os.Getpid()); <-c; os.Exit(0)
  }
  os.Exit(0)
 }
 if mode=="authfail" {os.Exit(31)}
 args:=os.Args[index:]
 if len(args)==0 {panic("no target")}
 if e:=syscall.Exec(os.Getenv("TRYSUDO_RUNTIME_TARGET"),args,os.Environ()); e!=nil {panic(e)}
}
`), 0600)
	sudoBinary := filepath.Join(dir, "fake-sudo")
	buildTestBinary(t, sudoBinary, sudoSource)
	sudoBytes, err := os.ReadFile(sudoBinary)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, mode, availability, credential       string
		prefix                                     []string
		code, preflights, finals, targets, notices int
	}{
		{name: "root direct", availability: "present", credential: "root", code: 37, targets: 1},
		{name: "root bypasses discovery error", availability: "loop", credential: "root", code: 37, targets: 1},
		{name: "sudo missing", availability: "missing", code: 37, targets: 1, notices: 1},
		{name: "sudo missing noninteractive", availability: "missing", prefix: []string{"-n"}, code: 37, targets: 1, notices: 1},
		{name: "relative sudo rejected", availability: "relative", code: 37, targets: 1, notices: 1},
		{name: "sudo discovery loop aborts", availability: "loop", code: 1},
		{name: "preflight ordinary failure", mode: "failure1", code: 37, preflights: 1, targets: 1, notices: 1},
		{name: "preflight exit130 is ordinary", mode: "failure130", prefix: []string{"-n"}, code: 37, preflights: 1, targets: 1, notices: 1},
		{name: "preflight launch failure", availability: "badinterpreter", code: 37, targets: 1, notices: 1},
		{name: "committed sudo authentication failure", mode: "authfail", code: 31, preflights: 1, finals: 1},
		{name: "committed sudo exec failure", mode: "remove", code: 126, preflights: 1},
		{name: "child signal aborts", mode: "signal", code: 137, preflights: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			work := t.TempDir()
			sudo := filepath.Join(work, "sudo")
			record := filepath.Join(work, "events")
			writeTestFile(t, sudo, sudoBytes, 0700)
			path := work
			switch tt.availability {
			case "missing":
				path = ""
			case "relative":
				path = "."
			case "loop":
				if err := os.Remove(sudo); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("sudo", sudo); err != nil {
					t.Fatal(err)
				}
			case "badinterpreter":
				writeTestFile(t, sudo, []byte("#!/trysudo-test-missing-interpreter\n"), 0700)
			}
			args := append(append([]string{}, tt.prefix...), target, "a b", "", "日本語")
			env := runtimeEnvironment(record, target, path, tt.mode, tt.credential)
			stdout, stderr, code, pid := runRuntimeCLI(t, exe, work, env, "input 日本語\n", args...)
			if code != tt.code {
				t.Fatalf("exit %d, want %d; stdout %q stderr %q", code, tt.code, stdout, stderr)
			}
			if got := strings.Count(stderr, "target stderr\n"); got != tt.targets {
				t.Errorf("target diagnostics %d, want %d: %q", got, tt.targets, stderr)
			}
			if got := strings.Count(stderr, "running directly"); got != tt.notices {
				t.Errorf("fallback notices %d, want %d: %q", got, tt.notices, stderr)
			}
			events := readRuntimeEvents(t, record)
			assertRuntimeCounts(t, events, tt.preflights, tt.finals, tt.targets)
			if tt.targets != 0 {
				var got runtimeTargetOutput
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatal(err)
				}
				if got.PID != pid || got.Input != "input 日本語\n" || got.Value != "inherited 日本語" {
					t.Errorf("target %+v, parent PID %d", got, pid)
				}
			} else if stdout != "" {
				t.Errorf("unexpected target output %q", stdout)
			}
		})
	}
	for _, prefix := range [][]string{nil, {"-n"}, {"--non-interactive"}} {
		t.Run("sudo committed argv "+strings.Join(prefix, " "), func(t *testing.T) {
			work := t.TempDir()
			sudo := filepath.Join(work, "sudo")
			record := filepath.Join(work, "events")
			writeTestFile(t, sudo, sudoBytes, 0700)
			// This name deliberately cannot resolve in trysudo's PATH. The fake sudo
			// resolves it using its own environment, exercising sudo-owned resolution.
			command := []string{"sudo-owned-target", "", "a b", "日本語", "-n", "--", "$HOME", "a|b", "$(exit 9)", "; exit 9"}
			stdout, stderr, code, pid := runRuntimeCLI(t, exe, work, runtimeEnvironment(record, target, work, "", ""), "input 日本語\n", append(append([]string{}, prefix...), command...)...)
			if code != 37 || stderr != "target stderr\n" {
				t.Fatalf("exit %d stderr %q", code, stderr)
			}
			events := readRuntimeEvents(t, record)
			assertRuntimeCounts(t, events, 1, 1, 1)
			preflightArgs := []string{sudo}
			finalArgs := []string{sudo}
			if len(prefix) != 0 {
				preflightArgs = append(preflightArgs, "-n")
				finalArgs = append(finalArgs, "-n")
			}
			preflightArgs = append(preflightArgs, "-l", "-u", "root", "--")
			preflightArgs = append(preflightArgs, command...)
			finalArgs = append(finalArgs, "-u", "root", "--")
			finalArgs = append(finalArgs, command...)
			if !reflect.DeepEqual(events[0].Args, preflightArgs) || !events[0].StdinNull || !events[0].StdoutNull {
				t.Errorf("preflight %+v, want args %q", events[0], preflightArgs)
			}
			if !reflect.DeepEqual(events[1].Args, finalArgs) || events[1].PID != pid || events[1].StdinNull || events[1].StdoutNull {
				t.Errorf("final %+v, want args %q, PID %d, inherited descriptors", events[1], finalArgs, pid)
			}
			var got runtimeTargetOutput
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Args, command) || got.PID != pid || got.Input != "input 日本語\n" || got.Value != "inherited 日本語" {
				t.Errorf("target %+v", got)
			}
		})
	}
	t.Run("retained sudo after PATH changes", func(t *testing.T) {
		work := t.TempDir()
		other := t.TempDir()
		sudo := filepath.Join(work, "sudo")
		record := filepath.Join(work, "events")
		writeTestFile(t, sudo, sudoBytes, 0700)
		writeTestFile(t, filepath.Join(other, "sudo"), []byte("#!/trysudo-unusable-alternate\n"), 0700)
		env := runtimeEnvironment(record, target, work, "", "")
		env = append(env, "TRYSUDO_RUNTIME_NEW_PATH="+other)
		_, stderr, code, _ := runRuntimeCLI(t, exe, work, env, "", target)
		if code != 37 || stderr != "target stderr\n" {
			t.Fatalf("exit %d stderr %q", code, stderr)
		}
		events := readRuntimeEvents(t, record)
		assertRuntimeCounts(t, events, 1, 1, 1)
		if events[0].Args[0] != sudo || events[1].Args[0] != sudo {
			t.Errorf("discovery/final path changed: %+v", events)
		}
	})
	t.Run("fallback target missing remains127", func(t *testing.T) {
		work := t.TempDir()
		record := filepath.Join(work, "events")
		_, stderr, code, _ := runRuntimeCLI(t, exe, work, runtimeEnvironment(record, target, "", "", ""), "", "missing-target")
		if code != 127 || strings.Count(stderr, "running directly") != 1 {
			t.Fatalf("exit %d stderr %q", code, stderr)
		}
		assertRuntimeCounts(t, readRuntimeEvents(t, record), 0, 0, 0)
	})
	t.Run("compiled CLI reads actual credentials", func(t *testing.T) {
		if os.Getuid() != os.Geteuid() || os.Getgid() != os.Getegid() {
			t.Skip("host has unsupported credential mismatch")
		}
		work := t.TempDir()
		record := filepath.Join(work, "events")
		writeTestFile(t, filepath.Join(work, "sudo"), sudoBytes, 0700)
		cli := filepath.Join(work, "trysudo")
		buildTestBinary(t, cli, ".")
		stdout, stderr, code, pid := runTestCLI(t, cli, work, runtimeEnvironment(record, target, work, "", ""), "actual CLI input\n", target)
		if code != 37 || stderr != "target stderr\n" {
			t.Fatalf("exit %d stderr %q", code, stderr)
		}
		sudoCount := 1
		if os.Geteuid() == 0 {
			sudoCount = 0
		}
		assertRuntimeCounts(t, readRuntimeEvents(t, record), sudoCount, sudoCount, 1)
		var got runtimeTargetOutput
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatal(err)
		}
		if got.PID != pid || got.Input != "actual CLI input\n" {
			t.Errorf("target %+v parent PID %d", got, pid)
		}
	})

	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT} {
		t.Run("parent interruption "+sig.String(), func(t *testing.T) {
			work := t.TempDir()
			record := filepath.Join(work, "events")
			writeTestFile(t, filepath.Join(work, "sudo"), sudoBytes, 0700)
			runRuntimeSignal(t, exe, work, runtimeEnvironment(record, target, work, "hold", ""), record, target, sig)
			assertRuntimeCounts(t, readRuntimeEvents(t, record), 1, 0, 0)
		})
	}
}

func runtimeEnvironment(record, target, path, behavior, credential string) []string {
	return testEnvironment(map[string]string{
		"TRYSUDO_RUNTIME_HARNESS": "1", "TRYSUDO_RUNTIME_RECORD": record,
		"TRYSUDO_RUNTIME_TARGET": target, "PATH": path, "TRYSUDO_RUNTIME_BEHAVIOR": behavior,
		"TRYSUDO_RUNTIME_CREDENTIAL": credential, "TRYSUDO_RUNTIME_VALUE": "inherited 日本語",
		"TRYSUDO_RUNTIME_EXIT": "37",
	})
}

func runRuntimeCLI(t *testing.T, exe, work string, env []string, input string, args ...string) (string, string, int, int) {
	t.Helper()
	return runTestCLI(t, exe, work, env, input, append([]string{"-test.run=^TestRuntimeHarness$", "--"}, args...)...)
}

func TestRuntimeHarness(t *testing.T) {
	if os.Getenv("TRYSUDO_RUNTIME_HARNESS") != "1" {
		return
	}
	creds := credentials{uid: 1000, euid: 1000, gid: 1000, egid: 1000}
	if os.Getenv("TRYSUDO_RUNTIME_CREDENTIAL") == "root" {
		creds = credentials{}
	}
	args := os.Args[3:]
	if path := os.Getenv("TRYSUDO_RUNTIME_NEW_PATH"); path != "" {
		options, err := parseCLI(args)
		if err != nil {
			panic(err)
		}
		sudo, err := discoverSudo()
		if err != nil {
			panic(err)
		}
		result := runPreflight(sudo, options.command, options.nonInteractive)
		if err := os.Setenv("PATH", path); err != nil {
			panic(err)
		}
		os.Exit(runAfterPreflight(options, sudo, result, os.Stderr))
	}
	os.Exit(runWithCredentials(args, os.Stdout, os.Stderr, creds))
}

func readRuntimeEvents(t *testing.T, path string) []runtimeEvent {
	t.Helper()
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var events []runtimeEvent
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event runtimeEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func assertRuntimeCounts(t *testing.T, events []runtimeEvent, preflights, finals, targets int) {
	t.Helper()
	counts := map[string]int{}
	for _, e := range events {
		counts[e.Stage]++
	}
	if counts["preflight"] != preflights || counts["final"] != finals || counts["target"] != targets {
		t.Fatalf("events %+v, want preflight %d final %d target %d", events, preflights, finals, targets)
	}
}

func runRuntimeSignal(t *testing.T, exe, work string, env []string, record, target string, sig syscall.Signal) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestRuntimeHarness$", "--", target)
	cmd.Env, cmd.Dir = env, work
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		writer.Close()
		t.Fatal(err)
	}
	writer.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	childPID := 0
	complete := false
	t.Cleanup(func() {
		// The child records its PID before announcing readiness, so even a timeout
		// while waiting for the announcement cannot orphan an already-started sudo.
		if childPID == 0 {
			if file, err := os.Open(record); err == nil {
				decoder := json.NewDecoder(file)
				for {
					var event runtimeEvent
					if err := decoder.Decode(&event); err != nil {
						break
					}
					if event.Stage == "preflight" {
						childPID = event.PID
					}
				}
				file.Close()
			}
		}
		if childPID != 0 {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
		}
		_ = cmd.Process.Kill()
		if !complete {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("runtime harness cleanup timed out")
			}
		}
	})
	ready := make(chan string, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		r := bufio.NewReader(reader)
		line, _ := r.ReadString('\n')
		ready <- line
		_, _ = io.Copy(&stderr, r)
	}()
	select {
	case line := <-ready:
		if _, err := fmt.Sscanf(line, "ready %d\n", &childPID); err != nil {
			t.Fatalf("readiness %q: %v", line, err)
		}
	case <-ctx.Done():
		t.Fatal("preflight readiness timed out")
	}
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		complete = true
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 128+int(sig) {
			t.Fatalf("parent interruption status %v, want %d", err, 128+int(sig))
		}
	case <-ctx.Done():
		t.Fatal("runtime did not abort after parent interruption")
	}
	select {
	case <-readDone:
	case <-ctx.Done():
		t.Fatal("runtime diagnostics did not close")
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), "running directly") {
		t.Errorf("interruption output stdout %q stderr %q", stdout.String(), stderr.String())
	}
	if syscall.Kill(childPID, 0) == syscall.ESRCH {
		childPID = 0
	}
}
