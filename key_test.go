package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunKeyWithoutSubcommandPrintsUsage(t *testing.T) {
	var output bytes.Buffer
	if err := runKey(nil, strings.NewReader(""), &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"wcctl key <subcommand>", "acquire", "extract"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("usage is missing %q:\n%s", expected, output.String())
		}
	}
}

func TestRunKeyExtractFromCapture(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	accountPath, capturePath, keyPath := createExtractionFixture(t, root)

	var output, errors bytes.Buffer
	err := runKey([]string{
		"extract",
		"-capture", capturePath,
		"-data-dir", accountPath,
		"-keys", keyPath,
	}, strings.NewReader(""), &output, &errors)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errors.String(), "saved 1 verified database key") {
		t.Fatalf("unexpected status output:\n%s", errors.String())
	}
	if strings.Contains(errors.String(), "mask-free salt fallback") {
		t.Fatalf("plaintext extraction unexpectedly used the fallback:\n%s", errors.String())
	}
	assertFixtureKeySaved(t, keyPath)
}

func TestRunKeyExtractFallsBackToMaskedRecord(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	accountPath, capturePath, keyPath := createExtractionFixture(t, root)
	databasePath := filepath.Join(accountPath, "db_storage", "message", "message_0.db")
	page, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x42}, 32)
	masked := testMaskedKeyRecord(key, page[:16])
	data := append([]byte("noise"), masked...)
	data = append(data, []byte("trailing")...)
	if err := os.WriteFile(filepath.Join(capturePath, "segments", "sample.bin"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	var output, errors bytes.Buffer
	err = runKey([]string{
		"extract",
		"-capture", capturePath,
		"-data-dir", accountPath,
		"-keys", keyPath,
	}, strings.NewReader(""), &output, &errors)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errors.String(), "no plaintext x'<96 hex>' AES candidates found") ||
		!strings.Contains(errors.String(), "trying mask-free salt fallback") {
		t.Fatalf("masked extraction did not report the fallback:\n%s", errors.String())
	}
	assertFixtureKeySaved(t, keyPath)
}

func TestRunKeyAcquireOrchestratesCaptureAndCleanup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("key acquire intentionally rejects direct root execution")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	accountPath, sourceCapture, keyPath := createExtractionFixture(t, root)

	originalSIP := readSIPStatus
	originalProcesses := findRunningWeChat
	originalDump := runPrivilegedDump
	t.Cleanup(func() {
		readSIPStatus = originalSIP
		findRunningWeChat = originalProcesses
		runPrivilegedDump = originalDump
	})
	readSIPStatus = func() ([]byte, error) {
		return []byte("System Integrity Protection status: disabled."), nil
	}
	findRunningWeChat = func() []processInfo {
		return []processInfo{{PID: 1234, Name: "WeChat", Path: "/Applications/WeChat.app/Contents/MacOS/WeChat"}}
	}
	var generatedCapture string
	runPrivilegedDump = func(_ string, capturePath string, _, _, _, _ int, _ io.Reader, _ io.Writer) error {
		generatedCapture = capturePath
		if err := os.MkdirAll(filepath.Join(capturePath, "segments"), 0o700); err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(sourceCapture, "segments", "sample.bin"))
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(capturePath, "segments", "sample.bin"), data, 0o600)
	}

	var output, errors bytes.Buffer
	err := runKey([]string{
		"acquire", "-yes",
		"-data-dir", accountPath,
		"-keys", keyPath,
	}, strings.NewReader(""), &output, &errors)
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureKeySaved(t, keyPath)
	if generatedCapture == "" {
		t.Fatal("privileged capture was not invoked")
	}
	if _, err := os.Stat(generatedCapture); !os.IsNotExist(err) {
		t.Fatalf("temporary capture was not removed: %v", err)
	}
	for _, expected := range []string{
		"Re-enable System Integrity Protection now",
		"csrutil enable",
		"Normal wcctl queries work with SIP enabled",
	} {
		if !strings.Contains(errors.String(), expected) {
			t.Fatalf("successful acquisition is missing SIP reminder %q:\n%s", expected, errors.String())
		}
	}
}

func TestKeyAcquireRequiresDisabledSIPBeforeProcessDiscovery(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("key acquire intentionally rejects direct root execution")
	}
	t.Setenv("HOME", t.TempDir())
	originalSIP := readSIPStatus
	originalProcesses := findRunningWeChat
	t.Cleanup(func() {
		readSIPStatus = originalSIP
		findRunningWeChat = originalProcesses
	})
	readSIPStatus = func() ([]byte, error) {
		return []byte("System Integrity Protection status: enabled."), nil
	}
	findRunningWeChat = func() []processInfo {
		t.Fatal("process discovery should not run while SIP is enabled")
		return nil
	}
	err := runKey([]string{"acquire", "-yes"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "System Integrity Protection is enabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestKeyAcquireRequiresRunningWeChat(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("key acquire intentionally rejects direct root execution")
	}
	t.Setenv("HOME", t.TempDir())
	originalSIP := readSIPStatus
	originalProcesses := findRunningWeChat
	t.Cleanup(func() {
		readSIPStatus = originalSIP
		findRunningWeChat = originalProcesses
	})
	readSIPStatus = func() ([]byte, error) {
		return []byte("System Integrity Protection status: disabled."), nil
	}
	findRunningWeChat = func() []processInfo { return nil }
	err := runKey([]string{"acquire", "-yes"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "WeChat is not running") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareCaptureDirectoryIsPrivate(t *testing.T) {
	path, automatic, err := prepareCaptureDirectory(filepath.Join(t.TempDir(), "capture"))
	if err != nil {
		t.Fatal(err)
	}
	if automatic {
		t.Fatal("explicit capture path marked automatic")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("capture permissions = %o, want 700", info.Mode().Perm())
	}
}

func createExtractionFixture(t *testing.T, root string) (accountPath, capturePath, keyPath string) {
	t.Helper()
	accountPath = filepath.Join(root, "wxid_test")
	databasePath := filepath.Join(accountPath, "db_storage", "message", "message_0.db")
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x42}, 32)
	page := authenticatedTestPage(key)
	if err := os.WriteFile(databasePath, page, 0o600); err != nil {
		t.Fatal(err)
	}

	capturePath = filepath.Join(root, "capture")
	if err := os.MkdirAll(filepath.Join(capturePath, "segments"), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := "noise x'" + hex.EncodeToString(key) + hex.EncodeToString(page[:16]) + "' trailing"
	if err := os.WriteFile(filepath.Join(capturePath, "segments", "sample.bin"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	keyPath = filepath.Join(root, ".wcctl", "keys.json")
	return accountPath, capturePath, keyPath
}

func assertFixtureKeySaved(t *testing.T, keyPath string) {
	t.Helper()
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	var store keyStore
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatal(err)
	}
	user, ok := store.Users["wxid_test"]
	if !ok || len(user.Databases) != 1 {
		t.Fatalf("unexpected key store: %#v", store)
	}
	for _, database := range user.Databases {
		if database.AESKey != strings.Repeat("42", 32) {
			t.Fatalf("unexpected AES key: %s", database.AESKey)
		}
	}
}
