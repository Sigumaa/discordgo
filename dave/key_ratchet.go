package dave

/*
#include "libdave_c.h"
*/
import "C"
import "runtime"

// KeyRatchet wraps a libdave key ratchet handle.
// It is obtained from MLSSession.GetKeyRatchet and passed to
// Encryptor.SetKeyRatchet or Decryptor.TransitionToKeyRatchet.
// Once passed to an encryptor or decryptor, the KeyRatchet handle
// is consumed and must not be used again.
type KeyRatchet struct {
	h *C.dave_key_ratchet_t
}

func newKeyRatchet(h *C.dave_key_ratchet_t) *KeyRatchet {
	kr := &KeyRatchet{h: h}
	runtime.SetFinalizer(kr, (*KeyRatchet).destroy)
	return kr
}

func (kr *KeyRatchet) destroy() {
	if kr.h != nil {
		C.dave_key_ratchet_destroy(kr.h)
		kr.h = nil
	}
}

// consume takes the underlying handle and nullifies the wrapper so the
// finalizer becomes a no-op. Used when ownership transfers to C.
func (kr *KeyRatchet) consume() *C.dave_key_ratchet_t {
	h := kr.h
	kr.h = nil
	runtime.SetFinalizer(kr, nil)
	return h
}
