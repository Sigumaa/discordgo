package dave

/*
#include "libdave_c.h"
*/
import "C"
import (
	"fmt"
	"runtime"
	"unsafe"
)

// Encryptor wraps a libdave frame encryptor.
type Encryptor struct {
	h *C.dave_encryptor_t
}

// NewEncryptor creates a new DAVE frame encryptor.
func NewEncryptor() *Encryptor {
	e := &Encryptor{h: C.dave_encryptor_create()}
	runtime.SetFinalizer(e, (*Encryptor).Close)
	return e
}

// SetKeyRatchet sets the key ratchet for encryption. The KeyRatchet is consumed
// (ownership transfers to C++) and must not be used after this call.
func (e *Encryptor) SetKeyRatchet(kr *KeyRatchet) {
	if kr == nil {
		return
	}
	C.dave_encryptor_set_key_ratchet(e.h, kr.consume())
}

// SetPassthroughMode enables or disables passthrough (no encryption).
func (e *Encryptor) SetPassthroughMode(enabled bool) {
	v := C.int(0)
	if enabled {
		v = 1
	}
	C.dave_encryptor_set_passthrough_mode(e.h, v)
}

// IsPassthroughMode returns true if passthrough mode is active.
func (e *Encryptor) IsPassthroughMode() bool {
	return C.dave_encryptor_is_passthrough_mode(e.h) != 0
}

// Encrypt encrypts an opus frame for DAVE E2EE.
func (e *Encryptor) Encrypt(mediaType int, ssrc uint32, frame []byte) ([]byte, error) {
	if len(frame) == 0 {
		return frame, nil
	}

	maxSize := C.dave_encryptor_get_max_ciphertext_byte_size(
		e.h,
		C.dave_media_type(mediaType),
		C.size_t(len(frame)),
	)

	out := make([]byte, maxSize)
	var bytesWritten C.size_t

	rc := C.dave_encryptor_encrypt(
		e.h,
		C.dave_media_type(mediaType),
		C.uint32_t(ssrc),
		(*C.uint8_t)(unsafe.Pointer(&frame[0])),
		C.size_t(len(frame)),
		(*C.uint8_t)(unsafe.Pointer(&out[0])),
		maxSize,
		&bytesWritten,
	)

	if rc != C.DAVE_RESULT_SUCCESS {
		return nil, fmt.Errorf("dave: encrypt failed with code %d", rc)
	}

	return out[:bytesWritten], nil
}

// Close destroys the encryptor.
func (e *Encryptor) Close() {
	if e.h != nil {
		C.dave_encryptor_destroy(e.h)
		e.h = nil
		runtime.SetFinalizer(e, nil)
	}
}
