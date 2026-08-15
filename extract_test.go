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

	candidates, hits := extractCandidates([]string{path})
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
