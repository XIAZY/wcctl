package main

import (
	"errors"
	"strings"
	"testing"
)

func TestRequireSIPDisabled(t *testing.T) {
	original := readSIPStatus
	t.Cleanup(func() { readSIPStatus = original })
	readSIPStatus = func() ([]byte, error) {
		return []byte("System Integrity Protection status: disabled."), nil
	}
	status, err := requireSIPDisabled()
	if err != nil || !strings.Contains(status, "disabled") {
		t.Fatalf("status %q, error %v", status, err)
	}
}

func TestRequireSIPDisabledRejectsEnabledAndCommandFailure(t *testing.T) {
	original := readSIPStatus
	t.Cleanup(func() { readSIPStatus = original })
	readSIPStatus = func() ([]byte, error) {
		return []byte("System Integrity Protection status: enabled."), nil
	}
	if _, err := requireSIPDisabled(); err == nil || !strings.Contains(err.Error(), "enabled") {
		t.Fatalf("unexpected enabled error: %v", err)
	}
	readSIPStatus = func() ([]byte, error) {
		return []byte("csrutil failed"), errors.New("exit status 1")
	}
	if _, err := requireSIPDisabled(); err == nil || !strings.Contains(err.Error(), "csrutil failed") {
		t.Fatalf("unexpected command error: %v", err)
	}
}

func TestIsWeChatMainProcess(t *testing.T) {
	for _, process := range []processInfo{
		{PID: 1, Name: "WeChat", Path: "/Applications/WeChat.app/Contents/MacOS/WeChat"},
		{PID: 2, Name: "anything", Path: "/private/var/AppTranslocation/WeChat.app/Contents/MacOS/WeChat"},
	} {
		if !isWeChatMainProcess(process) {
			t.Fatalf("main process rejected: %#v", process)
		}
	}
	for _, process := range []processInfo{
		{PID: 3, Name: "WeChat", Path: ""},
		{PID: 4, Name: "WeChat", Path: "/tmp/WeChat"},
		{PID: 5, Name: "WeChatAppEx", Path: "/Applications/WeChat.app/Contents/MacOS/WeChatAppEx.app/Contents/MacOS/WeChatAppEx"},
		{PID: 6, Name: "WeChatAppEx Helper", Path: "/Applications/WeChat.app/Contents/MacOS/WeChatAppEx.app/Contents/Frameworks/WeChatAppEx Helper.app/Contents/MacOS/WeChatAppEx Helper"},
		{PID: 7, Name: "wxutility", Path: "/Applications/WeChat.app/Contents/Frameworks/wxutility"},
	} {
		if isWeChatMainProcess(process) {
			t.Fatalf("non-main process accepted: %#v", process)
		}
	}
}

func TestResolveWeChatProcessAutoDetection(t *testing.T) {
	original := findRunningWeChat
	t.Cleanup(func() { findRunningWeChat = original })
	findRunningWeChat = func() []processInfo {
		return []processInfo{{PID: 42, Name: "WeChat", Path: "/Applications/WeChat.app/Contents/MacOS/WeChat"}}
	}
	process, _, err := resolveWeChatProcess(0)
	if err != nil || process.PID != 42 {
		t.Fatalf("process %#v, error %v", process, err)
	}
}
