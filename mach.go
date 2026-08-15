// mach.go — the wrapper layer for all cgo / Mach calls.
// main.go never imports "C" and never touches unsafe; every cross-language
// type conversion is contained in this file.
package main

/*
#include <stdlib.h>
#include <string.h>
#include <mach/mach.h>
#include <mach/mach_vm.h>
#include <libproc.h>

// Everything about one region, filled in a single call so the Go side
// does zero assembly work.
typedef struct {
    uint64_t start;      // region start address (target virtual address)
    uint64_t size;       // region size in bytes (page-aligned)
    int32_t  prot;       // VM_PROT_* bitmask
    int32_t  user_tag;   // VM_MEMORY_* tag (32 == VM_MEMORY_SHARED_PMAP)
    int32_t  share_mode; // SM_* share mode
    char     prot_str[4];// "rwx" + NUL
    char     path[4096]; // backing file path; empty for anonymous mappings
} region_t;

// Thin task_for_pid wrapper (cgo cannot use the mach_task_self() macro;
// it must be called from real C).
static kern_return_t wc_task_for_pid(int pid, mach_port_t *out) {
    return task_for_pid(mach_task_self(), (pid_t)pid, out);
}

// Iterate the target's VM map: find the next region at or above cursor and
// fill in all metadata. Combines three operations: mach_vm_region (region +
// extended info), protection-bits to string, and proc_regionfilename.
static kern_return_t wc_next_region(mach_port_t task, int pid, uint64_t cursor, region_t *r) {
    mach_vm_address_t addr = (mach_vm_address_t)cursor;
    mach_vm_size_t size = 0;
    vm_region_extended_info_data_t info;
    mach_msg_type_number_t count = VM_REGION_EXTENDED_INFO_COUNT;
    mach_port_t obj = MACH_PORT_NULL;
    kern_return_t kr = mach_vm_region(task, &addr, &size, VM_REGION_EXTENDED_INFO,
                                      (vm_region_info_t)&info, &count, &obj);
    if (kr != KERN_SUCCESS) return kr;
    r->start      = (uint64_t)addr;
    r->size       = (uint64_t)size;
    r->prot       = (int32_t)info.protection;
    r->user_tag   = (int32_t)info.user_tag;
    r->share_mode = (int32_t)info.share_mode;
    r->prot_str[0] = (info.protection & VM_PROT_READ)    ? 'r' : '-';
    r->prot_str[1] = (info.protection & VM_PROT_WRITE)   ? 'w' : '-';
    r->prot_str[2] = (info.protection & VM_PROT_EXECUTE) ? 'x' : '-';
    r->prot_str[3] = 0;
    r->path[0] = 0;
    proc_regionfilename((pid_t)pid, (uint64_t)addr, r->path, (uint32_t)sizeof(r->path));
    return KERN_SUCCESS;
}

// Read len bytes starting at addr in the target directly into the caller's
// buffer. mach_vm_read_overwrite writes into an existing buffer, so the
// kernel side needs no allocate/deallocate round-trip.
static kern_return_t wc_read(mach_port_t task, uint64_t addr, void *buf, uint64_t len, uint32_t *out_len) {
    mach_vm_size_t out = 0;
    kern_return_t kr = mach_vm_read_overwrite(task, (mach_vm_address_t)addr,
                                              (mach_vm_size_t)len,
                                              (mach_vm_address_t)buf, &out);
    if (kr != KERN_SUCCESS) return kr;
    *out_len = (uint32_t)out;
    return KERN_SUCCESS;
}

// TASK_EXTMOD_INFO (flavor 19): counters of external operations against the
// task (task_for_pid calls, remote thread creation, ...).
static kern_return_t wc_extmod(mach_port_t task, int64_t out[6]) {
    task_extmod_info_data_t info;
    mach_msg_type_number_t count = TASK_EXTMOD_INFO_COUNT;
    kern_return_t kr = task_info(task, TASK_EXTMOD_INFO, (task_info_t)&info, &count);
    if (kr != KERN_SUCCESS) return kr;
    memcpy(out, &info.extmod_statistics, 6 * sizeof(int64_t));
    return KERN_SUCCESS;
}

// Child PID list. Note: proc_listchildpids returns a pid *count*
// (unlike proc_listpids, which returns a byte count).
static int wc_childpids(int pid, int *buf, int n) {
    int r = proc_listchildpids((pid_t)pid, (void *)buf, n * (int)sizeof(int));
    return r > 0 ? r : 0;
}

static int wc_pname(int pid, char *buf, int n) {
    return proc_name((pid_t)pid, buf, (uint32_t)n);
}

static int wc_allpids(int *buf, int n) {
    return proc_listallpids((void *)buf, n * (int)sizeof(int));
}

static int wc_pidpath(int pid, char *buf, int n) {
    return proc_pidpath((pid_t)pid, (void *)buf, (uint32_t)n);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

var errAddressSpaceEnd = errors.New("end of process address space")

// taskPort is an alias of C.mach_port_t so main.go can hold a task port
// without importing "C".
type taskPort = C.mach_port_t

// region is the Go-facing view of region_t.
type region struct {
	Start, Size uint64
	ProtMask    int32
	Prot        string
	Tag, Share  int
	Path        string
}

// Writable reports whether the region is writable. Data memory
// (heap/stack/__DATA/malloc zones) is writable; code (r-x) and read-only
// file mappings (r--) are not. VM_PROT_WRITE == 0x2 (stable ABI).
func (r *region) Writable() bool { return r.ProtMask&0x2 != 0 }

// acquireTask gets the target's task port (the single authorization gate).
func acquireTask(pid int) (taskPort, error) {
	var p C.mach_port_t
	if kr := C.wc_task_for_pid(C.int(pid), &p); kr != C.KERN_SUCCESS {
		return 0, fmt.Errorf("kr=%d", int(kr))
	}
	return p, nil
}

// nextRegion returns the region at or above cursor; error means the address
// space has been fully walked.
func nextRegion(port taskPort, pid int, cursor uint64) (*region, error) {
	var r C.region_t
	kr := C.wc_next_region(port, C.int(pid), C.uint64_t(cursor), &r)
	if kr == C.KERN_INVALID_ADDRESS {
		return nil, errAddressSpaceEnd
	}
	if kr != C.KERN_SUCCESS {
		return nil, fmt.Errorf("kr=%d", int(kr))
	}
	return &region{
		Start:    uint64(r.start),
		Size:     uint64(r.size),
		ProtMask: int32(r.prot),
		Prot:     C.GoString(&r.prot_str[0]),
		Tag:      int(r.user_tag),
		Share:    int(r.share_mode),
		Path:     C.GoString(&r.path[0]),
	}, nil
}

// readChunk reads the target at addr into buf and returns the byte count.
// The only unsafe in the entire project (Go slice → C void*).
func readChunk(port taskPort, addr uint64, buf []byte) (int, error) {
	var n C.uint32_t
	kr := C.wc_read(port, C.uint64_t(addr), unsafe.Pointer(&buf[0]),
		C.uint64_t(len(buf)), &n)
	if kr != C.KERN_SUCCESS {
		return 0, fmt.Errorf("kr=%d", int(kr))
	}
	return int(n), nil
}

// extmodCounts summarizes external-modification counters of the target (audit).
func extmodCounts(port taskPort) string {
	var v [6]C.int64_t
	if C.wc_extmod(port, &v[0]) != C.KERN_SUCCESS {
		return "unavailable"
	}
	return fmt.Sprintf("task_for_pid=%d thread_create=%d thread_set_state=%d",
		int64(v[0]), int64(v[2]), int64(v[4]))
}

// childPIDs returns the direct children of pid.
func childPIDs(pid int) []int {
	buf := make([]C.int, 4096)
	n := int(C.wc_childpids(C.int(pid), &buf[0], 4096))
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, int(buf[i]))
	}
	return out
}

func procName(pid int) string {
	buf := make([]C.char, 256)
	if C.wc_pname(C.int(pid), &buf[0], 256) <= 0 {
		return "?"
	}
	return C.GoString(&buf[0])
}

func allPIDs() []int {
	buf := make([]C.int, 16384)
	n := int(C.wc_allpids(&buf[0], C.int(len(buf))))
	if n <= 0 {
		return nil
	}
	if n > len(buf) {
		n = len(buf)
	}
	result := make([]int, 0, n)
	for index := 0; index < n; index++ {
		if buf[index] > 0 {
			result = append(result, int(buf[index]))
		}
	}
	return result
}

func procPath(pid int) string {
	buf := make([]C.char, 4096)
	if C.wc_pidpath(C.int(pid), &buf[0], C.int(len(buf))) <= 0 {
		return ""
	}
	return C.GoString(&buf[0])
}
