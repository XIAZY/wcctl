package sqlcipher

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type workingCopy struct {
	path      string
	directory string
}

func makeWorkingCopy(path string) (*workingCopy, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("resolve database symlinks: %w", err)
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("inspect database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("database is not a regular file: %s", resolvedPath)
	}

	directory, err := os.MkdirTemp("", "wcctl-sqlite-*")
	if err != nil {
		return nil, fmt.Errorf("create database working directory: %w", err)
	}
	copy := &workingCopy{
		path:      filepath.Join(directory, filepath.Base(resolvedPath)),
		directory: directory,
	}
	cleanUpOnError := true
	defer func() {
		if cleanUpOnError {
			_ = copy.Close()
		}
	}()

	if err := copyFile(resolvedPath, copy.path); err != nil {
		return nil, fmt.Errorf("copy database: %w", err)
	}
	for _, suffix := range []string{"-wal", "-journal"} {
		source := resolvedPath + suffix
		destination := copy.path + suffix
		if err := copyOptionalFile(source, destination); err != nil {
			return nil, fmt.Errorf("copy database%s: %w", suffix, err)
		}
	}

	cleanUpOnError = false
	return copy, nil
}

func (copy *workingCopy) Close() error {
	if copy == nil || copy.directory == "" {
		return nil
	}
	directory := copy.directory
	copy.directory = ""
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("remove database working directory: %w", err)
	}
	return nil
}

func copyOptionalFile(source, destination string) error {
	info, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", source)
	}
	if err := copyFile(source, destination); errors.Is(err, os.ErrNotExist) {
		// WAL and rollback-journal files can disappear between Stat and the
		// clone attempt. Treat that exactly like an absent sidecar.
		return nil
	} else {
		return err
	}
}

func copyFile(source, destination string) error {
	return copyFileWithClone(source, destination, cloneFile)
}

func copyFileWithClone(source, destination string, clone func(string, string) error) error {
	if err := clone(source, destination); err == nil {
		return makeOwnerWritable(destination)
	} else if !errors.Is(err, syscall.EXDEV) && !errors.Is(err, syscall.ENOTSUP) {
		return err
	}

	// clonefile requires source and destination to share a clone-capable
	// filesystem. Preserve isolation with a regular copy for external or
	// non-APFS database locations.
	return copyFileContents(source, destination)
}

func copyFileContents(source, destination string) (resultErr error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, input.Close())
	}()

	info, err := input.Stat()
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, output.Close())
	}()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	return nil
}

func makeOwnerWritable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.Chmod(path, info.Mode().Perm()|0o600)
}
