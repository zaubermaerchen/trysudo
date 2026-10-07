package main

// This file checks notification teardown and bounded cleanup without OS scheduling races.

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestPreflightSignalTeardown(t *testing.T) {
	for _, startFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "after wait", true: "after start failure"}[startFails], func(t *testing.T) {
			notifications := make(chan os.Signal, 4)
			stopped := false
			process := preflightProcess{start: func() error {
				if startFails {
					return &os.PathError{Op: "fork/exec", Err: syscall.ENOENT}
				}
				return nil
			}, wait: func() error { return nil }}
			got := runPreflightProcess(process, notifications, func() { stopped = true; notifications <- syscall.SIGTERM; notifications <- syscall.SIGINT }, time.Second, time.Second)
			if !stopped || len(notifications) != 0 || got.kind != preflightInterrupted || got.signal != syscall.SIGTERM {
				t.Fatalf("stopped=%v queued=%d result=%+v", stopped, len(notifications), got)
			}
		})
	}
}

func TestPreflightInterruptedCleanup(t *testing.T) {
	for _, completes := range []bool{false, true} {
		t.Run(map[bool]string{false: "bounded after failed signal and kill", true: "completed Wait with error"}[completes], func(t *testing.T) {
			notifications := make(chan os.Signal, 4)
			notifications <- syscall.SIGHUP
			notifications <- syscall.SIGTERM
			waitDone := make(chan error, 1)
			t.Cleanup(func() { close(waitDone) })
			waiterExited := make(chan struct{})
			var forwarded []os.Signal
			killed := false
			stopped := false
			process := preflightProcess{
				start: func() error { return nil },
				wait:  func() error { defer close(waiterExited); return <-waitDone },
				signal: func(s os.Signal) error {
					forwarded = append(forwarded, s)
					if completes {
						waitDone <- errors.New("Wait failure")
					}
					return errors.New("forward failed")
				},
				kill: func() error { killed = true; return errors.New("kill failed") },
			}
			began := time.Now()
			grace := 5 * time.Millisecond
			if completes {
				// Allow the Wait goroutine to be scheduled even on a busy race builder.
				grace = time.Second
			}
			got := runPreflightProcess(process, notifications, func() { stopped = true; notifications <- syscall.SIGQUIT }, grace, 5*time.Millisecond)
			if got.kind != preflightInterrupted || got.signal != syscall.SIGHUP || !stopped || len(notifications) != 0 || len(forwarded) != 1 || forwarded[0] != syscall.SIGHUP {
				t.Fatalf("result=%+v stopped=%v forward=%v queued=%d", got, stopped, forwarded, len(notifications))
			}
			if killed == completes {
				t.Errorf("killed=%v completed=%v", killed, completes)
			}
			if time.Since(began) > time.Second {
				t.Error("cleanup exceeded finite bound")
			}
			if !completes {
				waitDone <- nil
			}
			select {
			case <-waiterExited:
			case <-time.After(time.Second):
				t.Fatal("Wait did not return after release")
			}
		})
	}
}
