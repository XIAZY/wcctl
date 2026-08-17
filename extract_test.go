package main

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanFileExtractsAndDeduplicatesCandidates(t *testing.T) {
	payload := strings.Repeat("ab", 48)
	path := filepath.Join(t.TempDir(), "segment.bin")
	data := []byte("noise x'" + payload + "' more x'" + payload + "' trailing")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	candidates, hits := extractCandidates([]string{path}, &bytes.Buffer{})
	if hits != 2 {
		t.Fatalf("hits = %d, want 2", hits)
	}
	if len(candidates) != 1 {
		t.Fatalf("unique candidates = %d, want 1", len(candidates))
	}
	wantKey, _ := hex.DecodeString(strings.Repeat("ab", 32))
	if !bytes.Equal(candidates[0].AESKey[:], wantKey) {
		t.Fatal("unexpected AES key bytes")
	}
}

func TestMatchAtRejectsMalformedPatterns(t *testing.T) {
	valid := []byte("x'" + strings.Repeat("01", 48) + "'")
	if got := matchAt(valid, 0); got != len(valid) {
		t.Fatalf("valid match length = %d, want %d", got, len(valid))
	}
	invalid := append([]byte(nil), valid...)
	invalid[10] = 'z'
	if got := matchAt(invalid, 0); got != 0 {
		t.Fatalf("malformed match length = %d, want 0", got)
	}
}

func TestExtractMaskedCandidatesDerivesMaskFromDatabaseSalt(t *testing.T) {
	root := t.TempDir()
	key := bytes.Repeat([]byte{0x42}, 32)
	page := authenticatedTestPage(key)
	databasePath := filepath.Join(root, "message.db")
	if err := os.WriteFile(databasePath, page, 0o600); err != nil {
		t.Fatal(err)
	}

	segmentPath := filepath.Join(root, "segment.bin")
	record := testMaskedKeyRecord(key, page[:16])
	data := append([]byte("unrelated memory"), record...)
	data = append(data, []byte("trailing memory")...)
	if err := os.WriteFile(segmentPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	candidates, hits := extractMaskedCandidates([]string{segmentPath}, []string{databasePath}, &bytes.Buffer{})
	if hits != 1 {
		t.Fatalf("masked hits = %d, want 1", hits)
	}
	if len(candidates) != 1 {
		t.Fatalf("unique masked candidates = %d, want 1", len(candidates))
	}
	if !bytes.Equal(candidates[0].AESKey[:], key) {
		t.Fatal("masked fallback recovered the wrong AES key")
	}
	if !bytes.Equal(candidates[0].SaltHint[:], page[:16]) {
		t.Fatal("masked fallback recovered the wrong salt hint")
	}
}

func testMaskedKeyRecord(key, salt []byte) []byte {
	plaintext := []byte("x'" + hex.EncodeToString(key) + hex.EncodeToString(salt) + "'")
	mask := make([]byte, 32)
	for index := range mask {
		mask[index] = byte(index*7 + 3)
	}
	ciphertext := make([]byte, len(plaintext))
	for index := range plaintext {
		ciphertext[index] = plaintext[index] ^ mask[index&31]
	}
	return ciphertext
}
