// extract.go implements key extract (and the legacy extract-key alias): locate
// WeChat v4 AES material in a memory dump, validate it against an account's
// encrypted databases, and persist only cryptographically verified mappings.
package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const keyLen = 96 // x'<32-byte AES key><16-byte database salt>'

type keyCandidate struct {
	AESKey   [32]byte
	SaltHint [16]byte
}

func cmdExtractKey(args []string) {
	if err := runKeyExtract(args, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fatal("extract key: %v", err)
	}
}

func runKeyExtract(args []string, input io.Reader, output, errorOutput io.Writer) error {
	defaultKeys, err := defaultKeyStorePath()
	if err != nil {
		return fmt.Errorf("resolve key store: %w", err)
	}
	fs := flag.NewFlagSet("key extract", flag.ContinueOnError)
	fs.SetOutput(errorOutput)
	fs.Usage = func() {
		fmt.Fprintln(errorOutput, "usage: wcctl key extract [-capture PATH] [-data-dir PATH] [-account ACCOUNT] [-keys PATH]")
	}
	capture := fs.String("capture", "./dumps", "capture directory or a single binary file")
	legacyInput := fs.String("in", "", "deprecated alias for -capture")
	dataDir := fs.String("data-dir", "", "WeChat account folder or xwechat_files folder")
	accountName := fs.String("account", "", "WeChat account to verify")
	keyStorePath := fs.String("keys", defaultKeys, "path to keys.json")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *legacyInput != "" {
		*capture = *legacyInput
	}

	account, err := resolveExtractionAccount(*dataDir, *accountName, input, errorOutput)
	if err != nil {
		return err
	}
	_, err = extractAndSave(*capture, account, *keyStorePath, output, errorOutput)
	return err
}

func resolveExtractionAccount(dataDir, accountName string, input io.Reader, output io.Writer) (weChatAccount, error) {
	root := dataDir
	var err error
	if root == "" {
		root, err = defaultWeChatDataDir()
		if err != nil {
			return weChatAccount{}, fmt.Errorf("resolve default WeChat data directory: %w", err)
		}
	}
	accounts, err := discoverAccounts(root)
	if err != nil {
		return weChatAccount{}, fmt.Errorf("discover WeChat accounts: %w", err)
	}
	if accountName != "" {
		for _, account := range accounts {
			if account.Name == accountName {
				fmt.Fprintf(output, "using WeChat account %s (%s)\n", account.Name, account.Path)
				return account, nil
			}
		}
		return weChatAccount{}, fmt.Errorf("WeChat account %q not found under %s", accountName, root)
	}
	if len(accounts) > 1 {
		configPath, configPathErr := defaultConfigPath()
		if configPathErr == nil {
			config, configErr := readAppConfig(configPath)
			if configErr != nil {
				return weChatAccount{}, configErr
			}
			for _, account := range accounts {
				if account.Name == config.DefaultUser {
					fmt.Fprintf(output, "using configured WeChat account %s (%s)\n", account.Name, account.Path)
					return account, nil
				}
			}
		}
	}
	account, err := chooseAccount(accounts, input, output)
	if err != nil {
		return weChatAccount{}, fmt.Errorf("select WeChat account: %w", err)
	}
	return account, nil
}

func extractAndSave(capture string, account weChatAccount, keyStorePath string, output, errorOutput io.Writer) (int, error) {
	files, err := inputFiles(capture)
	if err != nil {
		return 0, fmt.Errorf("input: %w", err)
	}
	candidates, hits := extractCandidates(files, errorOutput)
	if len(candidates) == 0 {
		return 0, fmt.Errorf("no x'<96 hex>' AES candidates found in %s", capture)
	}
	fmt.Fprintf(errorOutput, "found %d hit(s), %d unique AES candidate(s)\n", hits, len(candidates))

	databases, err := discoverDatabases(account.DatabaseRoot)
	if err != nil {
		return 0, fmt.Errorf("discover databases: %w", err)
	}
	fmt.Fprintf(errorOutput, "testing %d candidate(s) against %d database(s) for %s\n",
		len(candidates), len(databases), account.Name)

	matches := make(map[string]storedDatabase)
	for _, dbPath := range databases {
		candidate, ok, err := findDatabaseKey(dbPath, candidates)
		if err != nil {
			fmt.Fprintf(errorOutput, "skip %s: %v\n", dbPath, err)
			continue
		}
		if !ok {
			continue
		}
		matches[dbPath] = newStoredDatabase(account.Name, candidate.AESKey[:])
		fmt.Fprintf(output, "matched %s\n", dbPath)
	}
	if len(matches) == 0 {
		return 0, fmt.Errorf("none of the extracted AES candidates matched databases under %s", account.DatabaseRoot)
	}

	if err := saveDatabaseKeys(keyStorePath, matches); err != nil {
		return 0, fmt.Errorf("save verified keys: %w", err)
	}
	fmt.Fprintf(errorOutput, "saved %d verified database key(s) to %s\n", len(matches), keyStorePath)
	return len(matches), nil
}

func inputFiles(path string) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("-in is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}

	patterns := []string{
		filepath.Join(path, "segments", "*.bin"),
		filepath.Join(path, "*.bin"),
	}
	var files []string
	for _, pattern := range patterns {
		matches, globErr := filepath.Glob(pattern)
		if globErr != nil {
			return nil, globErr
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no segments/*.bin or *.bin under directory: %s", path)
	}
	sort.Strings(files)
	return files, nil
}

func extractCandidates(files []string, warningOutput io.Writer) ([]keyCandidate, int) {
	seen := make(map[[32]byte]bool)
	var candidates []keyCandidate
	hits := 0
	for _, path := range files {
		found, err := scanFile(path)
		if err != nil {
			fmt.Fprintf(warningOutput, "skip %s: %v\n", path, err)
			continue
		}
		hits += len(found)
		for _, candidate := range found {
			if seen[candidate.AESKey] {
				continue
			}
			seen[candidate.AESKey] = true
			candidates = append(candidates, candidate)
		}
	}
	return candidates, hits
}

func scanFile(path string) ([]keyCandidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var found []keyCandidate
	for i := 0; i < len(data); {
		j := bytes.IndexByte(data[i:], 'x')
		if j < 0 {
			break
		}
		i += j
		if matchLen := matchAt(data, i); matchLen > 0 {
			payload, decodeErr := hex.DecodeString(string(data[i+2 : i+2+keyLen]))
			if decodeErr == nil {
				var candidate keyCandidate
				copy(candidate.AESKey[:], payload[:32])
				copy(candidate.SaltHint[:], payload[32:48])
				found = append(found, candidate)
			}
			i += matchLen
		} else {
			i++
		}
	}
	return found, nil
}

// matchAt reports whether data[i:] starts with x'<96 hex>'.
func matchAt(data []byte, i int) int {
	const total = 2 + keyLen + 1
	if i < 0 || i+total > len(data) || data[i] != 'x' || data[i+1] != '\'' {
		return 0
	}
	for k := 0; k < keyLen; k++ {
		if !isHex(data[i+2+k]) {
			return 0
		}
	}
	if data[i+2+keyLen] != '\'' {
		return 0
	}
	return total
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// segBase is retained for consumers that need to translate a segment offset
// back to the target process's virtual address.
func segBase(path string) (uint64, bool) {
	name := filepath.Base(path)
	hexPart, ok := strings.CutSuffix(name, ".bin")
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseUint(hexPart, 16, 64)
	return value, err == nil
}
