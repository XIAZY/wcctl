// wcctl — forensic acquisition and WeChat database key discovery tool.
//
// Subcommands:
//
//	dump         freeze a process tree, dump the root process's memory, then terminate (dump.go)
//	key          acquire or extract verified WeChat database keys (key.go, extract.go)
//	contact      query contacts from a keyed WeChat contact database (contact.go)
//	chatroom     query chatrooms from a keyed WeChat contact database (chatroom.go)
//	message      query messages across keyed WeChat message shards (message.go)
//	session      query recent conversations from a keyed WeChat session database (session.go)
//	user         inspect and select users from the key store (user.go)
//
// Layout: main.go handles dispatch and shared helpers; mach.go wraps all cgo/Mach calls.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

var logOut io.Writer = os.Stderr

func audit(format string, args ...any) {
	fmt.Fprintf(logOut, "[%s] %s\n", time.Now().Format("15:04:05.000"),
		fmt.Sprintf(format, args...))
}

func fatal(format string, args ...any) {
	audit("FATAL: "+format, args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `wcctl — acquire process memory and identify WeChat database AES keys

subcommands:
  key          wcctl key acquire | wcctl key extract -capture PATH
  dump         wcctl dump -pid <PID> [-out DIR] [-full] [-shared] [-chunk N] [-meta] [-yes]
  contact      wcctl contact ls [-user USER] [-json] [-keys PATH]
  chatroom     wcctl chatroom ls [-user USER] [-json] [-keys PATH]
  message      wcctl message ls -chat USERNAME [-limit N] [-before TIME] [-user USER] [-json] [-keys PATH]
  session      wcctl session ls [-limit N] [-user USER] [-json] [-keys PATH]
  user         wcctl user <ls|current|use|clear>`)
}

func main() {
	configPath, err := startupConfigPath(os.Args)
	if err != nil {
		fatal("resolve config: %v", err)
	}
	if err := ensureLicenseAcceptance(configPath, os.Stdin, os.Stderr); err != nil {
		if errors.Is(err, errLicenseNotAccepted) {
			fmt.Fprintln(os.Stderr, "License confirmation declined; exiting.")
			os.Exit(1)
		}
		fatal("license confirmation: %v", err)
	}

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "key":
		cmdKey(os.Args[2:])
	case "dump":
		cmdDump(os.Args[2:])
	case "extract-key":
		fmt.Fprintln(os.Stderr, "warning: extract-key is deprecated; use `wcctl key extract`")
		cmdExtractKey(os.Args[2:])
	case "__dump-helper":
		cmdDumpHelper(os.Args[2:])
	case "contact":
		cmdContact(os.Args[2:])
	case "chatroom":
		cmdChatRoom(os.Args[2:])
	case "message":
		cmdMessage(os.Args[2:])
	case "session":
		cmdSession(os.Args[2:])
	case "user":
		cmdUser(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func startupConfigPath(args []string) (string, error) {
	if len(args) > 2 && args[1] == "__dump-helper" {
		return args[2], nil
	}
	return defaultConfigPath()
}
