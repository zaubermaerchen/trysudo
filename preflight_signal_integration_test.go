package main

// This file checks real signal delivery in isolated preflight processes.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPreflightSignalIntegration(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "sudo.go")
	writeTestFile(t, source, []byte(`package main
import("fmt"; "os"; "os/signal"; "syscall")
func main() {
 c := make(chan os.Signal, 8)
 signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGUSR1)
 fmt.Fprintf(os.Stderr, "ready %d\n", os.Getpid())
 for s := range c {
  if s == syscall.SIGUSR1 { os.Exit(0) }
  fmt.Fprintf(os.Stderr, "received %d\n", s.(syscall.Signal))
  if os.Getenv("TRYSUDO_SIGNAL_MODE") == "trap" { os.Exit(19) }
 }
}
`), 0600)
	sudo := filepath.Join(dir, "sudo")
	buildTestBinary(t, sudo, source)
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT} {
		t.Run("forward "+sig.String(), func(t *testing.T) {
			p := startPreflightSignalProcess(t, sudo, "trap", "")
			p.send(sig)
			p.line(fmt.Sprintf("received %d", sig))
			got := p.result()
			if got.Kind != preflightInterrupted || got.Signal != sig || got.Error != "" {
				t.Fatalf("trapped child exits normally, but parent interruption must win: %+v", got)
			}
		})
	}
	t.Run("stubborn child is killed and reaped", func(t *testing.T) {
		p := startPreflightSignalProcess(t, sudo, "hold", "")
		p.send(syscall.SIGTERM)
		p.line(fmt.Sprintf("received %d", syscall.SIGTERM))
		got := p.result()
		if got.Kind != preflightInterrupted || got.Signal != syscall.SIGTERM || got.Error != "" {
			t.Fatalf("result %+v", got)
		}
		if err := syscall.Kill(p.childPID, 0); err != syscall.ESRCH {
			t.Fatalf("child %d still exists after bounded cleanup: %v", p.childPID, err)
		}
	})
	t.Run("inherited ignored signals remain ignored", func(t *testing.T) {
		p := startPreflightSignalProcess(t, sudo, "hold", "inherited")
		p.send(syscall.SIGINT)
		p.send(syscall.SIGHUP)
		// Finishing the child, rather than waiting a fixed time, establishes that
		// preflight returns normally despite the ignored signals already sent.
		if err := syscall.Kill(p.childPID, syscall.SIGUSR1); err != nil {
			t.Fatal(err)
		}
		got := p.result()
		if got.Kind != preflightSuccess || !got.Ignored || got.Signal != 0 {
			t.Fatalf("result %+v", got)
		}
	})
	t.Run("all signals ignored does not subscribe to every signal", func(t *testing.T) {
		p := startPreflightSignalProcess(t, sudo, "hold", "allignored")
		// SIGWINCH has default nonfatal behavior, but Notify with an empty signal
		// list would intercept it and incorrectly classify this as interruption.
		p.send(syscall.SIGWINCH)
		if err := syscall.Kill(p.childPID, syscall.SIGUSR1); err != nil {
			t.Fatal(err)
		}
		got := p.result()
		if got.Kind != preflightSuccess || !got.Ignored || got.Signal != 0 {
			t.Fatalf("result %+v", got)
		}
	})
	t.Run("normal return restores default signal delivery", func(t *testing.T) {
		p := startPreflightSignalProcess(t, sudo, "hold", "restore")
		if err := syscall.Kill(p.childPID, syscall.SIGUSR1); err != nil {
			t.Fatal(err)
		}
		p.line("returned")
		p.childReaped = syscall.Kill(p.childPID, 0) == syscall.ESRCH
		p.send(syscall.SIGTERM)
		err := p.wait()
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("after return, SIGTERM should terminate parent: %v", err)
		}
		status, ok := exit.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
			t.Fatalf("parent status: %v", exit)
		}
	})
	t.Run("unobserved child signal remains child termination", func(t *testing.T) {
		p := startPreflightSignalProcess(t, sudo, "hold", "")
		if err := syscall.Kill(p.childPID, syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		got := p.result()
		if got.Kind != preflightChildSignaled || got.Signal != syscall.SIGKILL || got.Error != "" {
			t.Fatalf("result %+v", got)
		}
	})
}

type preflightSignalReport struct {
	Kind    preflightKind
	Signal  syscall.Signal
	Error   string
	Ignored bool
}

func TestPreflightSignalHarness(t *testing.T) {
	if os.Getenv("TRYSUDO_SIGNAL_HARNESS") != "1" {
		return
	}
	disposition := os.Getenv("TRYSUDO_SIGNAL_DISPOSITION")
	if disposition == "inherited" {
		signal.Ignore(syscall.SIGINT, syscall.SIGHUP)
		env := testEnvironment(map[string]string{"TRYSUDO_SIGNAL_DISPOSITION": "inherited-child"})
		exe, err := os.Executable()
		if err != nil {
			panic(err)
		}
		if err := syscall.Exec(exe, os.Args, env); err != nil {
			panic(err)
		}
	}
	ignored := []os.Signal{syscall.SIGINT, syscall.SIGHUP}
	if disposition == "allignored" {
		ignored = append(ignored, syscall.SIGTERM, syscall.SIGQUIT)
		signal.Ignore(ignored...)
	}
	result := runPreflight(os.Getenv("TRYSUDO_SIGNAL_SUDO"), []string{"target"}, false)
	report := preflightSignalReport{Kind: result.kind, Signal: result.signal}
	if result.err != nil {
		report.Error = result.err.Error()
	}
	if disposition == "allignored" || disposition == "inherited-child" {
		report.Ignored = true
		for _, s := range ignored {
			report.Ignored = report.Ignored && signal.Ignored(s)
		}
	}
	if disposition == "restore" {
		fmt.Fprintln(os.Stderr, "returned")
		// Keep this process alive after preflight so its restored default disposition
		// can be tested from the outside without signaling the test runner.
		_, _ = io.Copy(io.Discard, os.Stdin)
	}
	json.NewEncoder(os.Stdout).Encode(report)
	os.Exit(0)
}

type preflightSignalProcess struct {
	t           *testing.T
	cmd         *exec.Cmd
	output      bytes.Buffer
	lines       chan string
	done        chan error
	childPID    int
	childReaped bool
	completed   bool
	waitError   error
}

func startPreflightSignalProcess(t *testing.T, sudo, mode, disposition string) *preflightSignalProcess {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := &preflightSignalProcess{t: t, lines: make(chan string, 32), done: make(chan error, 1)}
	p.cmd = exec.Command(exe, "-test.run=^TestPreflightSignalHarness$")
	p.cmd.Env = testEnvironment(map[string]string{"TRYSUDO_SIGNAL_HARNESS": "1", "TRYSUDO_SIGNAL_SUDO": sudo, "TRYSUDO_SIGNAL_MODE": mode, "TRYSUDO_SIGNAL_DISPOSITION": disposition})
	p.cmd.Stdout = &p.output
	input, err := p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	p.cmd.Stderr = writer
	stopScanner := make(chan struct{})
	go func() {
		defer close(p.lines)
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			select {
			case p.lines <- scanner.Text():
			case <-stopScanner:
				return
			}
		}
	}()
	if err := p.cmd.Start(); err != nil {
		reader.Close()
		writer.Close()
		t.Fatal(err)
	}
	_ = writer.Close()
	go func() { p.done <- p.cmd.Wait() }()
	t.Cleanup(func() {
		close(stopScanner)
		if p.childPID != 0 && !p.childReaped {
			_ = syscall.Kill(p.childPID, syscall.SIGKILL)
		}
		_ = p.cmd.Process.Kill()
		input.Close()
		reader.Close()
		if !p.completed {
			select {
			case <-p.done:
			case <-time.After(2 * time.Second):
				t.Error("preflight harness was not reaped during cleanup")
			}
		}
	})
	line := p.nextLine()
	if !strings.HasPrefix(line, "ready ") {
		t.Fatalf("expected child readiness, got %q", line)
	}
	p.childPID, err = strconv.Atoi(strings.TrimPrefix(line, "ready "))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *preflightSignalProcess) nextLine() string {
	p.t.Helper()
	select {
	case line, ok := <-p.lines:
		if !ok {
			p.t.Fatal("preflight process exited before expected acknowledgment")
		}
		return line
	case err := <-p.done:
		p.completed, p.waitError = true, err
		if err != nil {
			p.t.Fatalf("preflight process terminated before forwarding acknowledgment: %v", err)
		}
		return p.nextLine()
	case <-time.After(10 * time.Second):
		p.t.Fatal("timed out waiting for preflight acknowledgment")
	}
	return ""
}

func (p *preflightSignalProcess) line(want string) {
	p.t.Helper()
	if got := p.nextLine(); got != want {
		p.t.Fatalf("acknowledgment %q, want %q", got, want)
	}
}

func (p *preflightSignalProcess) send(sig syscall.Signal) {
	p.t.Helper()
	if err := p.cmd.Process.Signal(sig); err != nil {
		p.t.Fatal(err)
	}
}

func (p *preflightSignalProcess) wait() error {
	p.t.Helper()
	if p.completed {
		return p.waitError
	}
	select {
	case err := <-p.done:
		p.completed, p.waitError = true, err
		return err
	case <-time.After(10 * time.Second):
		p.t.Fatal("preflight process did not finish within bounded cleanup")
	}
	return nil
}

func (p *preflightSignalProcess) result() preflightSignalReport {
	p.t.Helper()
	if err := p.wait(); err != nil {
		p.t.Fatalf("preflight harness failed: %v", err)
	}
	var result preflightSignalReport
	if err := json.Unmarshal(p.output.Bytes(), &result); err != nil {
		p.t.Fatalf("result %q: %v", p.output.String(), err)
	}
	p.childReaped = syscall.Kill(p.childPID, 0) == syscall.ESRCH
	return result
}
