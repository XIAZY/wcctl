package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunSessionHelp(t *testing.T) {
	var output bytes.Buffer
	if err := runSession([]string{"-h"}, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "wcctl sessions [-limit N] [-user USER] [-json] [-keys PATH]") {
		t.Fatalf("unexpected session usage:\n%s", got)
	}
}

func TestStoredSessionDatabase(t *testing.T) {
	user := storedUser{Databases: map[string]storedDatabase{
		"/db/session/session.db": {AESKey: "key"},
		"/db/message/session.db": {},
	}}
	path, database, err := storedSessionDatabase(user)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/db/session/session.db" || database.AESKey != "key" {
		t.Fatalf("unexpected session database: %q %#v", path, database)
	}
}

func TestSessionQueryIncludesUnreadAndNoContactMetadata(t *testing.T) {
	for _, fragment := range []string{
		"FROM SessionTable AS session",
		"SessionUnreadStatTable_1",
		"SessionUnreadListTable_1",
		"SessionNoContactInfoTable",
		"ORDER BY session.last_timestamp DESC",
	} {
		if !strings.Contains(sessionSelect, fragment) {
			t.Fatalf("session query is missing %q", fragment)
		}
	}
}

func TestPrintSessionTable(t *testing.T) {
	var output bytes.Buffer
	err := printSessionTable(&output, []sessionRecord{{
		Username:              "wxid_test",
		DisplayName:           "Test\nUser",
		UnreadCount:           3,
		LastTimestamp:         1786764461,
		LastSenderDisplayName: "Alice",
		Summary:               "hello\nworld",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "Test User") || !strings.Contains(got, "Alice") || !strings.Contains(got, "hello world") {
		t.Fatalf("unexpected session table:\n%s", got)
	}
}

func TestSessionFallbackName(t *testing.T) {
	if got := sessionFallbackName(sessionRecord{Username: "wxid_test", NoContactTitle: "Room"}); got != "Room" {
		t.Fatalf("fallback name = %q", got)
	}
	if got := sessionFallbackName(sessionRecord{Username: "wxid_test"}); got != "wxid_test" {
		t.Fatalf("fallback username = %q", got)
	}
}
