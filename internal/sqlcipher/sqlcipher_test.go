package sqlcipher

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func fixtureKey() []byte {
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index)
	}
	return key
}

func TestQueryEncryptedDatabase(t *testing.T) {
	rows, err := Query(
		"testdata/fixture.db",
		fixtureKey(),
		"SELECT id, name, score, payload, optional FROM sample",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row["id"] != int64(1) || row["name"] != "alpha" || row["score"] != 1.5 {
		t.Fatalf("unexpected scalar values: %#v", row)
	}
	if payload, ok := row["payload"].([]byte); !ok || !bytes.Equal(payload, []byte{1, 2, 255}) {
		t.Fatalf("unexpected blob: %#v", row["payload"])
	}
	if row["optional"] != nil {
		t.Fatalf("unexpected NULL value: %#v", row["optional"])
	}
}

func TestQueryRejectsWrongKey(t *testing.T) {
	_, err := Query("testdata/fixture.db", make([]byte, 32), "SELECT * FROM sample")
	if err == nil {
		t.Fatal("Query succeeded with the wrong key")
	}
}

func TestQueryRejectsWrite(t *testing.T) {
	_, err := Query("testdata/fixture.db", fixtureKey(), "DELETE FROM sample")
	if err == nil {
		t.Fatal("Query accepted a write statement")
	}
}

func TestQueryLeavesSourceDirectoryUnchanged(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "fixture.db")
	fixture, err := os.ReadFile("testdata/fixture.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasePath, fixture, 0o400); err != nil {
		t.Fatal(err)
	}

	if _, err := Query(databasePath, fixtureKey(), "SELECT name FROM sample"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "fixture.db" {
		t.Fatalf("query changed source directory: %#v", entries)
	}
	after, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, fixture) {
		t.Fatal("query changed source database")
	}
	info, err := os.Stat(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o400 {
		t.Fatalf("source database mode = %#o, want 0400", info.Mode().Perm())
	}
}

func TestWorkingCopyIncludesWALAndJournalButNotSHM(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "message.db")
	files := map[string][]byte{
		"":         []byte("database contents"),
		"-wal":     []byte("wal contents"),
		"-journal": []byte("journal contents"),
		"-shm":     []byte("shm contents"),
	}
	for suffix, contents := range files {
		if err := os.WriteFile(databasePath+suffix, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	copy, err := makeWorkingCopy(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	copyDirectory := copy.directory
	for _, suffix := range []string{"", "-wal", "-journal"} {
		contents, err := os.ReadFile(copy.path + suffix)
		if err != nil {
			t.Fatalf("read copied %q file: %v", suffix, err)
		}
		if !bytes.Equal(contents, files[suffix]) {
			t.Fatalf("copied %q contents = %q, want %q", suffix, contents, files[suffix])
		}
		info, err := os.Stat(copy.path + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o600 != 0o600 {
			t.Fatalf("copied %q mode = %#o, want owner read/write", suffix, info.Mode().Perm())
		}
	}
	if _, err := os.Stat(copy.path + "-shm"); !os.IsNotExist(err) {
		t.Fatalf("SHM was copied or could not be inspected: %v", err)
	}

	if err := os.WriteFile(databasePath, []byte("changed source"), 0o600); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(copy.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contents, files[""]) {
		t.Fatalf("working copy changed with source: %q", contents)
	}

	if err := copy.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(copyDirectory); !os.IsNotExist(err) {
		t.Fatalf("working directory was not removed: %v", err)
	}
	if err := copy.Close(); err != nil {
		t.Fatalf("second cleanup failed: %v", err)
	}
}

func TestCopyFileFallsBackWhenCloneIsUnavailable(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	destination := filepath.Join(root, "destination.db")
	want := []byte("encrypted database")
	if err := os.WriteFile(source, want, 0o400); err != nil {
		t.Fatal(err)
	}

	cloneCalls := 0
	err := copyFileWithClone(source, destination, func(_, _ string) error {
		cloneCalls++
		return os.NewSyscallError("clonefile", syscall.ENOTSUP)
	})
	if err != nil {
		t.Fatal(err)
	}
	if cloneCalls != 1 {
		t.Fatalf("clone calls = %d, want 1", cloneCalls)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fallback copy = %q, want %q", got, want)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o600 != 0o600 {
		t.Fatalf("fallback mode = %#o, want owner read/write", info.Mode().Perm())
	}
}
