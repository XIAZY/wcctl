package main

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestRunMessageWithoutSubcommandPrintsUsage(t *testing.T) {
	var output bytes.Buffer
	if err := runMessage(nil, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "wcctl message <subcommand>") || !strings.Contains(got, "ls  list messages") {
		t.Fatalf("unexpected message usage:\n%s", got)
	}
}

func TestStoredMessageDatabasesSelectsNumberedPrimaryShards(t *testing.T) {
	user := storedUser{Databases: map[string]storedDatabase{
		"/db/message/message_2.db":     {},
		"/db/message/message_0.db":     {},
		"/db/message/biz_message_0.db": {},
		"/db/message/message_fts.db":   {},
		"/db/message/media_0.db":       {},
	}}
	want := []string{"/db/message/message_0.db", "/db/message/message_2.db"}
	got := storedMessageDatabases(user)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("message databases = %#v, want %#v", got, want)
	}
}

func TestDecodeMessagePayloadsZstd(t *testing.T) {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	compressed := encoder.EncodeAll([]byte("hello compressed world"), nil)
	encoder.Close()
	messages := []messageRecord{{
		ContentCompression: 4,
		ContentEncoding:    "text",
		contentHex:         hex.EncodeToString(compressed),
	}}
	if err := decodeMessagePayloads(messages); err != nil {
		t.Fatal(err)
	}
	if messages[0].MessageContent != "hello compressed world" || messages[0].ContentEncoding != "zstd" {
		t.Fatalf("unexpected decoded message: %#v", messages[0])
	}
}

func TestMessageTypeAndSummary(t *testing.T) {
	message := messageRecord{
		LocalType:      int64(57)<<32 | 49,
		MessageContent: "<msg><appmsg><title>Example</title><url>https://example.com</url></appmsg></msg>",
	}
	if got := messageTypeName(message.LocalType); got != "app/57" {
		t.Fatalf("message type = %q", got)
	}
	if got := messageSummary(message); got != "Example https://example.com" {
		t.Fatalf("message summary = %q", got)
	}
}

func TestParseMessageBefore(t *testing.T) {
	want := time.Date(2026, 8, 15, 1, 2, 3, 0, time.UTC).Unix()
	for _, value := range []string{"1786755723", "2026-08-15T01:02:03Z"} {
		got, err := parseMessageBefore(value)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("parseMessageBefore(%q) = %d, want %d", value, got, want)
		}
	}
}

func TestPrintMessageTable(t *testing.T) {
	var output bytes.Buffer
	err := printMessageTable(&output, []messageRecord{{
		CreateTime:     1786755723,
		SenderName:     "Alice",
		LocalType:      1,
		MessageContent: "hello\nworld",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "text") || !strings.Contains(got, "hello world") {
		t.Fatalf("unexpected message table:\n%s", got)
	}
}
