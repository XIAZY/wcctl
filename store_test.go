package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveDatabaseKeysCreatesPrivateMergedStore(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".wcctl", "keys.json")
	firstDB := filepath.Join(root, "message_0.db")
	secondDB := filepath.Join(root, "message_1.db")
	thirdDB := filepath.Join(root, "contact.db")

	if err := saveDatabaseKeys(path, map[string]storedDatabase{
		firstDB: newStoredDatabase("user-a", make([]byte, 32)),
	}); err != nil {
		t.Fatal(err)
	}
	if err := saveDatabaseKeys(path, map[string]storedDatabase{
		secondDB: newStoredDatabase("user-b", make([]byte, 32)),
	}); err != nil {
		t.Fatal(err)
	}
	if err := saveDatabaseKeys(path, map[string]storedDatabase{
		thirdDB: newStoredDatabase("user-a", make([]byte, 32)),
	}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("key store permissions = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("key store directory permissions = %o, want 700", got)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var store keyStore
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatal(err)
	}
	if len(store.Users) != 2 {
		t.Fatalf("stored user count = %d, want 2", len(store.Users))
	}
	if len(store.Users["user-a"].Databases) != 2 {
		t.Fatalf("user-a database count = %d, want 2", len(store.Users["user-a"].Databases))
	}
	if len(store.Users["user-b"].Databases) != 1 {
		t.Fatalf("user-b database count = %d, want 1", len(store.Users["user-b"].Databases))
	}
	if _, ok := store.Users["user-a"].Databases[firstDB]; !ok {
		t.Fatalf("user-a databases do not contain %q", firstDB)
	}
	if _, ok := store.Users["user-b"].Databases[secondDB]; !ok {
		t.Fatalf("user-b databases do not contain %q", secondDB)
	}
	if _, ok := store.Users["user-a"].Databases[thirdDB]; !ok {
		t.Fatalf("user-a databases do not contain %q", thirdDB)
	}

	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if _, ok := document["databases"]; ok {
		t.Fatal("key store still contains a top-level databases field")
	}
	if _, ok := document["version"]; ok {
		t.Fatal("key store should not contain a version field")
	}
	if string(data) == "" || containsJSONField(data, "account") {
		t.Fatal("database entries should not repeat the account field")
	}
}

func containsJSONField(data []byte, field string) bool {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return false
	}
	return hasJSONField(value, field)
}

func hasJSONField(value any, field string) bool {
	switch value := value.(type) {
	case map[string]any:
		if _, ok := value[field]; ok {
			return true
		}
		for _, child := range value {
			if hasJSONField(child, field) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if hasJSONField(child, field) {
				return true
			}
		}
	}
	return false
}
