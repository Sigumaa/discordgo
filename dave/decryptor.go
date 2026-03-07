package dave

/*
#include "dave/dave.h"
*/
import "C"
import (
	"fmt"
	"runtime"
	"unsafe"
)

// Decryptor wraps a libdave frame decryptor.
type Decryptor struct {
	h C.DAVEDecryptorHandle
}

// NewDecryptor creates a new DAVE frame decryptor.
func NewDecryptor() *Decryptor {
	d := &Decryptor{h: C.daveDecryptorCreate()}
	runtime.SetFinalizer(d, (*Decryptor).Close)
	return d
}

// Decrypt decrypts a DAVE-encrypted frame.
func (d *Decryptor) Decrypt(mediaType int, encryptedFrame []byte) ([]byte, error) {
	if len(encryptedFrame) == 0 {
		return encryptedFrame, nil
	}

	maxSize := C.daveDecryptorGetMaxPlaintextByteSize(
		d.h,
		C.DAVEMediaType(mediaType),
		C.size_t(len(encryptedFrame)),
	)

	out := make([]byte, maxSize)
	var bytesWritten C.size_t

	rc := C.daveDecryptorDecrypt(
		d.h,
		C.DAVEMediaType(mediaType),
		(*C.uint8_t)(unsafe.Pointer(&encryptedFrame[0])),
		C.size_t(len(encryptedFrame)),
		(*C.uint8_t)(unsafe.Pointer(&out[0])),
		maxSize,
		&bytesWritten,
	)

	if rc != C.DAVE_DECRYPTOR_RESULT_CODE_SUCCESS {
		return nil, fmt.Errorf("dave: decrypt failed with code %d", rc)
	}

	return out[:bytesWritten], nil
}

// TransitionToKeyRatchet transitions the decryptor to a new key ratchet.
// The decryptor does NOT take ownership; the caller must keep the KeyRatchet alive.
func (d *Decryptor) TransitionToKeyRatchet(kr *KeyRatchet) {
	if kr == nil {
		return
	}
	C.daveDecryptorTransitionToKeyRatchet(d.h, kr.h)
}

// TransitionToPassthroughMode transitions the decryptor to/from passthrough.
func (d *Decryptor) TransitionToPassthroughMode(passthrough bool) {
	C.daveDecryptorTransitionToPassthroughMode(d.h, C.bool(passthrough))
}

// Close destroys the decryptor.
func (d *Decryptor) Close() {
	if d.h != nil {
		C.daveDecryptorDestroy(d.h)
		d.h = nil
		runtime.SetFinalizer(d, nil)
	}
}
