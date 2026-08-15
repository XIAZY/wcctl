package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureLicenseAcceptancePromptsAndPersistsPrivateConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".wcctl", "config.json")
	if err := saveAppConfig(path, appConfig{DefaultUser: "user-a"}); err != nil {
		t.Fatal(err)
	}
	var prompt bytes.Buffer
	if err := ensureLicenseAcceptance(path, strings.NewReader("yes\n"), &prompt); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"wcctl license",
		"read and agree",
		"not physically located",
		"lawful access",
		"solely for computational data analysis",
	} {
		if !strings.Contains(prompt.String(), statement) {
			t.Fatalf("prompt is missing %q:\n%s", statement, prompt.String())
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config appConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if !config.LicenseAccepted || config.LicenseAcceptedAt.IsZero() || config.DefaultUser != "user-a" {
		t.Fatalf("acceptance not persisted: %#v", config)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config permissions = %o, want 600", got)
	}
	directoryInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config directory permissions = %o, want 700", got)
	}
}

func TestEnsureLicenseAcceptanceSkipsPromptOnceAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveAppConfig(path, appConfig{LicenseAccepted: true}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ensureLicenseAcceptance(path, strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("unexpected repeat prompt: %q", output.String())
	}
}

func TestEnsureLicenseAcceptanceDeclineDoesNotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".wcctl", "config.json")
	err := ensureLicenseAcceptance(path, strings.NewReader("no\n"), &bytes.Buffer{})
	if !errors.Is(err, errLicenseNotAccepted) {
		t.Fatalf("error = %v, want errLicenseNotAccepted", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config should not exist after decline; stat error = %v", err)
	}
}

func TestEnsureLicenseAcceptanceRejectsMalformedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := ensureLicenseAcceptance(path, strings.NewReader("yes\n"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("unexpected error: %v", err)
	}
}
