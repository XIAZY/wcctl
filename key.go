package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func cmdKey(args []string) {
	if err := runKey(args, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fatal("key: %v", err)
	}
}

func runKey(args []string, input io.Reader, output, errorOutput io.Writer) error {
	if len(args) == 0 {
		keyUsage(output)
		return nil
	}
	switch args[0] {
	case "acquire":
		return runKeyAcquire(args[1:], input, output, errorOutput)
	case "extract":
		return runKeyExtract(args[1:], input, output, errorOutput)
	case "help", "-h", "--help":
		keyUsage(output)
		return nil
	default:
		keyUsage(errorOutput)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func keyUsage(output io.Writer) {
	fmt.Fprintln(output, `usage: wcctl key <subcommand>

subcommands:
  acquire  capture WeChat memory and save verified database keys
  extract  extract and verify keys from an existing capture`)
}

type acquireOptions struct {
	PID          int
	DataDir      string
	Account      string
	KeyStorePath string
	OutputDir    string
	ChunkMiB     int
	KeepDump     bool
	Yes          bool
}

var runPrivilegedDump = elevatedDump

func runKeyAcquire(args []string, input io.Reader, output, errorOutput io.Writer) error {
	defaultKeys, err := defaultKeyStorePath()
	if err != nil {
		return fmt.Errorf("resolve key store: %w", err)
	}
	fs := flag.NewFlagSet("key acquire", flag.ContinueOnError)
	fs.SetOutput(errorOutput)
	fs.Usage = func() {
		fmt.Fprintln(errorOutput, "usage: wcctl key acquire [-pid PID] [-data-dir PATH] [-account ACCOUNT] [-keys PATH] [-out DIR] [-chunk N] [-keep-dump] [-yes]")
	}
	options := acquireOptions{}
	fs.IntVar(&options.PID, "pid", 0, "main WeChat PID (default: auto-detect)")
	fs.StringVar(&options.DataDir, "data-dir", "", "WeChat account folder or xwechat_files folder")
	fs.StringVar(&options.Account, "account", "", "WeChat account to verify")
	fs.StringVar(&options.KeyStorePath, "keys", defaultKeys, "path to keys.json")
	fs.StringVar(&options.OutputDir, "out", "", "capture directory (default: private temporary directory)")
	fs.IntVar(&options.ChunkMiB, "chunk", 4, "memory read chunk size in MiB")
	fs.BoolVar(&options.KeepDump, "keep-dump", false, "retain the sensitive memory capture after success")
	fs.BoolVar(&options.Yes, "yes", false, "skip the destructive confirmation")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if options.ChunkMiB <= 0 {
		return fmt.Errorf("-chunk must be greater than zero")
	}
	if os.Geteuid() == 0 {
		return fmt.Errorf("run key acquire as your regular user; it elevates only the memory capture step")
	}

	sip, err := requireSIPDisabled()
	if err != nil {
		return err
	}
	process, processes, err := resolveWeChatProcess(options.PID)
	if err != nil {
		return err
	}
	if process.PID == 0 {
		process, err = chooseWeChatProcess(processes, input, errorOutput)
		if err != nil {
			return err
		}
	}
	account, err := resolveExtractionAccount(options.DataDir, options.Account, input, errorOutput)
	if err != nil {
		return err
	}

	fmt.Fprintf(errorOutput, "SIP: disabled (%s)\n", sip)
	fmt.Fprintf(errorOutput, "WeChat process: %d (%s)\n", process.PID, process.Path)
	fmt.Fprintf(errorOutput, "WeChat account: %s (%s)\n", account.Name, account.Path)
	if !options.Yes {
		confirmed, err := confirmAcquisition(input, errorOutput, process.PID)
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("acquisition cancelled")
		}
	}

	capturePath, automatic, err := prepareCaptureDirectory(options.OutputDir)
	if err != nil {
		return err
	}
	configPath, err := defaultConfigPath()
	if err != nil {
		return fmt.Errorf("resolve config: %w", err)
	}
	fmt.Fprintln(errorOutput, "[1/3] Capturing writable WeChat memory (administrator authorization required)...")
	if err := runPrivilegedDump(configPath, capturePath, process.PID, options.ChunkMiB, os.Getuid(), os.Getgid(), input, errorOutput); err != nil {
		fmt.Fprintf(errorOutput, "capture retained at %s\n", capturePath)
		return err
	}

	fmt.Fprintln(errorOutput, "[2/3] Extracting and verifying database keys...")
	matched, err := extractAndSave(capturePath, account, options.KeyStorePath, output, errorOutput)
	if err != nil {
		fmt.Fprintf(errorOutput, "capture retained at %s\n", capturePath)
		return err
	}
	fmt.Fprintf(errorOutput, "[3/3] Saved %d verified database key(s).\n", matched)

	keepCapture := options.KeepDump || !automatic
	if keepCapture {
		fmt.Fprintf(errorOutput, "sensitive memory capture retained at %s\n", capturePath)
		return nil
	}
	if err := os.RemoveAll(capturePath); err != nil {
		return fmt.Errorf("remove temporary capture %s: %w", capturePath, err)
	}
	fmt.Fprintln(errorOutput, "temporary memory capture deleted")
	return nil
}

func chooseWeChatProcess(processes []processInfo, input io.Reader, output io.Writer) (processInfo, error) {
	if len(processes) == 0 {
		return processInfo{}, fmt.Errorf("WeChat is not running")
	}
	fmt.Fprintln(output, "Multiple main WeChat processes found:")
	for index, process := range processes {
		fmt.Fprintf(output, "  %d) PID %d (%s)\n", index+1, process.PID, process.Path)
	}
	fmt.Fprintf(output, "Select a process [1-%d]: ", len(processes))
	scanner := bufio.NewScanner(input)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return processInfo{}, err
		}
		return processInfo{}, fmt.Errorf("no process selected; pass -pid explicitly")
	}
	selection, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || selection < 1 || selection > len(processes) {
		return processInfo{}, fmt.Errorf("invalid process selection %q", scanner.Text())
	}
	return processes[selection-1], nil
}

func confirmAcquisition(input io.Reader, output io.Writer, pid int) (bool, error) {
	fmt.Fprintf(output, `This operation will freeze and terminate WeChat process %d and its child processes.
Unsaved application state may be lost. Continue? [y/N]: `, pid)
	scanner := bufio.NewScanner(input)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

func prepareCaptureDirectory(requested string) (string, bool, error) {
	if requested == "" {
		path, err := os.MkdirTemp("", "wcctl-capture-*")
		if err != nil {
			return "", false, fmt.Errorf("create temporary capture directory: %w", err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			os.RemoveAll(path)
			return "", false, err
		}
		return path, true, nil
	}
	absolute, err := filepath.Abs(requested)
	if err != nil {
		return "", false, err
	}
	if info, err := os.Lstat(absolute); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", false, fmt.Errorf("capture directory must not be a symbolic link: %s", absolute)
	} else if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	if entries, err := os.ReadDir(absolute); err == nil {
		if len(entries) != 0 {
			return "", false, fmt.Errorf("capture directory %s is not empty", absolute)
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(absolute, 0o700); err != nil {
			return "", false, err
		}
	} else {
		return "", false, err
	}
	if err := os.Chmod(absolute, 0o700); err != nil {
		return "", false, err
	}
	return absolute, false, nil
}

func elevatedDump(configPath, capturePath string, pid, chunkMiB, ownerUID, ownerGID int, input io.Reader, output io.Writer) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve executable symlinks: %w", err)
	}
	arguments := []string{
		"--", executable, "__dump-helper", configPath,
		strconv.Itoa(ownerUID), strconv.Itoa(ownerGID), strconv.Itoa(pid),
		strconv.Itoa(chunkMiB), capturePath,
	}
	command := exec.Command("/usr/bin/sudo", arguments...)
	command.Stdin = input
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		return fmt.Errorf("privileged memory capture failed: %w", err)
	}
	return nil
}

func cmdDumpHelper(args []string) {
	if len(args) != 6 {
		fatal("invalid internal dump-helper arguments")
	}
	ownerUID, uidErr := strconv.Atoi(args[1])
	ownerGID, gidErr := strconv.Atoi(args[2])
	pid, pidErr := strconv.Atoi(args[3])
	chunkMiB, chunkErr := strconv.Atoi(args[4])
	if uidErr != nil || gidErr != nil || pidErr != nil || chunkErr != nil || ownerUID < 0 || ownerGID < 0 {
		fatal("invalid internal dump-helper numeric arguments")
	}
	if os.Geteuid() != 0 {
		fatal("internal dump helper must execute as root")
	}
	if _, _, err := resolveWeChatProcess(pid); err != nil {
		fatal("WeChat process preflight: %v", err)
	}
	capturePath := args[5]
	cmdDump([]string{"-pid", strconv.Itoa(pid), "-out", capturePath, "-chunk", strconv.Itoa(chunkMiB), "-yes"})
	if err := chownCapture(capturePath, ownerUID, ownerGID); err != nil {
		fatal("return capture ownership: %v", err)
	}
}

func chownCapture(root string, uid, gid int) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Lchown(path, uid, gid)
	})
}
