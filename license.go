package main

import (
	_ "embed"
	"fmt"
	"io"
	"os"
)

//go:embed LICENSE
var licenseText string

func cmdLicense(args []string) {
	if err := runLicense(args, os.Stdout, os.Stderr); err != nil {
		fatal("license: %v", err)
	}
}

func runLicense(args []string, output, errorOutput io.Writer) error {
	if len(args) != 0 {
		fmt.Fprintln(errorOutput, "usage: wcctl license")
		return fmt.Errorf("unexpected arguments")
	}
	_, err := io.WriteString(output, licenseText)
	return err
}

func isLicenseCommand(args []string) bool {
	return len(args) >= 2 && args[1] == "license"
}
