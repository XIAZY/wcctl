package sqlcipher

import (
	"bytes"
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
		true,
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
	_, err := Query("testdata/fixture.db", make([]byte, 32), "SELECT * FROM sample", false)
	if err == nil {
		t.Fatal("Query succeeded with the wrong key")
	}
}

func TestQueryRejectsWrite(t *testing.T) {
	_, err := Query("testdata/fixture.db", fixtureKey(), "DELETE FROM sample", false)
	if err == nil {
		t.Fatal("Query accepted a write statement")
	}
}
