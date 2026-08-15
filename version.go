package main

import (
	"fmt"
	"io"
	"os"
)

// version is replaced by the release build with:
//
//	-ldflags "-X main.version=v0.0.0"
var version = "dev"

func isVersionCommand(args []string) bool {
	return len(args) >= 2 && args[1] == "version"
}

func cmdVersion(args []string) {
	if err := runVersion(args, os.Stdout, os.Stderr); err != nil {
		fatal("version: %v", err)
	}
}

func runVersion(args []string, output, errorOutput io.Writer) error {
	if len(args) != 0 {
		fmt.Fprintln(errorOutput, "usage: wcctl version")
		return fmt.Errorf("unexpected arguments")
	}
	_, err := fmt.Fprintf(output, "wcctl %s\n", version)
	return err
}
