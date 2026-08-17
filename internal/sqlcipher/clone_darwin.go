package sqlcipher

/*
#include <errno.h>
#include <stdlib.h>
#include <sys/clonefile.h>

static int wcctl_clonefile(const char *source, const char *destination) {
	if (clonefile(source, destination, 0) == 0) {
		return 0;
	}
	return errno;
}
*/
import "C"

import (
	"os"
	"syscall"
	"unsafe"
)

func cloneFile(source, destination string) error {
	cSource := C.CString(source)
	defer C.free(unsafe.Pointer(cSource))
	cDestination := C.CString(destination)
	defer C.free(unsafe.Pointer(cDestination))

	if code := C.wcctl_clonefile(cSource, cDestination); code != 0 {
		return os.NewSyscallError("clonefile", syscall.Errno(code))
	}
	return nil
}
