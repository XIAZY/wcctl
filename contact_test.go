package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunContactListJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	databasePath := filepath.Join(root, "contact", "contact.db")
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasePath, []byte("0123456789abcdefdatabase"), 0o600); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, "keys.json")
	store := keyStore{Users: map[string]storedUser{
		"user-a": {Databases: map[string]storedDatabase{
			databasePath: {AESKey: hex.EncodeToString(make([]byte, 32))},
		}},
	}}
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	originalQuery := querySQLCipher
	querySQLCipher = func(path string, aesKey []byte, statement string, immutable bool) ([]map[string]any, error) {
		return []map[string]any{{
			"id":         int64(7),
			"username":   "wxid_test",
			"local_type": int64(1),
			"alias":      "tester",
			"nick_name":  "Test User",
			"remark":     "Friend",
		}}, nil
	}
	t.Cleanup(func() { querySQLCipher = originalQuery })

	var output bytes.Buffer
	if err := runContact([]string{"ls", "-keys", keyPath, "-json"}, &output); err != nil {
		t.Fatal(err)
	}
	var contacts []contactRecord
	if err := json.Unmarshal(output.Bytes(), &contacts); err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 || contacts[0].Username != "wxid_test" || contacts[0].Remark != "Friend" {
		t.Fatalf("unexpected contacts: %#v", contacts)
	}
}

func TestRunContactWithoutSubcommandPrintsUsage(t *testing.T) {
	var output bytes.Buffer
	if err := runContact(nil, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "wcctl contact <subcommand>") || !strings.Contains(got, "ls  list regular contacts") {
		t.Fatalf("unexpected contact usage:\n%s", got)
	}
}

func TestSelectStoredUserRequiresChoiceForMultipleUsers(t *testing.T) {
	store := keyStore{Users: map[string]storedUser{
		"user-b": {},
		"user-a": {},
	}}
	_, err := resolveStoredUser(store, "", "")
	if err == nil || !strings.Contains(err.Error(), "user-a, user-b") {
		t.Fatalf("unexpected error: %v", err)
	}
	selection, err := resolveStoredUser(store, "user-b", "")
	if err != nil || selection.Name != "user-b" {
		t.Fatalf("selected %q with error %v", selection.Name, err)
	}
}

func TestPrintContactTableUsesRemarkAsDisplayName(t *testing.T) {
	var output bytes.Buffer
	err := printContactTable(&output, []contactRecord{{
		ID:           7,
		Username:     "wxid_test",
		Alias:        "tester",
		NickName:     "Test User",
		Remark:       "Friend\nName",
		LocalType:    1,
		Description:  "line one\nline two",
		IsInChatRoom: 2,
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "Friend Name") || !strings.Contains(got, "line one line two") {
		t.Fatalf("unexpected table output:\n%s", got)
	}
}

func TestContactQueryFiltersToRegularContacts(t *testing.T) {
	query := contactSelect
	for _, condition := range []string{
		"(COALESCE(local_type, 0) & 1) != 0",
		"COALESCE(delete_flag, 0) = 0",
		"COALESCE(verify_flag, 0) = 0",
		"NOT LIKE '%@chatroom'",
		"'notifymessage'",
		"'filehelper'",
	} {
		if !strings.Contains(query, condition) {
			t.Fatalf("contact query is missing filter %q", condition)
		}
	}
}
