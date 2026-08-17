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

const (
	keyLen       = 96 // x'<32-byte AES key><16-byte database salt>'
	keyRecordLen = 2 + keyLen + 1
)

type keyCandidate struct {
	AESKey   [32]byte
	SaltHint [16]byte
}

type maskedSaltProbe struct {
	Salt    [16]byte
	SaltHex [32]byte
}

type maskedSaltSignature [3]byte

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
	databases, err := discoverDatabases(account.DatabaseRoot)
	if err != nil {
		return 0, fmt.Errorf("discover databases: %w", err)
	}

	candidates, hits := extractCandidates(files, errorOutput)
	if len(candidates) > 0 {
		fmt.Fprintf(errorOutput, "found %d plaintext hit(s), %d unique AES candidate(s)\n", hits, len(candidates))
		fmt.Fprintf(errorOutput, "testing %d plaintext candidate(s) against %d database(s) for %s\n",
			len(candidates), len(databases), account.Name)
	} else {
		fmt.Fprintln(errorOutput, "no plaintext x'<96 hex>' AES candidates found")
	}

	matches := make(map[string]storedDatabase)
	matchDatabaseCandidates(databases, candidates, account.Name, matches, output, errorOutput)

	var unmatched []string
	for _, dbPath := range databases {
		if _, ok := matches[dbPath]; !ok {
			unmatched = append(unmatched, dbPath)
		}
	}

	maskedCandidates := []keyCandidate(nil)
	maskedHits := 0
	if len(unmatched) > 0 {
		fmt.Fprintf(errorOutput, "trying mask-free salt fallback for %d database(s) without a plaintext match\n", len(unmatched))
		maskedCandidates, maskedHits = extractMaskedCandidates(files, unmatched, errorOutput)
		if len(maskedCandidates) > 0 {
			fmt.Fprintf(errorOutput, "found %d masked hit(s), %d unique AES candidate(s)\n", maskedHits, len(maskedCandidates))
			matchDatabaseCandidates(unmatched, maskedCandidates, account.Name, matches, output, errorOutput)
		} else {
			fmt.Fprintln(errorOutput, "no salt-derived masked AES candidates found")
		}
	}

	if len(matches) == 0 {
		if len(candidates) == 0 && len(maskedCandidates) == 0 {
			return 0, fmt.Errorf("no plaintext or salt-derived masked AES candidates found in %s", capture)
		}
		return 0, fmt.Errorf("none of the extracted AES candidates matched databases under %s", account.DatabaseRoot)
	}

	if err := saveDatabaseKeys(keyStorePath, matches); err != nil {
		return 0, fmt.Errorf("save verified keys: %w", err)
	}
	fmt.Fprintf(errorOutput, "saved %d verified database key(s) to %s\n", len(matches), keyStorePath)
	return len(matches), nil
}

func matchDatabaseCandidates(databases []string, candidates []keyCandidate, accountName string, matches map[string]storedDatabase, output, errorOutput io.Writer) {
	for _, dbPath := range databases {
		if _, ok := matches[dbPath]; ok {
			continue
		}
		candidate, ok, err := findDatabaseKey(dbPath, candidates)
		if err != nil {
			fmt.Fprintf(errorOutput, "skip %s: %v\n", dbPath, err)
			continue
		}
		if !ok {
			continue
		}
		matches[dbPath] = newStoredDatabase(accountName, candidate.AESKey[:])
		fmt.Fprintf(output, "matched %s\n", dbPath)
	}
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

// extractMaskedCandidates finds masked x'<key><salt>' records without knowing
// the mask. The known database salt occupies 32 hexadecimal characters, which
// spans one complete period of WeChat's repeating 32-byte XOR mask. Each
// candidate window therefore contains enough known plaintext to reconstruct
// its own mask and recover the key.
func extractMaskedCandidates(files, databases []string, warningOutput io.Writer) ([]keyCandidate, int) {
	buckets := make(map[maskedSaltSignature][]maskedSaltProbe)
	seenProbe := make(map[[32]byte]bool)
	for _, dbPath := range databases {
		page, err := readFirstPage(dbPath)
		if err != nil {
			fmt.Fprintf(warningOutput, "skip fallback salt for %s: %v\n", dbPath, err)
			continue
		}
		if bytes.HasPrefix(page, []byte("SQLite format 3\x00")) {
			continue
		}
		var salt [16]byte
		copy(salt[:], page[:16])
		var lower [32]byte
		hex.Encode(lower[:], salt[:])
		forms := [][32]byte{lower}
		upper := lower
		for i, character := range upper {
			if character >= 'a' && character <= 'f' {
				upper[i] = character - ('a' - 'A')
			}
		}
		if upper != lower {
			forms = append(forms, upper)
		}
		for _, saltHex := range forms {
			if seenProbe[saltHex] {
				continue
			}
			seenProbe[saltHex] = true
			signature := maskedSaltSignature{saltHex[30], saltHex[31], saltHex[0]}
			buckets[signature] = append(buckets[signature], maskedSaltProbe{Salt: salt, SaltHex: saltHex})
		}
	}

	seenKey := make(map[[32]byte]bool)
	var candidates []keyCandidate
	hits := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(warningOutput, "skip %s: %v\n", path, err)
			continue
		}
		found := scanMaskedData(data, buckets)
		hits += len(found)
		for _, candidate := range found {
			if seenKey[candidate.AESKey] {
				continue
			}
			seenKey[candidate.AESKey] = true
			candidates = append(candidates, candidate)
		}
	}
	return candidates, hits
}

func scanMaskedData(data []byte, buckets map[maskedSaltSignature][]maskedSaltProbe) []keyCandidate {
	var found []keyCandidate
	for start := 0; start+keyRecordLen <= len(data); start++ {
		ciphertext := data[start : start+keyRecordLen]
		signature := maskedSaltSignature{
			ciphertext[0] ^ ciphertext[96] ^ 'x',
			ciphertext[1] ^ ciphertext[97] ^ '\'',
			ciphertext[98] ^ ciphertext[66] ^ '\'',
		}
		if !isHex(signature[0]) || !isHex(signature[1]) || !isHex(signature[2]) {
			continue
		}
		for _, probe := range buckets[signature] {
			var mask [32]byte
			for index := range probe.SaltHex {
				mask[(2+index)&31] = ciphertext[66+index] ^ probe.SaltHex[index]
			}
			var plaintext [keyRecordLen]byte
			for index := range plaintext {
				plaintext[index] = ciphertext[index] ^ mask[index&31]
			}
			if matchAt(plaintext[:], 0) == 0 {
				continue
			}
			var payload [48]byte
			if _, err := hex.Decode(payload[:], plaintext[2:98]); err != nil || !bytes.Equal(payload[32:], probe.Salt[:]) {
				continue
			}
			var candidate keyCandidate
			copy(candidate.AESKey[:], payload[:32])
			candidate.SaltHint = probe.Salt
			found = append(found, candidate)
		}
	}
	return found
}

// matchAt reports whether data[i:] starts with x'<96 hex>'.
func matchAt(data []byte, i int) int {
	if i < 0 || i+keyRecordLen > len(data) || data[i] != 'x' || data[i+1] != '\'' {
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
	return keyRecordLen
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
