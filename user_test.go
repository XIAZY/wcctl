package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveStoredUserPrecedence(t *testing.T) {
	store := keyStore{Users: map[string]storedUser{
		"user-a": {},
		"user-b": {},
	}}
	selection, err := resolveStoredUser(store, "user-b", "user-a")
	if err != nil || selection.Name != "user-b" || selection.Source != "explicit" {
		t.Fatalf("explicit selection = %#v, error %v", selection, err)
	}
	selection, err = resolveStoredUser(store, "", "user-a")
	if err != nil || selection.Name != "user-a" || selection.Source != "default" {
		t.Fatalf("default selection = %#v, error %v", selection, err)
	}

	only := keyStore{Users: map[string]storedUser{"only-user": {}}}
	selection, err = resolveStoredUser(only, "", "")
	if err != nil || selection.Name != "only-user" || selection.Source != "only" {
		t.Fatalf("only-user selection = %#v, error %v", selection, err)
	}
}

func TestResolveStoredUserRejectsStaleDefault(t *testing.T) {
	store := keyStore{Users: map[string]storedUser{"user-a": {}}}
	_, err := resolveStoredUser(store, "", "missing-user")
	if err == nil || !strings.Contains(err.Error(), "configured default user") {
		t.Fatalf("unexpected stale-default error: %v", err)
	}
}

func TestUserCommandsSetShowAndClearDefault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	keyPath := filepath.Join(root, "keys.json")
	store := keyStore{Users: map[string]storedUser{
		"user-a": {Databases: map[string]storedDatabase{"a.db": {}}},
		"user-b": {Databases: map[string]storedDatabase{"b.db": {}, "c.db": {}}},
	}}
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath, err := defaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveAppConfig(configPath, appConfig{LicenseAccepted: true}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := runUser([]string{"use", "-keys", keyPath, "user-b"}, &output); err != nil {
		t.Fatal(err)
	}
	config, err := readAppConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !config.LicenseAccepted || config.DefaultUser != "user-b" {
		t.Fatalf("unexpected config after user use: %#v", config)
	}

	output.Reset()
	if err := runUser([]string{"current", "-keys", keyPath}, &output); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "user-b" {
		t.Fatalf("current user output = %q", output.String())
	}

	output.Reset()
	if err := runUser([]string{"ls", "-keys", keyPath}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "*") || !strings.Contains(output.String(), "user-b") || !strings.Contains(output.String(), "user-a") {
		t.Fatalf("unexpected user list:\n%s", output.String())
	}

	output.Reset()
	if err := runUser([]string{"clear"}, &output); err != nil {
		t.Fatal(err)
	}
	config, err = readAppConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.DefaultUser != "" || !config.LicenseAccepted {
		t.Fatalf("unexpected config after clear: %#v", config)
	}
}
