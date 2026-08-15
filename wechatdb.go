package main

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	wechatPageSize = 4096
	wechatReserve  = 80
	wechatIVSize   = 16
	wechatHMACSize = 64
)

type weChatAccount struct {
	Name         string
	Path         string
	DatabaseRoot string
}

func defaultWeChatDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Containers", "com.tencent.xinWeChat", "Data", "Documents", "xwechat_files"), nil
}

// discoverAccounts accepts the xwechat_files root, a single account folder,
// its db_storage folder, or an arbitrary folder containing copied .db files.
func discoverAccounts(root string) ([]weChatAccount, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", absRoot)
	}

	if dbRoot := filepath.Join(absRoot, "db_storage"); isDir(dbRoot) {
		return []weChatAccount{{Name: filepath.Base(absRoot), Path: absRoot, DatabaseRoot: dbRoot}}, nil
	}
	if filepath.Base(absRoot) == "db_storage" {
		return []weChatAccount{{Name: filepath.Base(filepath.Dir(absRoot)), Path: filepath.Dir(absRoot), DatabaseRoot: absRoot}}, nil
	}

	entries, err := os.ReadDir(absRoot)
	if err != nil {
		return nil, err
	}
	var accounts []weChatAccount
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		accountPath := filepath.Join(absRoot, entry.Name())
		dbRoot := filepath.Join(accountPath, "db_storage")
		if isDir(dbRoot) {
			accounts = append(accounts, weChatAccount{Name: entry.Name(), Path: accountPath, DatabaseRoot: dbRoot})
		}
	}
	if len(accounts) > 0 {
		sort.Slice(accounts, func(i, j int) bool { return accounts[i].Name < accounts[j].Name })
		return accounts, nil
	}

	if databases, walkErr := discoverDatabases(absRoot); walkErr == nil && len(databases) > 0 {
		return []weChatAccount{{Name: filepath.Base(absRoot), Path: absRoot, DatabaseRoot: absRoot}}, nil
	}
	return nil, fmt.Errorf("no WeChat account or .db files found under %s", absRoot)
}

func chooseAccount(accounts []weChatAccount, input io.Reader, output io.Writer) (weChatAccount, error) {
	if len(accounts) == 0 {
		return weChatAccount{}, fmt.Errorf("no accounts found")
	}
	if len(accounts) == 1 {
		fmt.Fprintf(output, "using WeChat account %s (%s)\n", accounts[0].Name, accounts[0].Path)
		return accounts[0], nil
	}

	fmt.Fprintln(output, "Multiple WeChat users found:")
	for i, account := range accounts {
		fmt.Fprintf(output, "  %d) %s (%s)\n", i+1, account.Name, account.Path)
	}
	fmt.Fprintf(output, "Select a user [1-%d]: ", len(accounts))
	scanner := bufio.NewScanner(input)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return weChatAccount{}, err
		}
		return weChatAccount{}, fmt.Errorf("no selection provided; pass -data-dir with a specific account folder")
	}
	selection, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || selection < 1 || selection > len(accounts) {
		return weChatAccount{}, fmt.Errorf("invalid selection %q", scanner.Text())
	}
	return accounts[selection-1], nil
}

func discoverDatabases(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".db") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no .db files found under %s", root)
	}
	sort.Strings(paths)
	return paths, nil
}

func findDatabaseKey(path string, candidates []keyCandidate) (keyCandidate, bool, error) {
	page, err := readFirstPage(path)
	if err != nil {
		return keyCandidate{}, false, err
	}
	if bytes.HasPrefix(page, []byte("SQLite format 3\x00")) {
		return keyCandidate{}, false, nil
	}

	// The final 16 bytes in each extracted 48-byte record are the database
	// salt. Test matching hints first, then fall back to every candidate so a
	// changed memory layout does not create false negatives.
	for _, hintedOnly := range []bool{true, false} {
		for _, candidate := range candidates {
			hintMatches := bytes.Equal(candidate.SaltHint[:], page[:16])
			if hintedOnly != hintMatches {
				continue
			}
			if validateWeChatV4AESKey(page, candidate.AESKey[:]) {
				return candidate, true, nil
			}
		}
	}
	return keyCandidate{}, false, nil
}

func readFirstPage(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	page := make([]byte, wechatPageSize)
	if _, err := io.ReadFull(file, page); err != nil {
		return nil, fmt.Errorf("read first page: %w", err)
	}
	return page, nil
}

func validateWeChatV4AESKey(page, aesKey []byte) bool {
	if len(page) < wechatPageSize || len(aesKey) != 32 {
		return false
	}
	macSalt := make([]byte, 16)
	for i := range macSalt {
		macSalt[i] = page[i] ^ 0x3a
	}
	macKey := pbkdf2SHA512Two(aesKey, macSalt, 32)
	dataEnd := wechatPageSize - wechatReserve + wechatIVSize
	mac := hmac.New(sha512.New, macKey)
	_, _ = mac.Write(page[16:dataEnd])
	var pageNumber [4]byte
	binary.LittleEndian.PutUint32(pageNumber[:], 1)
	_, _ = mac.Write(pageNumber[:])
	return hmac.Equal(mac.Sum(nil), page[dataEnd:dataEnd+wechatHMACSize])
}

// pbkdf2SHA512Two is the exact PBKDF2 operation used to derive the WeChat v4
// HMAC key from an already-derived AES key: one block, two iterations.
func pbkdf2SHA512Two(password, salt []byte, length int) []byte {
	blockInput := make([]byte, len(salt)+4)
	copy(blockInput, salt)
	binary.BigEndian.PutUint32(blockInput[len(salt):], 1)

	prf := hmac.New(sha512.New, password)
	_, _ = prf.Write(blockInput)
	u1 := prf.Sum(nil)
	prf.Reset()
	_, _ = prf.Write(u1)
	u2 := prf.Sum(nil)
	for i := range u1 {
		u1[i] ^= u2[i]
	}
	return u1[:length]
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
