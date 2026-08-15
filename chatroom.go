package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
)

type chatRoomRecord struct {
	ID                      int64  `json:"id"`
	Username                string `json:"username"`
	Owner                   string `json:"owner"`
	Remark                  string `json:"remark"`
	NickName                string `json:"nick_name"`
	Description             string `json:"description"`
	LocalType               int64  `json:"local_type"`
	Flag                    int64  `json:"flag"`
	DeleteFlag              int64  `json:"delete_flag"`
	ChatRoomNotify          int64  `json:"chat_room_notify"`
	ChatRoomType            int64  `json:"chat_room_type"`
	BigHeadURL              string `json:"big_head_url"`
	SmallHeadURL            string `json:"small_head_url"`
	MemberCount             int64  `json:"member_count"`
	Announcement            string `json:"announcement"`
	AnnouncementEditor      string `json:"announcement_editor"`
	AnnouncementPublishTime int64  `json:"announcement_publish_time"`
	ChatRoomStatus          int64  `json:"chat_room_status"`
	XMLAnnouncement         string `json:"xml_announcement"`
	RoomExtraBufferBytes    int64  `json:"room_extra_buffer_bytes"`
	DetailExtraBufferBytes  int64  `json:"detail_extra_buffer_bytes"`
}

func cmdChatRoom(args []string) {
	if err := runChatRoom(args, os.Stdout); err != nil {
		fatal("chatrooms: %v", err)
	}
}

func runChatRoom(args []string, output io.Writer) error {
	defaultKeys, err := defaultKeyStorePath()
	if err != nil {
		return fmt.Errorf("resolve key store: %w", err)
	}
	fs := flag.NewFlagSet("chatrooms", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() {
		fmt.Fprintln(output, "usage: wcctl chatrooms [-user USER] [-json] [-keys PATH]")
		fs.PrintDefaults()
	}
	userName := fs.String("user", "", "WeChat user in the key store")
	jsonOutput := fs.Bool("json", false, "print all non-binary chatroom metadata as JSON")
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

	store, err := readKeyStore(*keyStorePath)
	if err != nil {
		return err
	}
	selectedName, user, err := selectStoredUser(store, *userName)
	if err != nil {
		return err
	}
	databasePath, database, err := storedContactDatabase(user)
	if err != nil {
		return fmt.Errorf("user %q: %w", selectedName, err)
	}
	aesKey, err := hex.DecodeString(database.AESKey)
	if err != nil || len(aesKey) != 32 {
		return fmt.Errorf("user %q: contact database has an invalid AES-256 key", selectedName)
	}

	rooms, err := queryChatRooms(databasePath, aesKey)
	if err != nil {
		return fmt.Errorf("user %q: %w", selectedName, err)
	}
	if *jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(rooms)
	}
	return printChatRoomTable(output, rooms)
}

func queryChatRooms(path string, aesKey []byte) ([]chatRoomRecord, error) {
	rooms := make([]chatRoomRecord, 0)
	if err := queryDatabaseJSON(path, aesKey, chatRoomSelect, &rooms); err != nil {
		return nil, fmt.Errorf("query chatrooms: %w", err)
	}
	return rooms, nil
}

const chatRoomSelect = `SELECT
  room.id AS id,
  COALESCE(room.username, '') AS username,
  COALESCE(room.owner, '') AS owner,
  COALESCE(contact.remark, '') AS remark,
  COALESCE(contact.nick_name, '') AS nick_name,
  COALESCE(contact.description, '') AS description,
  COALESCE(contact.local_type, 0) AS local_type,
  COALESCE(contact.flag, 0) AS flag,
  COALESCE(contact.delete_flag, 0) AS delete_flag,
  COALESCE(contact.chat_room_notify, 0) AS chat_room_notify,
  COALESCE(contact.chat_room_type, 0) AS chat_room_type,
  COALESCE(contact.big_head_url, '') AS big_head_url,
  COALESCE(contact.small_head_url, '') AS small_head_url,
  COALESCE(members.member_count, 0) AS member_count,
  COALESCE(detail.announcement_, '') AS announcement,
  COALESCE(detail.announcement_editor_, '') AS announcement_editor,
  COALESCE(detail.announcement_publish_time_, 0) AS announcement_publish_time,
  COALESCE(detail.chat_room_status_, 0) AS chat_room_status,
  COALESCE(detail.xml_announcement_, '') AS xml_announcement,
  COALESCE(length(room.ext_buffer), 0) AS room_extra_buffer_bytes,
  COALESCE(length(detail.ext_buffer_), 0) AS detail_extra_buffer_bytes
FROM chat_room AS room
LEFT JOIN contact ON contact.id = room.id
LEFT JOIN chat_room_info_detail AS detail ON detail.room_id_ = room.id
LEFT JOIN (
  SELECT room_id, COUNT(*) AS member_count
  FROM chatroom_member
  GROUP BY room_id
) AS members ON members.room_id = room.id
ORDER BY COALESCE(NULLIF(contact.remark, ''), NULLIF(contact.nick_name, ''), room.username) COLLATE NOCASE;`

func printChatRoomTable(output io.Writer, rooms []chatRoomRecord) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	buffer := bufio.NewWriter(writer)
	if _, err := fmt.Fprintln(buffer, "ID\tDISPLAY NAME\tUSERNAME\tOWNER\tMEMBERS\tSTATUS\tNOTIFY\tANNOUNCEMENT"); err != nil {
		return err
	}
	for _, room := range rooms {
		if _, err := fmt.Fprintf(buffer, "%d\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
			room.ID,
			singleLine(room.displayName()),
			singleLine(room.Username),
			singleLine(room.Owner),
			room.MemberCount,
			room.ChatRoomStatus,
			room.ChatRoomNotify,
			singleLine(room.Announcement),
		); err != nil {
			return err
		}
	}
	if err := buffer.Flush(); err != nil {
		return err
	}
	return writer.Flush()
}

func (room chatRoomRecord) displayName() string {
	for _, value := range []string{room.Remark, room.NickName, room.Username} {
		if value != "" {
			return value
		}
	}
	return ""
}
