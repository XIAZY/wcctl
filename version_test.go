package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestIsVersionCommand(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{args: []string{"wcctl", "version"}, want: true},
		{args: []string{"wcctl", "version", "extra"}, want: true},
		{args: []string{"wcctl", "license"}, want: false},
		{args: []string{"wcctl"}, want: false},
	} {
		if got := isVersionCommand(test.args); got != test.want {
			t.Fatalf("isVersionCommand(%q) = %v, want %v", test.args, got, test.want)
		}
	}
}

func TestRunVersion(t *testing.T) {
	originalVersion := version
	version = "v1.2.3"
	t.Cleanup(func() { version = originalVersion })

	var output bytes.Buffer
	var errors bytes.Buffer
	if err := runVersion(nil, &output, &errors); err != nil {
		t.Fatalf("runVersion: %v", err)
	}
	if got, want := output.String(), "wcctl v1.2.3\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if errors.Len() != 0 {
		t.Fatalf("unexpected error output: %q", errors.String())
	}
}

func TestRunVersionRejectsArguments(t *testing.T) {
	var output bytes.Buffer
	var errors bytes.Buffer
	err := runVersion([]string{"extra"}, &output, &errors)
	if err == nil {
		t.Fatal("runVersion should reject arguments")
	}
	if !strings.Contains(errors.String(), "usage: wcctl version") {
		t.Fatalf("error output = %q", errors.String())
	}
}
