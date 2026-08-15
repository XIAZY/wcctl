package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

type storedUserSelection struct {
	Name   string
	User   storedUser
	Source string
}

func resolveStoredUser(store keyStore, requested, configuredDefault string) (storedUserSelection, error) {
	if requested != "" {
		user, ok := store.Users[requested]
		if !ok {
			return storedUserSelection{}, fmt.Errorf("user %q is not in the key store", requested)
		}
		return storedUserSelection{Name: requested, User: user, Source: "explicit"}, nil
	}
	if configuredDefault != "" {
		user, ok := store.Users[configuredDefault]
		if !ok {
			return storedUserSelection{}, fmt.Errorf("configured default user %q is not in the key store; run 'wcctl user use USER' to select another", configuredDefault)
		}
		return storedUserSelection{Name: configuredDefault, User: user, Source: "default"}, nil
	}
	if len(store.Users) == 1 {
		for name, user := range store.Users {
			return storedUserSelection{Name: name, User: user, Source: "only"}, nil
		}
	}
	names := sortedStoredUserNames(store)
	if len(names) == 0 {
		return storedUserSelection{}, fmt.Errorf("key store contains no users")
	}
	return storedUserSelection{}, fmt.Errorf("multiple users in key store; pass -user or run 'wcctl user use USER'; available users: %s", strings.Join(names, ", "))
}

func sortedStoredUserNames(store keyStore) []string {
	names := make([]string, 0, len(store.Users))
	for name := range store.Users {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func cmdUser(args []string) {
	if err := runUser(args, os.Stdout); err != nil {
		fatal("user: %v", err)
	}
}

func runUser(args []string, output io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		userUsage(output)
		return nil
	}
	switch args[0] {
	case "ls":
		return runUserList(args[1:], output)
	case "current":
		return runUserCurrent(args[1:], output)
	case "use":
		return runUserUse(args[1:], output)
	case "clear":
		return runUserClear(args[1:], output)
	default:
		return fmt.Errorf("unknown subcommand %q; run 'wcctl user' for usage", args[0])
	}
}

func userUsage(output io.Writer) {
	fmt.Fprintln(output, `usage: wcctl user <subcommand>

subcommands:
  ls       list users in the key store
  current  print the effective user
  use      set the persistent default user
  clear    clear the persistent default user`)
}

func runUserList(args []string, output io.Writer) error {
	keyStorePath, _, err := userKeyStoreFlagSet("user ls", args, output)
	if err != nil || keyStorePath == "" {
		return err
	}
	store, config, err := readUserState(keyStorePath)
	if err != nil {
		return err
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "DEFAULT\tUSER\tDATABASES"); err != nil {
		return err
	}
	for _, name := range sortedStoredUserNames(store) {
		marker := ""
		if name == config.DefaultUser {
			marker = "*"
		}
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%d\n", marker, name, len(store.Users[name].Databases)); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if config.DefaultUser != "" {
		if _, ok := store.Users[config.DefaultUser]; !ok {
			fmt.Fprintf(output, "Configured default %q is not in this key store.\n", config.DefaultUser)
		}
	}
	return nil
}

func runUserCurrent(args []string, output io.Writer) error {
	keyStorePath, _, err := userKeyStoreFlagSet("user current", args, output)
	if err != nil || keyStorePath == "" {
		return err
	}
	store, config, err := readUserState(keyStorePath)
	if err != nil {
		return err
	}
	selection, err := resolveStoredUser(store, "", config.DefaultUser)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, selection.Name)
	return err
}

func runUserUse(args []string, output io.Writer) error {
	defaultKeys, err := defaultKeyStorePath()
	if err != nil {
		return fmt.Errorf("resolve key store: %w", err)
	}
	fs := flag.NewFlagSet("user use", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() {
		fmt.Fprintln(output, "usage: wcctl user use [-keys PATH] USER")
		fs.PrintDefaults()
	}
	keyStorePath := fs.String("keys", defaultKeys, "path to keys.json")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("user use requires exactly one USER argument")
	}
	store, config, err := readUserState(*keyStorePath)
	if err != nil {
		return err
	}
	name := fs.Arg(0)
	if _, ok := store.Users[name]; !ok {
		return fmt.Errorf("user %q is not in the key store; available users: %s", name, strings.Join(sortedStoredUserNames(store), ", "))
	}
	config.DefaultUser = name
	if err := writeCurrentConfig(config); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "Default user set to %s.\n", name)
	return err
}

func runUserClear(args []string, output io.Writer) error {
	fs := flag.NewFlagSet("user clear", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() { fmt.Fprintln(output, "usage: wcctl user clear") }
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	configPath, err := defaultConfigPath()
	if err != nil {
		return fmt.Errorf("resolve config: %w", err)
	}
	config, err := readAppConfig(configPath)
	if err != nil {
		return err
	}
	config.DefaultUser = ""
	if err := saveAppConfig(configPath, config); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	_, err = fmt.Fprintln(output, "Default user cleared.")
	return err
}

func userKeyStoreFlagSet(name string, args []string, output io.Writer) (string, *flag.FlagSet, error) {
	defaultKeys, err := defaultKeyStorePath()
	if err != nil {
		return "", nil, fmt.Errorf("resolve key store: %w", err)
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() {
		fmt.Fprintf(output, "usage: wcctl %s [-keys PATH]\n", name)
		fs.PrintDefaults()
	}
	keyStorePath := fs.String("keys", defaultKeys, "path to keys.json")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return "", fs, nil
		}
		return "", fs, err
	}
	if fs.NArg() != 0 {
		return "", fs, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return *keyStorePath, fs, nil
}

func readUserState(keyStorePath string) (keyStore, appConfig, error) {
	store, err := readKeyStore(keyStorePath)
	if err != nil {
		return keyStore{}, appConfig{}, err
	}
	configPath, err := defaultConfigPath()
	if err != nil {
		return keyStore{}, appConfig{}, fmt.Errorf("resolve config: %w", err)
	}
	config, err := readAppConfig(configPath)
	if err != nil {
		return keyStore{}, appConfig{}, err
	}
	return store, config, nil
}

func writeCurrentConfig(config appConfig) error {
	configPath, err := defaultConfigPath()
	if err != nil {
		return fmt.Errorf("resolve config: %w", err)
	}
	if err := saveAppConfig(configPath, config); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}
