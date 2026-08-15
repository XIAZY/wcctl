package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

type processInfo struct {
	PID  int
	Name string
	Path string
}

var readSIPStatus = func() ([]byte, error) {
	return exec.Command("/usr/bin/csrutil", "status").CombinedOutput()
}

func sipStatus() (string, error) {
	output, err := readSIPStatus()
	status := strings.TrimSpace(string(output))
	if err != nil {
		if status != "" {
			return status, fmt.Errorf("run /usr/bin/csrutil status: %s", status)
		}
		return status, fmt.Errorf("run /usr/bin/csrutil status: %w", err)
	}
	return status, nil
}

func requireSIPDisabled() (string, error) {
	status, err := sipStatus()
	if err != nil {
		return status, err
	}
	if !strings.Contains(strings.ToLower(status), "status: disabled") {
		return status, fmt.Errorf("System Integrity Protection is enabled; disable SIP before acquiring WeChat memory")
	}
	return status, nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func isWeChatMainProcess(process processInfo) bool {
	name := strings.ToLower(strings.TrimSpace(process.Name))
	base := strings.ToLower(filepath.Base(process.Path))
	if name != "wechat" && base != "wechat" {
		return false
	}
	lowerPath := strings.ToLower(process.Path)
	return lowerPath == "" || strings.Contains(lowerPath, ".app/contents/macos/wechat")
}

var findRunningWeChat = func() []processInfo {
	var processes []processInfo
	for _, pid := range allPIDs() {
		process := processInfo{PID: pid, Name: procName(pid), Path: procPath(pid)}
		if isWeChatMainProcess(process) {
			processes = append(processes, process)
		}
	}
	sort.Slice(processes, func(i, j int) bool { return processes[i].PID < processes[j].PID })
	return processes
}

func resolveWeChatProcess(requestedPID int) (processInfo, []processInfo, error) {
	processes := findRunningWeChat()
	if requestedPID > 0 {
		for _, process := range processes {
			if process.PID == requestedPID {
				return process, processes, nil
			}
		}
		if !processExists(requestedPID) {
			return processInfo{}, processes, fmt.Errorf("process %d is not running", requestedPID)
		}
		return processInfo{}, processes, fmt.Errorf("process %d is not the main WeChat process", requestedPID)
	}
	if len(processes) == 0 {
		return processInfo{}, nil, fmt.Errorf("WeChat is not running; open WeChat, sign in, and try again")
	}
	if len(processes) == 1 {
		return processes[0], processes, nil
	}
	return processInfo{}, processes, nil
}
