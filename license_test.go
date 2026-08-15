package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestEmbeddedLicenseMatchesLicenseFile(t *testing.T) {
	want, err := os.ReadFile("LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	if licenseText != string(want) {
		t.Fatal("embedded license does not match LICENSE")
	}
}

func TestRunLicensePrintsEmbeddedLicense(t *testing.T) {
	var output, errors bytes.Buffer
	if err := runLicense(nil, &output, &errors); err != nil {
		t.Fatal(err)
	}
	if output.String() != licenseText {
		t.Fatal("license output does not match embedded license")
	}
	if errors.Len() != 0 {
		t.Fatalf("unexpected error output: %s", errors.String())
	}
}

func TestRunLicenseRejectsArguments(t *testing.T) {
	var errors bytes.Buffer
	err := runLicense([]string{"extra"}, &bytes.Buffer{}, &errors)
	if err == nil || !strings.Contains(errors.String(), "wcctl license") {
		t.Fatalf("unexpected result: err=%v output=%q", err, errors.String())
	}
}

func TestLicenseCommandBypassesAcceptance(t *testing.T) {
	if !isLicenseCommand([]string{"wcctl", "license"}) {
		t.Fatal("license command should bypass acceptance so its terms can be read")
	}
	if isLicenseCommand([]string{"wcctl", "contacts"}) {
		t.Fatal("non-license command bypassed acceptance")
	}
}
