package main

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestFreezeProcessTreeAbortsOnFirstFailure(t *testing.T) {
	originalLog := logOut
	logOut = io.Discard
	t.Cleanup(func() { logOut = originalLog })

	stopFailure := errors.New("stop failed")
	var signaled []int
	stopped, err := freezeProcessTree([]int{10, 11, 12}, func(pid int, signal syscall.Signal) error {
		if signal != syscall.SIGSTOP {
			t.Fatalf("signal = %v, want SIGSTOP", signal)
		}
		signaled = append(signaled, pid)
		if pid == 11 {
			return stopFailure
		}
		return nil
	})
	if !errors.Is(err, stopFailure) {
		t.Fatalf("error = %v, want %v", err, stopFailure)
	}
	if !reflect.DeepEqual(signaled, []int{10, 11}) {
		t.Fatalf("signaled PIDs = %v, want [10 11]", signaled)
	}
	if !reflect.DeepEqual(stopped, []int{10}) {
		t.Fatalf("stopped PIDs = %v, want [10]", stopped)
	}
}

func TestFreezeProcessTreeStopsEveryProcessBeforeSuccess(t *testing.T) {
	originalLog := logOut
	logOut = io.Discard
	t.Cleanup(func() { logOut = originalLog })

	var signaled []int
	stopped, err := freezeProcessTree([]int{20, 21, 22}, func(pid int, signal syscall.Signal) error {
		signaled = append(signaled, pid)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(signaled, []int{20, 21, 22}) || !reflect.DeepEqual(stopped, signaled) {
		t.Fatalf("signaled = %v, stopped = %v", signaled, stopped)
	}
}

func TestReadRegionReturnsSegmentCreationFailure(t *testing.T) {
	reg := &region{Start: 0x1000, Size: 4096}
	metadata := &regionMeta{}
	err := readRegion(0, reg, t.TempDir()+"/missing", make([]byte, 4096), metadata)
	if err == nil || !strings.Contains(err.Error(), "create segment") {
		t.Fatalf("unexpected error: %v", err)
	}
}
