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
)

type contactRecord struct {
	ID                  int64  `json:"id"`
	Username            string `json:"username"`
	LocalType           int64  `json:"local_type"`
	Alias               string `json:"alias"`
	EncryptUsername     string `json:"encrypt_username"`
	Flag                int64  `json:"flag"`
	DeleteFlag          int64  `json:"delete_flag"`
	VerifyFlag          int64  `json:"verify_flag"`
	Remark              string `json:"remark"`
	RemarkQuanPin       string `json:"remark_quan_pin"`
	RemarkPinYinInitial string `json:"remark_pin_yin_initial"`
	NickName            string `json:"nick_name"`
	PinYinInitial       string `json:"pin_yin_initial"`
	QuanPin             string `json:"quan_pin"`
	BigHeadURL          string `json:"big_head_url"`
	SmallHeadURL        string `json:"small_head_url"`
	HeadImageMD5        string `json:"head_img_md5"`
	ChatRoomNotify      int64  `json:"chat_room_notify"`
	IsInChatRoom        int64  `json:"is_in_chat_room"`
	Description         string `json:"description"`
	ExtraBufferBytes    int64  `json:"extra_buffer_bytes"`
	ChatRoomType        int64  `json:"chat_room_type"`
}

func cmdContact(args []string) {
	if err := runContact(args, os.Stdout); err != nil {
		fatal("contacts: %v", err)
	}
}

func runContact(args []string, output io.Writer) error {
	defaultKeys, err := defaultKeyStorePath()
	if err != nil {
		return fmt.Errorf("resolve key store: %w", err)
	}
	fs := flag.NewFlagSet("contacts", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() {
		fmt.Fprintln(output, "usage: wcctl contacts [-user USER] [-json] [-keys PATH]")
		fs.PrintDefaults()
	}
	userName := fs.String("user", "", "WeChat user in the key store")
	jsonOutput := fs.Bool("json", false, "print all non-binary contact metadata as JSON")
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

	contacts, err := queryContactDatabase(databasePath, aesKey)
	if err != nil {
		return fmt.Errorf("user %q: %w", selectedName, err)
	}
	if *jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(contacts)
	}
	return printContactTable(output, contacts)
}

func readKeyStore(path string) (keyStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return keyStore{}, fmt.Errorf("read key store %s: %w", path, err)
	}
	var store keyStore
	if err := json.Unmarshal(data, &store); err != nil {
		return keyStore{}, fmt.Errorf("parse key store %s: %w", path, err)
	}
	if len(store.Users) == 0 {
		return keyStore{}, fmt.Errorf("key store %s contains no users", path)
	}
	return store, nil
}

func selectStoredUser(store keyStore, requested string) (string, storedUser, error) {
	if requested != "" {
		selection, err := resolveStoredUser(store, requested, "")
		if err != nil {
			return "", storedUser{}, err
		}
		return selection.Name, selection.User, nil
	}
	configPath, err := defaultConfigPath()
	if err != nil {
		return "", storedUser{}, fmt.Errorf("resolve config: %w", err)
	}
	config, err := readAppConfig(configPath)
	if err != nil {
		return "", storedUser{}, err
	}
	selection, err := resolveStoredUser(store, requested, config.DefaultUser)
	if err != nil {
		return "", storedUser{}, err
	}
	return selection.Name, selection.User, nil
}

func storedContactDatabase(user storedUser) (string, storedDatabase, error) {
	var paths []string
	for path := range user.Databases {
		if filepath.Base(path) == "contact.db" && filepath.Base(filepath.Dir(path)) == "contact" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return "", storedDatabase{}, fmt.Errorf("no contact/contact.db entry in key store")
	}
	if len(paths) > 1 {
		return "", storedDatabase{}, fmt.Errorf("multiple contact databases in key store: %s", strings.Join(paths, ", "))
	}
	return paths[0], user.Databases[paths[0]], nil
}

func queryContactDatabase(path string, aesKey []byte) ([]contactRecord, error) {
	contacts := make([]contactRecord, 0)
	if err := queryDatabaseJSON(path, aesKey, contactSelect, &contacts); err != nil {
		return nil, fmt.Errorf("query contact database: %w", err)
	}
	return contacts, nil
}

const contactSelect = `SELECT
  id,
  COALESCE(username, '') AS username,
  COALESCE(local_type, 0) AS local_type,
  COALESCE(alias, '') AS alias,
  COALESCE(encrypt_username, '') AS encrypt_username,
  COALESCE(flag, 0) AS flag,
  COALESCE(delete_flag, 0) AS delete_flag,
  COALESCE(verify_flag, 0) AS verify_flag,
  COALESCE(remark, '') AS remark,
  COALESCE(remark_quan_pin, '') AS remark_quan_pin,
  COALESCE(remark_pin_yin_initial, '') AS remark_pin_yin_initial,
  COALESCE(nick_name, '') AS nick_name,
  COALESCE(pin_yin_initial, '') AS pin_yin_initial,
  COALESCE(quan_pin, '') AS quan_pin,
  COALESCE(big_head_url, '') AS big_head_url,
  COALESCE(small_head_url, '') AS small_head_url,
  COALESCE(head_img_md5, '') AS head_img_md5,
  COALESCE(chat_room_notify, 0) AS chat_room_notify,
  COALESCE(is_in_chat_room, 0) AS is_in_chat_room,
  COALESCE(description, '') AS description,
  COALESCE(length(extra_buffer), 0) AS extra_buffer_bytes,
  COALESCE(chat_room_type, 0) AS chat_room_type
FROM contact
WHERE (COALESCE(local_type, 0) & 1) != 0
  AND COALESCE(delete_flag, 0) = 0
  AND COALESCE(verify_flag, 0) = 0
  AND COALESCE(username, '') NOT LIKE '%@chatroom'
  AND COALESCE(username, '') NOT LIKE 'gh\_%' ESCAPE '\'
  AND COALESCE(username, '') NOT IN (
    'notifymessage',
    'newsapp',
    'fmessage',
    'filehelper',
    'weibo',
    'qqmail',
    'tmessage',
    'qmessage',
    'qqsync',
    'floatbottle',
    'lbsapp',
    'shakeapp',
    'medianote',
    'readerapp',
    'blogapp',
    'facebookapp',
    'masssendapp',
    'meishiapp',
    'feedsapp',
    'voip',
    'blogappweixin',
    'brandsessionholder',
    'weixin',
    'weixinreminder',
    'officialaccounts',
    'notification_messages',
    'wxitil',
    'userexperience_alarm'
  )
ORDER BY COALESCE(NULLIF(remark, ''), NULLIF(nick_name, ''), NULLIF(alias, ''), username) COLLATE NOCASE;`

func printContactTable(output io.Writer, contacts []contactRecord) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	buffer := bufio.NewWriter(writer)
	if _, err := fmt.Fprintln(buffer, "ID\tDISPLAY NAME\tUSERNAME\tALIAS\tLOCAL\tVERIFY\tDELETED\tIN ROOM\tDESCRIPTION"); err != nil {
		return err
	}
	for _, contact := range contacts {
		if _, err := fmt.Fprintf(buffer, "%d\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%s\n",
			contact.ID,
			singleLine(contact.displayName()),
			singleLine(contact.Username),
			singleLine(contact.Alias),
			contact.LocalType,
			contact.VerifyFlag,
			contact.DeleteFlag,
			contact.IsInChatRoom,
			singleLine(contact.Description),
		); err != nil {
			return err
		}
	}
	if err := buffer.Flush(); err != nil {
		return err
	}
	return writer.Flush()
}

func (contact contactRecord) displayName() string {
	for _, value := range []string{contact.Remark, contact.NickName, contact.Alias, contact.Username} {
		if value != "" {
			return value
		}
	}
	return ""
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
