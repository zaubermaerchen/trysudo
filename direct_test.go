package main

// This file checks direct execution through isolated CLI subprocesses.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDirectExecution(t *testing.T) {
	dir := t.TempDir()
	cli, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "command.go")
	writeTestFile(t, source, []byte(`package main
import ("encoding/json"; "fmt"; "io"; "os"; "strconv")
func main() {
 input, err := io.ReadAll(os.Stdin); if err != nil { panic(err) }
 fmt.Fprintln(os.Stderr, "command stderr")
 json.NewEncoder(os.Stdout).Encode(struct { Args []string; Env, Input string; PID int }{os.Args, os.Getenv("TRYSUDO_TEST_VALUE"), string(input), os.Getpid()})
 code, _ := strconv.Atoi(os.Getenv("TRYSUDO_TEST_EXIT")); os.Exit(code)
}
`), 0600)
	command := filepath.Join(dir, "command")
	buildTestBinary(t, command, source)
	executable, err := os.ReadFile(command)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("argv environment standard descriptors PID and exit status", func(t *testing.T) {
		args := []string{command, "", "a b", "日本語", "--help", "--version", "-n", "--", "$HOME", "a|b", "*.txt", "$(exit 9)", "; exit 9"}
		for _, prefix := range [][]string{nil, {"-n"}, {"--non-interactive"}, {"-n", "--non-interactive", "--"}} {
			env := testEnvironment(map[string]string{"TRYSUDO_TEST_VALUE": "inherited 日本語", "TRYSUDO_TEST_EXIT": "37"})
			stdout, stderr, code, pid := runRootDirectCLI(t, cli, dir, env, "stdin 日本語\n", append(append([]string{}, prefix...), args...)...)
			if code != 37 {
				t.Fatalf("exit = %d, want 37; stderr %q", code, stderr)
			}
			var got struct {
				Args       []string
				Env, Input string
				PID        int
			}
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("output %q: %v", stdout, err)
			}
			if !reflect.DeepEqual(got.Args, args) || got.Env != "inherited 日本語" || got.Input != "stdin 日本語\n" || got.PID != pid {
				t.Errorf("command observed %+v; want argv %q, environment, stdin and PID %d", got, args, pid)
			}
			if stderr != "command stderr\n" {
				t.Errorf("stderr = %q", stderr)
			}
		}
	})

	t.Run("resolution", func(t *testing.T) {
		work := filepath.Join(dir, "work")
		bin := filepath.Join(work, "bin")
		later := filepath.Join(dir, "later")
		for _, path := range []string{bin, later} {
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		for _, path := range []string{filepath.Join(work, "command"), filepath.Join(bin, "command"), filepath.Join(later, "command"), filepath.Join(work, "--help"), filepath.Join(work, "-")} {
			writeTestFile(t, path, executable, 0700)
		}
		blocked := filepath.Join(dir, "blocked")
		if err := os.Mkdir(blocked, 0700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(blocked, "command"), []byte("unexecutable"), 0600)
		writeTestFile(t, filepath.Join(work, "text"), []byte("exit 0\n"), 0700)
		writeTestFile(t, filepath.Join(work, "interpreter"), []byte("#!/trysudo-test-missing-interpreter\n"), 0700)
		loop := filepath.Join(dir, "loop")
		if err := os.Symlink("loop", loop); err != nil {
			t.Fatal(err)
		}
		tests := []struct {
			name, path, debug string
			args              []string
			code              int
		}{
			{"absolute PATH", bin, "", []string{"command"}, 0},
			{"missing PATH", later, "", []string{"missing"}, 127},
			{"empty PATH", "", "", []string{"command"}, 127},
			{"non executable PATH", blocked, "", []string{"command"}, 126},
			{"later executable wins", blocked + string(os.PathListSeparator) + later, "", []string{"command"}, 0},
			{"relative PATH", "bin", "", []string{"command"}, 126},
			{"relative PATH with ErrDot disabled", "bin", "execerrdot=0", []string{"command"}, 126},
			{"dot PATH", ".", "", []string{"command"}, 126},
			{"dot PATH with ErrDot disabled", ".", "execerrdot=0", []string{"command"}, 126},
			{"empty PATH component", string(os.PathListSeparator) + later, "execerrdot=0", []string{"command"}, 126},
			{"trailing empty PATH component", blocked + string(os.PathListSeparator), "", []string{"command"}, 126},
			{"explicit current path", "", "", []string{"./command"}, 0},
			{"explicit parent path", "", "", []string{"../command"}, 0},
			{"explicit absolute path", "", "", []string{command}, 0},
			{"missing explicit path", "", "", []string{"./missing"}, 127},
			{"not directory", "", "", []string{"./command/child"}, 127},
			{"non executable explicit path", "", "", []string{filepath.Join(blocked, "command")}, 126},
			{"directory", "", "", []string{"./bin"}, 126},
			{"shebangless", "", "", []string{"./text"}, 126},
			{"missing interpreter", "", "", []string{"./interpreter"}, 126},
			{"PATH lookup other error", loop, "", []string{"command"}, 126},
			{"explicit other error", "", "", []string{loop}, 126},
			{"separator permits dash command", work, "", []string{"--", "--help"}, 0},
			{"single dash command", work, "", []string{"-"}, 0},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stdout, stderr, code, _ := runRootDirectCLI(t, cli, work, testEnvironment(map[string]string{"PATH": tt.path, "GODEBUG": tt.debug, "TRYSUDO_TEST_EXIT": "0"}), "", tt.args...)
				if code != tt.code {
					t.Fatalf("exit = %d, want %d; stdout %q; stderr %q", code, tt.code, stdout, stderr)
				}
				if tt.code != 0 {
					if stdout != "" || !strings.HasPrefix(stderr, "trysudo: ") {
						t.Errorf("launch failure stdout %q stderr %q", stdout, stderr)
					}
					return
				}
				var got struct{ Args []string }
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatal(err)
				}
				wantArgs := tt.args
				if wantArgs[0] == "--" {
					wantArgs = wantArgs[1:]
				}
				if !reflect.DeepEqual(got.Args, wantArgs) {
					t.Errorf("argv = %q, want %q", got.Args, wantArgs)
				}
			})
		}
	})
}

func TestDirectInaccessiblePathDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may bypass directory search permissions")
	}
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	visible := filepath.Join(dir, "visible")
	for _, path := range []string{blocked, visible} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Restore permissions before TempDir removes its contents.
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0700); err != nil {
			t.Errorf("restore directory permissions: %v", err)
		}
	})
	if err := os.Chmod(blocked, 0000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(blocked, "missing")); !errors.Is(err, syscall.EACCES) {
		t.Skipf("directory search permission is not enforced: %v", err)
	}
	writeTestFile(t, filepath.Join(visible, "present"), []byte("not executable"), 0600)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		command string
		code    int
	}{{"missing", 127}, {"present", 126}} {
		t.Run(fixture.command, func(t *testing.T) {
			stdout, stderr, code, _ := runRootDirectCLI(t, exe, dir,
				testEnvironment(map[string]string{"PATH": blocked + string(os.PathListSeparator) + visible}), "", fixture.command)
			if code != fixture.code || stdout != "" || !strings.HasPrefix(stderr, "trysudo: ") {
				t.Errorf("code=%d stdout=%q stderr=%q; want %d and a launch diagnostic", code, stdout, stderr, fixture.code)
			}
		})
	}
}

// The direct-path contract must not depend on the CI user's privileges or
// installed sudo policy. The subprocess still replaces itself through exec.
func runRootDirectCLI(t *testing.T, cli, dir string, env []string, input string, args ...string) (string, string, int, int) {
	t.Helper()
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "TRYSUDO_DIRECT_HARNESS=") {
			filtered = append(filtered, entry)
		}
	}
	filtered = append(filtered, "TRYSUDO_DIRECT_HARNESS=1")
	return runTestCLI(t, cli, dir, filtered, input, append([]string{"-test.run=^TestDirectHarness$", "--"}, args...)...)
}

func TestDirectHarness(t *testing.T) {
	if os.Getenv("TRYSUDO_DIRECT_HARNESS") != "1" {
		return
	}
	os.Exit(runWithCredentials(os.Args[3:], os.Stdout, os.Stderr, credentials{}))
}

func buildTestBinary(t *testing.T, output, source string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", output, source)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
}

func writeTestFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func testEnvironment(values map[string]string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replace := values[key]; !replace {
			env = append(env, entry)
		}
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

func runTestCLI(t *testing.T, cli, dir string, env []string, input string, args ...string) (string, string, int, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, env, strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	err := cmd.Wait()
	if ctx.Err() != nil {
		t.Fatalf("CLI timed out: %v", ctx.Err())
	}
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatal(fmt.Errorf("CLI wait: %w", err))
		}
	}
	return stdout.String(), stderr.String(), cmd.ProcessState.ExitCode(), pid
}
