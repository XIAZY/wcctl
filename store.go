package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type keyStore struct {
	Users map[string]storedUser `json:"users"`
}

type storedUser struct {
	Databases map[string]storedDatabase `json:"databases"`
}

type storedDatabase struct {
	Account   string    `json:"-"`
	AESKey    string    `json:"aes_key"`
	UpdatedAt time.Time `json:"updated_at"`
}

func newStoredDatabase(account string, aesKey []byte) storedDatabase {
	return storedDatabase{
		Account:   account,
		AESKey:    hex.EncodeToString(aesKey),
		UpdatedAt: time.Now().UTC(),
	}
}

func defaultKeyStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".wcctl", "keys.json"), nil
}

func saveDatabaseKeys(path string, matches map[string]storedDatabase) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}

	store := keyStore{Users: make(map[string]storedUser)}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &store); err != nil {
			return fmt.Errorf("read existing key store: %w", err)
		}
		if store.Users == nil {
			store.Users = make(map[string]storedUser)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for dbPath, match := range matches {
		if match.Account == "" {
			return fmt.Errorf("database %q has no account", dbPath)
		}
		absolutePath, err := filepath.Abs(dbPath)
		if err != nil {
			return err
		}
		user := store.Users[match.Account]
		if user.Databases == nil {
			user.Databases = make(map[string]storedDatabase)
		}
		user.Databases[absolutePath] = match
		store.Users[match.Account] = user
	}

	temporary, err := os.CreateTemp(directory, ".keys-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		temporary.Close()
		os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(store); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryPath)
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		os.Remove(temporaryPath)
		return err
	}
	return os.Chmod(path, 0o600)
}
