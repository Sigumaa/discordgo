package dave

/*
#cgo CFLAGS: -I${SRCDIR}/../../libdave/cpp/build/install/include
#cgo LDFLAGS: -L${SRCDIR}/../../libdave/cpp/build/install/lib -L${SRCDIR}/../../libdave/cpp/build/vcpkg_installed/arm64-osx/lib -ldave -lmlspp -lmls_vectors -lmls_ds -lbytes -ltls_syntax -lhpke -lssl -lcrypto -lstdc++
#include "dave/dave.h"
#include <stdlib.h>
*/
import "C"
import "unsafe"

// cgoFree frees memory allocated by the libdave C API.
func cgoFree(ptr unsafe.Pointer) {
	C.daveFree(ptr)
}

// MaxSupportedProtocolVersion returns the maximum DAVE protocol version
// supported by the linked libdave.
func MaxSupportedProtocolVersion() uint16 {
	return uint16(C.daveMaxSupportedProtocolVersion())
}
