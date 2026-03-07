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

// Encryptor wraps a libdave frame encryptor.
type Encryptor struct {
	h C.DAVEEncryptorHandle
}

// NewEncryptor creates a new DAVE frame encryptor.
func NewEncryptor() *Encryptor {
	e := &Encryptor{h: C.daveEncryptorCreate()}
	runtime.SetFinalizer(e, (*Encryptor).Close)
	return e
}

// SetKeyRatchet sets the key ratchet for encryption.
// The encryptor does NOT take ownership; the caller must keep the KeyRatchet alive.
func (e *Encryptor) SetKeyRatchet(kr *KeyRatchet) {
	if kr == nil {
		return
	}
	C.daveEncryptorSetKeyRatchet(e.h, kr.h)
}

// SetPassthroughMode enables or disables passthrough (no encryption).
func (e *Encryptor) SetPassthroughMode(enabled bool) {
	C.daveEncryptorSetPassthroughMode(e.h, C.bool(enabled))
}

// IsPassthroughMode returns true if passthrough mode is active.
func (e *Encryptor) IsPassthroughMode() bool {
	return bool(C.daveEncryptorIsPassthroughMode(e.h))
}

// AssignSsrcToCodec maps an SSRC to a codec type.
func (e *Encryptor) AssignSsrcToCodec(ssrc uint32, codec int) {
	C.daveEncryptorAssignSsrcToCodec(e.h, C.uint32_t(ssrc), C.DAVECodec(codec))
}

// Encrypt encrypts an opus frame for DAVE E2EE.
func (e *Encryptor) Encrypt(mediaType int, ssrc uint32, frame []byte) ([]byte, error) {
	if len(frame) == 0 {
		return frame, nil
	}

	maxSize := C.daveEncryptorGetMaxCiphertextByteSize(
		e.h,
		C.DAVEMediaType(mediaType),
		C.size_t(len(frame)),
	)

	out := make([]byte, maxSize)
	var bytesWritten C.size_t

	rc := C.daveEncryptorEncrypt(
		e.h,
		C.DAVEMediaType(mediaType),
		C.uint32_t(ssrc),
		(*C.uint8_t)(unsafe.Pointer(&frame[0])),
		C.size_t(len(frame)),
		(*C.uint8_t)(unsafe.Pointer(&out[0])),
		maxSize,
		&bytesWritten,
	)

	if rc != C.DAVE_ENCRYPTOR_RESULT_CODE_SUCCESS {
		return nil, fmt.Errorf("dave: encrypt failed with code %d", rc)
	}

	return out[:bytesWritten], nil
}

// Close destroys the encryptor.
func (e *Encryptor) Close() {
	if e.h != nil {
		C.daveEncryptorDestroy(e.h)
		e.h = nil
		runtime.SetFinalizer(e, nil)
	}
}
