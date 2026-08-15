package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var errLicenseNotAccepted = errors.New("license terms not accepted")

type appConfig struct {
	LicenseAccepted   bool      `json:"license_accepted"`
	LicenseAcceptedAt time.Time `json:"license_accepted_at,omitempty"`
	DefaultUser       string    `json:"default_user,omitempty"`
}

func defaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".wcctl", "config.json"), nil
}

func ensureLicenseAcceptance(path string, input io.Reader, output io.Writer) error {
	config, err := readAppConfig(path)
	if err != nil {
		return err
	}
	if config.LicenseAccepted {
		return nil
	}

	fmt.Fprintln(output, `Before using wcctl, please read the LICENSE file.

Confirm all of the following:
  1. You have read and agree to the Data Interoperability Source License v1.0.
  2. You are not physically located in the United States (including its
     territories and possessions), Mainland China, or Hong Kong SAR.
  3. You have lawful access to the data, accounts, databases, files, records,
     and other material you will use with this software.
  4. You will use the software only to achieve interoperability with other
     computer programs, solely for computational data analysis.

Do you confirm all four statements? Type "yes" to continue: `)

	reader := bufio.NewReader(input)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read license confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "yes" && answer != "y" {
		return errLicenseNotAccepted
	}

	config.LicenseAccepted = true
	config.LicenseAcceptedAt = time.Now().UTC()
	if err := saveAppConfig(path, config); err != nil {
		return fmt.Errorf("save license confirmation: %w", err)
	}
	return nil
}

func readAppConfig(path string) (appConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return appConfig{}, nil
	}
	if err != nil {
		return appConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var config appConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return appConfig{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return config, nil
}

func saveAppConfig(path string, config appConfig) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(directory, ".config-*.json")
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
	if err := encoder.Encode(config); err != nil {
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
