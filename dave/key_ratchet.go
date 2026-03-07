package dave

/*
#include "dave/dave.h"
*/
import "C"
import "runtime"

// KeyRatchet wraps a libdave key ratchet handle.
type KeyRatchet struct {
	h C.DAVEKeyRatchetHandle
}

func newKeyRatchet(h C.DAVEKeyRatchetHandle) *KeyRatchet {
	kr := &KeyRatchet{h: h}
	runtime.SetFinalizer(kr, (*KeyRatchet).Close)
	return kr
}

// Close destroys the key ratchet.
func (kr *KeyRatchet) Close() {
	if kr.h != nil {
		C.daveKeyRatchetDestroy(kr.h)
		kr.h = nil
		runtime.SetFinalizer(kr, nil)
	}
}
