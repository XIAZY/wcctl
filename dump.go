// dump.go — the dump subcommand: freeze a process tree → dump the root process's memory → terminate.
//
// Design (each property verified empirically on this machine):
//   - Freeze first (SIGSTOP the whole tree, root process first) for snapshot consistency;
//   - while frozen the target executes no code, so in-process self-checks
//     (e.g. a TASK_EXTMOD_INFO watchdog) cannot observe anything;
//   - SIGKILL after the dump: counters and wall-clock evidence lose their reader
//     when the task is destroyed;
//   - no path ever resumes (SIGCONT): resuming is the only action in the chain that
//     creates in-process perception — the failure path kills too;
//   - the tool does not hide itself: ES agents see SIGNAL/GET_TASK events, and every
//     action is written to an audit log.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

type regionMeta struct {
	Start  uint64   `json:"start"`
	End    uint64   `json:"end"`
	Prot   string   `json:"prot"`
	Path   string   `json:"path,omitempty"`
	Tag    int      `json:"tag,omitempty"`
	Share  int      `json:"share,omitempty"`
	File   string   `json:"file,omitempty"`
	Bytes  int64    `json:"bytes"`
	SHA256 string   `json:"sha256,omitempty"`
	Errors []string `json:"errors,omitempty"`
}

// treePreOrder returns the process tree root-first.
// The root is frozen first — it is the dump and termination target, so it is
// blinded first. Sequential freezing has an inherent race window (see README);
// it is bounded by per-process syscall latency.
func treePreOrder(root int) []int {
	out := []int{root}
	for _, c := range childPIDs(root) {
		out = append(out, treePreOrder(c)...)
	}
	return out
}

func cmdDump(args []string) {
	fs := flag.NewFlagSet("dump", flag.ExitOnError)
	pidFlag := fs.Int("pid", 0, "target process PID (required)")
	outFlag := fs.String("out", "./dumps", "output directory (refuses a non-empty directory, protecting prior captures)")
	chunkMiB := fs.Int("chunk", 4, "size of a single read chunk (MiB)")
	metaOnly := fs.Bool("meta", false, "enumerate region metadata only, do not read contents")
	fullDump := fs.Bool("full", false, "dump all readable regions (default dumps data memory only: writable regions)")
	incShared := fs.Bool("shared", false, "include dyld shared cache regions (several GB, skipped by default)")
	fs.Parse(args)

	root := *pidFlag
	if root <= 0 {
		fatal("-pid is required")
	}
	// Fail fast: verify privileges before touching anything (including freezing).
	// Running non-root would otherwise discover the task_for_pid denial only after
	// freezing — and kill the target for nothing on the failure path.
	if os.Geteuid() != 0 {
		fatal("dump requires root privileges, run with sudo (current euid=%d)", os.Geteuid())
	}
	out := *outFlag
	if entries, err := os.ReadDir(out); err == nil && len(entries) > 0 {
		fatal("output directory %s already exists and is not empty — pass -out with a different directory, or clean it first", out)
	}
	segDir := filepath.Join(out, "segments")
	if err := os.MkdirAll(segDir, 0o755); err != nil {
		fatal("mkdir: %v", err)
	}
	lf, err := os.Create(filepath.Join(out, "audit.log"))
	if err != nil {
		fatal("audit.log: %v", err)
	}
	defer lf.Close()
	logOut = io.MultiWriter(os.Stderr, lf)

	audit("wcctl start: pid=%d out=%s euid=%d", root, out, os.Geteuid())
	if s, err := exec.Command("csrutil", "status").Output(); err == nil {
		audit("env: %s", strings.TrimSpace(string(s)))
	}

	// ---- 1. Enumerate and freeze the tree (root first) ----
	tree := treePreOrder(root)
	audit("tree: %d process(es)", len(tree))
	stopped := []int{}
	for _, p := range tree {
		if err := syscall.Kill(p, syscall.SIGSTOP); err != nil {
			audit("SIGSTOP %-6d (%s): FAIL %v", p, procName(p), err)
		} else {
			audit("SIGSTOP %-6d (%s): ok", p, procName(p))
			stopped = append(stopped, p)
		}
	}
	// Failure path kills as well: no resume, since SIGCONT is the only
	// perception-producing action in the chain.
	killStopped := func() {
		for i := len(stopped) - 1; i >= 0; i-- {
			syscall.Kill(stopped[i], syscall.SIGKILL)
		}
		audit("killed all frozen processes (failure path: killed, not resumed)")
	}

	// ---- 2. Acquire the task port ----
	port, err := acquireTask(root)
	if err != nil {
		audit("task_for_pid(%d): %v", root, err)
		killStopped()
		fatal("cannot acquire task port (hardened targets require root + SIP disabled, or a target built with get-task-allow)")
	}
	audit("task_for_pid(%d): ok | extmod(before): %s", root, extmodCounts(port))

	// ---- 3. Walk the address space and dump ----
	metaF, err := os.Create(filepath.Join(out, "regions.jsonl"))
	if err != nil {
		killStopped()
		fatal("regions.jsonl: %v", err)
	}
	defer metaF.Close()
	enc := json.NewEncoder(metaF)

	buf := make([]byte, *chunkMiB<<20) // single reused read buffer for the whole run
	var total int64
	var regions, skipped int
	for cursor := uint64(0); ; {
		reg, err := nextRegion(port, root, cursor)
		if err != nil {
			break // end of the address space
		}
		regions++
		m := regionMeta{
			Start: reg.Start, End: reg.Start + reg.Size, Prot: reg.Prot,
			Path: reg.Path, Tag: reg.Tag, Share: reg.Share,
		}

		// Default: dump data memory only — writable regions (heap/stack/__DATA).
		// Code (r-x) and read-only file mappings (r--) are recoverable from disk
		// and skipped (-full includes them); dyld shared cache (path match or
		// tag == VM_MEMORY_SHARED_PMAP) is skipped by default (-shared includes it).
		isCache := strings.Contains(reg.Path, "dyld_shared_cache") || reg.Tag == 32
		readable := !*metaOnly && reg.ProtMask != 0 && reg.Size > 0 &&
			(*incShared || !isCache) && (*fullDump || reg.Writable())
		if readable {
			readRegion(port, reg, out, buf, &m)
			total += m.Bytes
		} else {
			skipped++
		}
		enc.Encode(m) // stream one JSON line per region as we go
		cursor = reg.Start + reg.Size
	}
	audit("dump: %d regions (%d skipped), %d bytes -> %s", regions, skipped, total, segDir)
	audit("extmod(after): %s", extmodCounts(port))

	// ---- 4. Terminate: children first, root last; all frozen, no code runs again ----
	for i := len(tree) - 1; i >= 0; i-- {
		p := tree[i]
		if err := syscall.Kill(p, syscall.SIGKILL); err != nil {
			audit("SIGKILL %-6d (%s): FAIL %v", p, procName(p), err)
		} else if p == root {
			audit("SIGKILL %-6d (main): ok — no crash report, counters destroyed with the task", p)
		} else {
			audit("SIGKILL %-6d (%s): ok", p, procName(p))
		}
	}
	audit("done: %s", out)
}

// readRegion reads one region in chunks into segments/<start>.bin and back-fills
// the metadata. Chunking caps peak memory at one chunk regardless of target size,
// and isolates read failures to a single chunk instead of losing the whole region.
func readRegion(port taskPort, reg *region, out string, buf []byte, m *regionMeta) {
	fname := fmt.Sprintf("segments/%x.bin", reg.Start)
	f, err := os.Create(filepath.Join(out, fname))
	if err != nil {
		m.Errors = append(m.Errors, "create: "+err.Error())
		return
	}
	defer f.Close()
	h := sha256.New()
	for off := uint64(0); off < reg.Size; off += uint64(len(buf)) {
		want := buf
		if rem := reg.Size - off; rem < uint64(len(buf)) {
			want = buf[:rem] // final chunk: truncate to the region end
		}
		n, err := readChunk(port, reg.Start+off, want)
		if err != nil {
			m.Errors = append(m.Errors, fmt.Sprintf("off=0x%x %v", off, err))
			continue
		}
		f.Write(want[:n])
		h.Write(want[:n])
		m.Bytes += int64(n)
	}
	m.File = fname
	m.SHA256 = hex.EncodeToString(h.Sum(nil))
}
