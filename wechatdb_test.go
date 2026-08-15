package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPBKDF2SHA512TwoKnownVector(t *testing.T) {
	// PBKDF2-HMAC-SHA512("password", "salt", 2), first 32 bytes.
	want, _ := hex.DecodeString("e1d9c16aa681708a45f5c7c4e215ceb66e011a2e9f0040713f18aefdb866d53c")
	got := pbkdf2SHA512Two([]byte("password"), []byte("salt"), 32)
	if !bytes.Equal(got, want) {
		t.Fatalf("PBKDF2 result = %x, want %x", got, want)
	}
}

func TestValidateWeChatV4AESKey(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	page := authenticatedTestPage(key)

	if !validateWeChatV4AESKey(page, key) {
		t.Fatal("valid AES key was rejected")
	}
	wrong := append([]byte(nil), key...)
	wrong[0] ^= 0xff
	if validateWeChatV4AESKey(page, wrong) {
		t.Fatal("invalid AES key was accepted")
	}
}

func TestFindDatabaseKeyLeavesDatabaseByteIdentical(t *testing.T) {
	validKey := bytes.Repeat([]byte{0x42}, 32)
	page := authenticatedTestPage(validKey)
	// Include additional deterministic bytes so the assertion covers the whole
	// database file, not only the first page read by key validation.
	original := append([]byte(nil), page...)
	for i := 0; i < wechatPageSize*2; i++ {
		original = append(original, byte((i*17+11)%251))
	}

	dbPath := filepath.Join(t.TempDir(), "message_0.db")
	if err := os.WriteFile(dbPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	var wrongCandidate, validCandidate keyCandidate
	copy(wrongCandidate.AESKey[:], bytes.Repeat([]byte{0x99}, 32))
	copy(wrongCandidate.SaltHint[:], page[:16])
	copy(validCandidate.AESKey[:], validKey)
	copy(validCandidate.SaltHint[:], page[:16])

	match, ok, err := findDatabaseKey(dbPath, []keyCandidate{wrongCandidate, validCandidate})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || match.AESKey != validCandidate.AESKey {
		t.Fatal("expected the valid AES candidate to match")
	}

	after, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("database changed during key testing: before sha256=%x after sha256=%x",
			sha256.Sum256(before), sha256.Sum256(after))
	}
}

func authenticatedTestPage(key []byte) []byte {
	page := make([]byte, wechatPageSize)
	for i := range page {
		page[i] = byte((i*31 + 7) % 251)
	}
	macSalt := make([]byte, 16)
	for i := range macSalt {
		macSalt[i] = page[i] ^ 0x3a
	}
	macKey := pbkdf2SHA512Two(key, macSalt, 32)
	dataEnd := wechatPageSize - wechatReserve + wechatIVSize
	mac := hmac.New(sha512.New, macKey)
	mac.Write(page[16:dataEnd])
	var pageNumber [4]byte
	binary.LittleEndian.PutUint32(pageNumber[:], 1)
	mac.Write(pageNumber[:])
	copy(page[dataEnd:], mac.Sum(nil))
	return page
}

func TestDiscoverAndChooseAccounts(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"user-b", "user-a"} {
		if err := os.MkdirAll(filepath.Join(root, name, "db_storage", "message"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Non-account support folders must not be offered.
	if err := os.MkdirAll(filepath.Join(root, "all_users", "config"), 0o700); err != nil {
		t.Fatal(err)
	}

	accounts, err := discoverAccounts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0].Name != "user-a" || accounts[1].Name != "user-b" {
		t.Fatalf("unexpected accounts: %#v", accounts)
	}
	var prompt bytes.Buffer
	selected, err := chooseAccount(accounts, strings.NewReader("2\n"), &prompt)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Name != "user-b" {
		t.Fatalf("selected %q, want user-b", selected.Name)
	}
	if !strings.Contains(prompt.String(), "Multiple WeChat users found") {
		t.Fatalf("missing interactive prompt: %q", prompt.String())
	}
}

func TestDiscoverCopiedDatabaseFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "message_0.db"), []byte("copied"), 0o600); err != nil {
		t.Fatal(err)
	}
	accounts, err := discoverAccounts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].DatabaseRoot != root {
		t.Fatalf("unexpected copied-folder account: %#v", accounts)
	}
}
