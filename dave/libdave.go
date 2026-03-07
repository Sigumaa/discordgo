package dave

/*
#cgo CFLAGS: -I${SRCDIR}/libdave_c
#cgo LDFLAGS: -L${SRCDIR}/libdave_c/build -ldave_c -ldave -lmlspp -lmls_vectors -lbytes -ltls_syntax -lhpke -lssl -lcrypto -lstdc++
#include "libdave_c.h"
#include <stdlib.h>
*/
import "C"
import "unsafe"

// cgoFree frees memory allocated by the C wrapper.
func cgoFree(ptr unsafe.Pointer) {
	C.dave_free(ptr)
}

// MaxSupportedProtocolVersion returns the maximum DAVE protocol version
// supported by the linked libdave.
func MaxSupportedProtocolVersion() uint16 {
	return uint16(C.dave_max_supported_protocol_version())
}
