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

// Decryptor wraps a libdave frame decryptor.
type Decryptor struct {
	h *C.dave_decryptor_t
}

// NewDecryptor creates a new DAVE frame decryptor.
func NewDecryptor() *Decryptor {
	d := &Decryptor{h: C.dave_decryptor_create()}
	runtime.SetFinalizer(d, (*Decryptor).Close)
	return d
}

// Decrypt decrypts a DAVE-encrypted frame.
func (d *Decryptor) Decrypt(mediaType int, encryptedFrame []byte) ([]byte, error) {
	if len(encryptedFrame) == 0 {
		return encryptedFrame, nil
	}

	maxSize := C.dave_decryptor_get_max_plaintext_byte_size(
		d.h,
		C.dave_media_type(mediaType),
		C.size_t(len(encryptedFrame)),
	)

	out := make([]byte, maxSize)
	written := C.dave_decryptor_decrypt(
		d.h,
		C.dave_media_type(mediaType),
		(*C.uint8_t)(unsafe.Pointer(&encryptedFrame[0])),
		C.size_t(len(encryptedFrame)),
		(*C.uint8_t)(unsafe.Pointer(&out[0])),
		maxSize,
	)

	if written == 0 {
		return nil, fmt.Errorf("dave: decrypt failed")
	}

	return out[:written], nil
}

// TransitionToKeyRatchet transitions the decryptor to a new key ratchet.
// The old keys are kept for transitionExpiryMs (0 = default 10s).
// The KeyRatchet is consumed.
func (d *Decryptor) TransitionToKeyRatchet(kr *KeyRatchet, transitionExpiryMs uint32) {
	if kr == nil {
		return
	}
	C.dave_decryptor_transition_to_key_ratchet(d.h, kr.consume(), C.uint32_t(transitionExpiryMs))
}

// TransitionToPassthroughMode transitions the decryptor to/from passthrough.
func (d *Decryptor) TransitionToPassthroughMode(passthrough bool, transitionExpiryMs uint32) {
	v := C.int(0)
	if passthrough {
		v = 1
	}
	C.dave_decryptor_transition_to_passthrough_mode(d.h, v, C.uint32_t(transitionExpiryMs))
}

// Close destroys the decryptor.
func (d *Decryptor) Close() {
	if d.h != nil {
		C.dave_decryptor_destroy(d.h)
		d.h = nil
		runtime.SetFinalizer(d, nil)
	}
}
