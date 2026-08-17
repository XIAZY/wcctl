package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

type sessionRecord struct {
	Username                 string `json:"username"`
	DisplayName              string `json:"display_name"`
	Type                     int64  `json:"type"`
	UnreadCount              int64  `json:"unread_count"`
	UnreadStat               int64  `json:"unread_stat"`
	UnreadListCount          int64  `json:"unread_list_count"`
	UnreadFirstMessageID     int64  `json:"unread_first_message_server_id"`
	UnreadFirstPatLocalID    int64  `json:"unread_first_pat_message_local_id"`
	UnreadFirstPatSortSeq    int64  `json:"unread_first_pat_message_sort_sequence"`
	IsHidden                 int64  `json:"is_hidden"`
	Summary                  string `json:"summary"`
	Draft                    string `json:"draft"`
	Status                   int64  `json:"status"`
	LastTimestamp            int64  `json:"last_timestamp"`
	SortTimestamp            int64  `json:"sort_timestamp"`
	LastClearUnreadTimestamp int64  `json:"last_clear_unread_timestamp"`
	LastMessageLocalID       int64  `json:"last_message_local_id"`
	LastMessageType          int64  `json:"last_message_type"`
	LastMessageSubType       int64  `json:"last_message_sub_type"`
	LastMessageSender        string `json:"last_message_sender"`
	LastSenderDisplayName    string `json:"last_sender_display_name"`
	LastMessageExtType       int64  `json:"last_message_ext_type"`
	NoContactTitle           string `json:"no_contact_title"`
}

type sessionIdentity struct {
	Username string `json:"username"`
	Remark   string `json:"remark"`
	NickName string `json:"nick_name"`
	Alias    string `json:"alias"`
}

func cmdSession(args []string) {
	if err := runSession(args, os.Stdout); err != nil {
		fatal("sessions: %v", err)
	}
}

func runSession(args []string, output io.Writer) error {
	defaultKeys, err := defaultKeyStorePath()
	if err != nil {
		return fmt.Errorf("resolve key store: %w", err)
	}
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() {
		fmt.Fprintln(output, "usage: wcctl sessions [-limit N] [-user USER] [-json] [-keys PATH]")
		fs.PrintDefaults()
	}
	limit := fs.Int("limit", 50, "maximum number of sessions")
	userName := fs.String("user", "", "WeChat user in the key store")
	jsonOutput := fs.Bool("json", false, "print all session metadata as JSON")
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
	if *limit < 1 || *limit > 10000 {
		return fmt.Errorf("-limit must be between 1 and 10000")
	}

	store, err := readKeyStore(*keyStorePath)
	if err != nil {
		return err
	}
	selectedName, user, err := selectStoredUser(store, *userName)
	if err != nil {
		return err
	}
	sessions, err := listSessions(user, *limit)
	if err != nil {
		return fmt.Errorf("user %q: %w", selectedName, err)
	}
	if *jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(sessions)
	}
	return printSessionTable(output, sessions)
}

func listSessions(user storedUser, limit int) ([]sessionRecord, error) {
	path, database, err := storedSessionDatabase(user)
	if err != nil {
		return nil, err
	}
	aesKey, err := hex.DecodeString(database.AESKey)
	if err != nil || len(aesKey) != 32 {
		return nil, fmt.Errorf("session database has an invalid AES-256 key")
	}
	statement := fmt.Sprintf(sessionSelect, limit)
	sessions := make([]sessionRecord, 0)
	if err := queryDatabaseJSON(path, aesKey, statement, &sessions); err != nil {
		return nil, fmt.Errorf("query session database: %w", err)
	}
	resolveSessionDisplayNames(user, sessions)
	return sessions, nil
}

func storedSessionDatabase(user storedUser) (string, storedDatabase, error) {
	var paths []string
	for path := range user.Databases {
		if filepath.Base(path) == "session.db" && filepath.Base(filepath.Dir(path)) == "session" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return "", storedDatabase{}, fmt.Errorf("no session/session.db entry in key store")
	}
	if len(paths) > 1 {
		return "", storedDatabase{}, fmt.Errorf("multiple session databases in key store: %s", strings.Join(paths, ", "))
	}
	return paths[0], user.Databases[paths[0]], nil
}

const sessionSelect = `SELECT
  COALESCE(session.username, '') AS username,
  '' AS display_name,
  COALESCE(session.type, 0) AS type,
  COALESCE(session.unread_count, 0) AS unread_count,
  COALESCE(unread_stat.unread_stat, 0) AS unread_stat,
  COALESCE(unread_list.message_count, 0) AS unread_list_count,
  COALESCE(session.unread_first_msg_srv_id, 0) AS unread_first_message_server_id,
  COALESCE(session.unread_first_pat_msg_local_id, 0) AS unread_first_pat_message_local_id,
  COALESCE(session.unread_first_pat_msg_sort_seq, 0) AS unread_first_pat_message_sort_sequence,
  COALESCE(session.is_hidden, 0) AS is_hidden,
  COALESCE(session.summary, '') AS summary,
  COALESCE(session.draft, '') AS draft,
  COALESCE(session.status, 0) AS status,
  COALESCE(session.last_timestamp, 0) AS last_timestamp,
  COALESCE(session.sort_timestamp, 0) AS sort_timestamp,
  COALESCE(session.last_clear_unread_timestamp, 0) AS last_clear_unread_timestamp,
  COALESCE(session.last_msg_locald_id, 0) AS last_message_local_id,
  COALESCE(session.last_msg_type, 0) AS last_message_type,
  COALESCE(session.last_msg_sub_type, 0) AS last_message_sub_type,
  COALESCE(session.last_msg_sender, '') AS last_message_sender,
  COALESCE(session.last_sender_display_name, '') AS last_sender_display_name,
  COALESCE(session.last_msg_ext_type, 0) AS last_message_ext_type,
  COALESCE(no_contact.session_title, '') AS no_contact_title
FROM SessionTable AS session
LEFT JOIN Name2Id AS name ON name.user_name = session.username
LEFT JOIN SessionUnreadStatTable_1 AS unread_stat ON unread_stat.username_id = name.rowid
LEFT JOIN (
  SELECT username_id, COUNT(*) AS message_count
  FROM SessionUnreadListTable_1
  GROUP BY username_id
) AS unread_list ON unread_list.username_id = name.rowid
LEFT JOIN SessionNoContactInfoTable AS no_contact ON no_contact.username = session.username
ORDER BY session.last_timestamp DESC, session.sort_timestamp DESC, session.username
LIMIT %d;`

func resolveSessionDisplayNames(user storedUser, sessions []sessionRecord) {
	path, database, err := storedContactDatabase(user)
	if err != nil {
		for i := range sessions {
			sessions[i].DisplayName = sessionFallbackName(sessions[i])
		}
		return
	}
	aesKey, err := hex.DecodeString(database.AESKey)
	if err != nil || len(aesKey) != 32 {
		return
	}
	var identities []sessionIdentity
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
	for i := range sessions {
		sessions[i].DisplayName = names[sessions[i].Username]
		if sessions[i].DisplayName == "" {
			sessions[i].DisplayName = sessionFallbackName(sessions[i])
		}
	}
}

func sessionFallbackName(session sessionRecord) string {
	if session.NoContactTitle != "" {
		return session.NoContactTitle
	}
	return session.Username
}

func printSessionTable(output io.Writer, sessions []sessionRecord) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	buffer := bufio.NewWriter(writer)
	if _, err := fmt.Fprintln(buffer, "TIME\tNAME\tUSERNAME\tUNREAD\tHIDDEN\tLAST SENDER\tSUMMARY"); err != nil {
		return err
	}
	for _, session := range sessions {
		if _, err := fmt.Fprintf(buffer, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			formatSessionTime(session.LastTimestamp),
			singleLine(session.DisplayName),
			singleLine(session.Username),
			session.UnreadCount,
			session.IsHidden,
			singleLine(session.LastSenderDisplayName),
			truncateMessage(singleLine(session.Summary), 200),
		); err != nil {
			return err
		}
	}
	if err := buffer.Flush(); err != nil {
		return err
	}
	return writer.Flush()
}

func formatSessionTime(value int64) string {
	if value <= 0 {
		return "-"
	}
	if value > 100000000000 {
		value /= 1000
	}
	return time.Unix(value, 0).Local().Format("2006-01-02 15:04:05")
}
