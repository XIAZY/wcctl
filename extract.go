// extract.go implements extract-key: locate WeChat v4 AES material in a memory
// dump, validate it against an account's encrypted databases, and persist only
// cryptographically verified database/key associations.
package main

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
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
	fs := flag.NewFlagSet("extract-key", flag.ExitOnError)
	in := fs.String("in", "./dumps", "dump output directory or a single binary file")
	dataDir := fs.String("data-dir", "", "WeChat account folder or xwechat_files folder (default: macOS WeChat 4.x location)")
	fs.Parse(args)

	files, err := inputFiles(*in)
	if err != nil {
		fatal("input: %v", err)
	}
	candidates, hits := extractCandidates(files)
	if len(candidates) == 0 {
		fatal("no x'<96 hex>' AES candidates found in %s", *in)
	}
	fmt.Fprintf(os.Stderr, "found %d hit(s), %d unique AES candidate(s)\n", hits, len(candidates))

	root := *dataDir
	if root == "" {
		root, err = defaultWeChatDataDir()
		if err != nil {
			fatal("resolve default WeChat data directory: %v", err)
		}
	}
	accounts, err := discoverAccounts(root)
	if err != nil {
		fatal("discover WeChat accounts: %v", err)
	}
	account, err := chooseAccount(accounts, os.Stdin, os.Stderr)
	if err != nil {
		fatal("select WeChat account: %v", err)
	}

	databases, err := discoverDatabases(account.DatabaseRoot)
	if err != nil {
		fatal("discover databases: %v", err)
	}
	fmt.Fprintf(os.Stderr, "testing %d candidate(s) against %d database(s) for %s\n",
		len(candidates), len(databases), account.Name)

	matches := make(map[string]storedDatabase)
	for _, dbPath := range databases {
		candidate, ok, err := findDatabaseKey(dbPath, candidates)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", dbPath, err)
			continue
		}
		if !ok {
			continue
		}
		matches[dbPath] = newStoredDatabase(account.Name, candidate.AESKey[:])
		fmt.Printf("matched %s\n", dbPath)
	}
	if len(matches) == 0 {
		fatal("none of the extracted AES candidates matched databases under %s", account.DatabaseRoot)
	}

	storePath, err := defaultKeyStorePath()
	if err != nil {
		fatal("resolve key store: %v", err)
	}
	if err := saveDatabaseKeys(storePath, matches); err != nil {
		fatal("save verified keys: %v", err)
	}
	fmt.Fprintf(os.Stderr, "saved %d verified database key(s) to %s\n", len(matches), storePath)
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

func extractCandidates(files []string) ([]keyCandidate, int) {
	seen := make(map[[32]byte]bool)
	var candidates []keyCandidate
	hits := 0
	for _, path := range files {
		found, err := scanFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", path, err)
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
