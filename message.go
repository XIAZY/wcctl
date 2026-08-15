package main

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/klauspost/compress/zstd"
)

type messageRecord struct {
	LocalID            int64  `json:"local_id"`
	ServerID           int64  `json:"server_id"`
	LocalType          int64  `json:"local_type"`
	SortSequence       int64  `json:"sort_sequence"`
	RealSenderID       int64  `json:"real_sender_id"`
	SenderUsername     string `json:"sender_username"`
	SenderName         string `json:"sender_name"`
	CreateTime         int64  `json:"create_time"`
	Status             int64  `json:"status"`
	UploadStatus       int64  `json:"upload_status"`
	DownloadStatus     int64  `json:"download_status"`
	ServerSequence     int64  `json:"server_sequence"`
	OriginSource       int64  `json:"origin_source"`
	Source             string `json:"source"`
	SourceEncoding     string `json:"source_encoding"`
	SourceDecodeError  string `json:"source_decode_error,omitempty"`
	MessageContent     string `json:"message_content"`
	ContentEncoding    string `json:"content_encoding"`
	ContentDecodeError string `json:"content_decode_error,omitempty"`
	PackedInfoBytes    int64  `json:"packed_info_bytes"`
	ContentCompression int64  `json:"content_compression"`
	SourceCompression  int64  `json:"source_compression"`
	CompressedContent  int64  `json:"compressed_content_bytes"`
	CompressedSource   int64  `json:"compressed_source_bytes"`
	Shard              string `json:"shard"`
	contentHex         string
	sourceHex          string
}

type messageIdentity struct {
	Username string `json:"username"`
	Remark   string `json:"remark"`
	NickName string `json:"nick_name"`
	Alias    string `json:"alias"`
}

func cmdMessage(args []string) {
	if err := runMessage(args, os.Stdout); err != nil {
		fatal("messages: %v", err)
	}
}

func runMessage(args []string, output io.Writer) error {
	defaultKeys, err := defaultKeyStorePath()
	if err != nil {
		return fmt.Errorf("resolve key store: %w", err)
	}
	fs := flag.NewFlagSet("messages", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() {
		fmt.Fprintln(output, "usage: wcctl messages -chat USERNAME [-limit N] [-before TIME] [-user USER] [-json] [-keys PATH]")
		fs.PrintDefaults()
	}
	chat := fs.String("chat", "", "contact or chatroom username")
	limit := fs.Int("limit", 50, "maximum number of messages")
	beforeValue := fs.String("before", "", "only messages before a Unix timestamp or RFC3339 time")
	userName := fs.String("user", "", "WeChat user in the key store")
	jsonOutput := fs.Bool("json", false, "print full decoded message metadata as JSON")
	keyStorePath := fs.String("keys", defaultKeys, "path to keys.json")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *chat == "" {
		return fmt.Errorf("-chat is required")
	}
	if *limit < 1 || *limit > 10000 {
		return fmt.Errorf("-limit must be between 1 and 10000")
	}
	before, err := parseMessageBefore(*beforeValue)
	if err != nil {
		return err
	}

	store, err := readKeyStore(*keyStorePath)
	if err != nil {
		return err
	}
	selectedName, user, err := selectStoredUser(store, *userName)
	if err != nil {
		return err
	}
	messages, err := listMessages(user, *chat, *limit, before)
	if err != nil {
		return fmt.Errorf("user %q: %w", selectedName, err)
	}
	if *jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(messages)
	}
	return printMessageTable(output, messages)
}

func parseMessageBefore(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		if unix <= 0 {
			return 0, fmt.Errorf("-before must be positive")
		}
		return unix, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0, fmt.Errorf("parse -before %q as Unix timestamp or RFC3339: %w", value, err)
	}
	return parsed.Unix(), nil
}

func listMessages(user storedUser, chat string, limit int, before int64) ([]messageRecord, error) {
	shards := storedMessageDatabases(user)
	if len(shards) == 0 {
		return nil, fmt.Errorf("no message/message_N.db entries in key store")
	}
	tableHash := md5.Sum([]byte(chat))
	tableName := "Msg_" + hex.EncodeToString(tableHash[:])
	all := make([]messageRecord, 0, limit*len(shards))
	for _, path := range shards {
		database := user.Databases[path]
		aesKey, err := hex.DecodeString(database.AESKey)
		if err != nil || len(aesKey) != 32 {
			return nil, fmt.Errorf("message shard %s has an invalid AES-256 key", path)
		}
		exists, err := messageTableExists(path, aesKey, tableName)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		messages, err := queryMessageShard(path, aesKey, tableName, limit, before)
		if err != nil {
			return nil, err
		}
		all = append(all, messages...)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no messages found for %q", chat)
	}

	if err := decodeMessagePayloads(all); err != nil {
		return nil, err
	}
	resolveMessageSenderNames(user, all)
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreateTime != all[j].CreateTime {
			return all[i].CreateTime > all[j].CreateTime
		}
		if all[i].SortSequence != all[j].SortSequence {
			return all[i].SortSequence > all[j].SortSequence
		}
		if all[i].LocalID != all[j].LocalID {
			return all[i].LocalID > all[j].LocalID
		}
		return all[i].Shard > all[j].Shard
	})
	if len(all) > limit {
		all = all[:limit]
	}
	for left, right := 0, len(all)-1; left < right; left, right = left+1, right-1 {
		all[left], all[right] = all[right], all[left]
	}
	return all, nil
}

func storedMessageDatabases(user storedUser) []string {
	var paths []string
	for path := range user.Databases {
		base := filepath.Base(path)
		if filepath.Base(filepath.Dir(path)) != "message" || !strings.HasPrefix(base, "message_") || !strings.HasSuffix(base, ".db") {
			continue
		}
		index := strings.TrimSuffix(strings.TrimPrefix(base, "message_"), ".db")
		if _, err := strconv.Atoi(index); err == nil {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func messageTableExists(path string, aesKey []byte, tableName string) (bool, error) {
	var tables []struct {
		Name string `json:"name"`
	}
	statement := fmt.Sprintf("SELECT name FROM sqlite_master WHERE type='table' AND name='%s';", tableName)
	if err := queryDatabaseJSON(path, aesKey, statement, &tables); err != nil {
		return false, fmt.Errorf("inspect message shard %s: %w", path, err)
	}
	return len(tables) == 1, nil
}

func queryMessageShard(path string, aesKey []byte, tableName string, limit int, before int64) ([]messageRecord, error) {
	condition := ""
	if before > 0 {
		condition = fmt.Sprintf("WHERE message.create_time < %d", before)
	}
	statement := fmt.Sprintf(`SELECT
  message.local_id AS local_id,
  COALESCE(message.server_id, 0) AS server_id,
  COALESCE(message.local_type, 0) AS local_type,
  COALESCE(message.sort_seq, 0) AS sort_sequence,
  COALESCE(message.real_sender_id, 0) AS real_sender_id,
  COALESCE(sender.user_name, '') AS sender_username,
  COALESCE(message.create_time, 0) AS create_time,
  COALESCE(message.status, 0) AS status,
  COALESCE(message.upload_status, 0) AS upload_status,
  COALESCE(message.download_status, 0) AS download_status,
  COALESCE(message.server_seq, 0) AS server_sequence,
  COALESCE(message.origin_source, 0) AS origin_source,
  CASE WHEN typeof(message.source) = 'text' THEN message.source ELSE '' END AS source,
  CASE WHEN typeof(message.source) = 'blob' THEN hex(message.source) ELSE '' END AS source_hex,
  CASE WHEN typeof(message.message_content) = 'text' THEN message.message_content ELSE '' END AS message_content,
  CASE WHEN typeof(message.message_content) = 'blob' THEN hex(message.message_content) ELSE '' END AS content_hex,
  COALESCE(length(message.packed_info_data), 0) AS packed_info_bytes,
  COALESCE(message.WCDB_CT_message_content, -1) AS content_compression,
  COALESCE(message.WCDB_CT_source, -1) AS source_compression
FROM "%s" AS message
LEFT JOIN Name2Id AS sender ON sender.rowid = message.real_sender_id
%s
ORDER BY message.create_time DESC, message.sort_seq DESC, message.local_id DESC
LIMIT %d;`, tableName, condition, limit)
	var rows []struct {
		LocalID            int64  `json:"local_id"`
		ServerID           int64  `json:"server_id"`
		LocalType          int64  `json:"local_type"`
		SortSequence       int64  `json:"sort_sequence"`
		RealSenderID       int64  `json:"real_sender_id"`
		SenderUsername     string `json:"sender_username"`
		CreateTime         int64  `json:"create_time"`
		Status             int64  `json:"status"`
		UploadStatus       int64  `json:"upload_status"`
		DownloadStatus     int64  `json:"download_status"`
		ServerSequence     int64  `json:"server_sequence"`
		OriginSource       int64  `json:"origin_source"`
		Source             string `json:"source"`
		SourceHex          string `json:"source_hex"`
		MessageContent     string `json:"message_content"`
		ContentHex         string `json:"content_hex"`
		PackedInfoBytes    int64  `json:"packed_info_bytes"`
		ContentCompression int64  `json:"content_compression"`
		SourceCompression  int64  `json:"source_compression"`
	}
	if err := queryDatabaseJSON(path, aesKey, statement, &rows); err != nil {
		return nil, fmt.Errorf("query message shard %s: %w", path, err)
	}
	messages := make([]messageRecord, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, messageRecord{
			LocalID:            row.LocalID,
			ServerID:           row.ServerID,
			LocalType:          row.LocalType,
			SortSequence:       row.SortSequence,
			RealSenderID:       row.RealSenderID,
			SenderUsername:     row.SenderUsername,
			SenderName:         row.SenderUsername,
			CreateTime:         row.CreateTime,
			Status:             row.Status,
			UploadStatus:       row.UploadStatus,
			DownloadStatus:     row.DownloadStatus,
			ServerSequence:     row.ServerSequence,
			OriginSource:       row.OriginSource,
			Source:             row.Source,
			SourceEncoding:     "text",
			MessageContent:     row.MessageContent,
			ContentEncoding:    "text",
			PackedInfoBytes:    row.PackedInfoBytes,
			ContentCompression: row.ContentCompression,
			SourceCompression:  row.SourceCompression,
			Shard:              filepath.Base(path),
			contentHex:         row.ContentHex,
			sourceHex:          row.SourceHex,
		})
	}
	return messages, nil
}

func decodeMessagePayloads(messages []messageRecord) error {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return fmt.Errorf("create zstd decoder: %w", err)
	}
	defer decoder.Close()
	for i := range messages {
		decodeMessageField(decoder, messages[i].contentHex, messages[i].ContentCompression,
			&messages[i].MessageContent, &messages[i].ContentEncoding,
			&messages[i].ContentDecodeError, &messages[i].CompressedContent)
		decodeMessageField(decoder, messages[i].sourceHex, messages[i].SourceCompression,
			&messages[i].Source, &messages[i].SourceEncoding,
			&messages[i].SourceDecodeError, &messages[i].CompressedSource)
		messages[i].contentHex = ""
		messages[i].sourceHex = ""
	}
	return nil
}

func decodeMessageField(decoder *zstd.Decoder, encoded string, compression int64, value, encoding, decodeError *string, compressedBytes *int64) {
	if encoded == "" {
		return
	}
	data, err := hex.DecodeString(encoded)
	if err != nil {
		*encoding = "blob"
		*decodeError = err.Error()
		return
	}
	*compressedBytes = int64(len(data))
	if compression != 4 {
		*encoding = "blob"
		*decodeError = fmt.Sprintf("unsupported WCDB compression type %d", compression)
		return
	}
	decoded, err := decoder.DecodeAll(data, nil)
	if err != nil {
		*encoding = "zstd"
		*decodeError = err.Error()
		return
	}
	*value = string(decoded)
	*encoding = "zstd"
}

func resolveMessageSenderNames(user storedUser, messages []messageRecord) {
	path, database, err := storedContactDatabase(user)
	if err != nil {
		return
	}
	aesKey, err := hex.DecodeString(database.AESKey)
	if err != nil || len(aesKey) != 32 {
		return
	}
	var identities []messageIdentity
	if err := queryDatabaseJSON(path, aesKey, `SELECT
  COALESCE(username, '') AS username,
  COALESCE(remark, '') AS remark,
  COALESCE(nick_name, '') AS nick_name,
  COALESCE(alias, '') AS alias
FROM contact;`, &identities); err != nil {
		return
	}
	names := make(map[string]string, len(identities))
	for _, identity := range identities {
		for _, name := range []string{identity.Remark, identity.NickName, identity.Alias, identity.Username} {
			if name != "" {
				names[identity.Username] = name
				break
			}
		}
	}
	for i := range messages {
		if name := names[messages[i].SenderUsername]; name != "" {
			messages[i].SenderName = name
		}
	}
}

func printMessageTable(output io.Writer, messages []messageRecord) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	buffer := bufio.NewWriter(writer)
	if _, err := fmt.Fprintln(buffer, "TIME\tSENDER\tTYPE\tMESSAGE"); err != nil {
		return err
	}
	for _, message := range messages {
		if _, err := fmt.Fprintf(buffer, "%s\t%s\t%s\t%s\n",
			formatMessageTime(message.CreateTime),
			singleLine(message.SenderName),
			messageTypeName(message.LocalType),
			truncateMessage(singleLine(messageSummary(message)), 200),
		); err != nil {
			return err
		}
	}
	if err := buffer.Flush(); err != nil {
		return err
	}
	return writer.Flush()
}

func formatMessageTime(value int64) string {
	if value > 100000000000 {
		value /= 1000
	}
	return time.Unix(value, 0).Local().Format("2006-01-02 15:04:05")
}

func messageTypeName(localType int64) string {
	base := uint32(localType)
	switch base {
	case 1:
		return "text"
	case 3:
		return "image"
	case 34:
		return "voice"
	case 42:
		return "contact"
	case 43:
		return "video"
	case 47:
		return "emoji"
	case 48:
		return "location"
	case 49:
		subtype := uint64(localType) >> 32
		if subtype != 0 {
			return fmt.Sprintf("app/%d", subtype)
		}
		return "app"
	case 50:
		return "call"
	case 10000:
		return "system"
	default:
		return strconv.FormatUint(uint64(localType), 10)
	}
}

func messageSummary(message messageRecord) string {
	if message.ContentDecodeError != "" {
		return "[compressed content could not be decoded]"
	}
	content := strings.TrimSpace(message.MessageContent)
	switch uint32(message.LocalType) {
	case 1:
		return content
	case 3:
		return "[image]"
	case 34:
		return "[voice]"
	case 42:
		return "[contact card]"
	case 43:
		return "[video]"
	case 47:
		return "[emoji]"
	case 48:
		return "[location]"
	case 49:
		if title, url := appMessageDetails(content); title != "" || url != "" {
			return strings.TrimSpace(strings.Join([]string{title, url}, " "))
		}
		return "[app message]"
	case 50:
		return "[call]"
	case 10000:
		if replacement := revokedMessageText(content); replacement != "" {
			return replacement
		}
		return content
	default:
		if content != "" && !strings.HasPrefix(content, "<") {
			return content
		}
		return "[message]"
	}
}

func appMessageDetails(content string) (string, string) {
	var document struct {
		AppMessage struct {
			Title string `xml:"title"`
			URL   string `xml:"url"`
		} `xml:"appmsg"`
	}
	if xml.Unmarshal([]byte(content), &document) != nil {
		return "", ""
	}
	return strings.TrimSpace(document.AppMessage.Title), strings.TrimSpace(document.AppMessage.URL)
}

func revokedMessageText(content string) string {
	var document struct {
		Revoke struct {
			Replacement string `xml:"replacemsg"`
		} `xml:"revokemsg"`
	}
	if xml.Unmarshal([]byte(content), &document) != nil {
		return ""
	}
	return strings.TrimSpace(document.Revoke.Replacement)
}

func truncateMessage(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	runes := []rune(value)
	return string(runes[:maximum-1]) + "…"
}
