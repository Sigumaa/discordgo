package dave

/*
#include "dave/dave.h"
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"runtime"
	"unsafe"
)

// MLSSession wraps a libdave MLS session.
type MLSSession struct {
	h C.DAVESessionHandle
}

// NewMLSSession creates a new MLS session with the given auth session ID.
func NewMLSSession(authSessionID string) (*MLSSession, error) {
	var cAuth *C.char
	if authSessionID != "" {
		cAuth = C.CString(authSessionID)
		defer C.free(unsafe.Pointer(cAuth))
	}

	h := C.daveSessionCreate(nil, cAuth, nil, nil)
	if h == nil {
		return nil, fmt.Errorf("dave: failed to create MLS session")
	}

	s := &MLSSession{h: h}
	runtime.SetFinalizer(s, (*MLSSession).Close)
	return s, nil
}

// Init initializes the MLS session for a voice channel.
func (s *MLSSession) Init(protocolVersion uint16, groupID uint64, selfUserID string) {
	cUser := C.CString(selfUserID)
	defer C.free(unsafe.Pointer(cUser))
	C.daveSessionInit(s.h, C.uint16_t(protocolVersion), C.uint64_t(groupID), cUser)
}

// Reset resets the MLS session state.
func (s *MLSSession) Reset() {
	C.daveSessionReset(s.h)
}

// SetProtocolVersion updates the protocol version.
func (s *MLSSession) SetProtocolVersion(version uint16) {
	C.daveSessionSetProtocolVersion(s.h, C.uint16_t(version))
}

// GetProtocolVersion returns the current protocol version.
func (s *MLSSession) GetProtocolVersion() uint16 {
	return uint16(C.daveSessionGetProtocolVersion(s.h))
}

// SetExternalSender sets the external sender credentials.
func (s *MLSSession) SetExternalSender(data []byte) {
	if len(data) == 0 {
		return
	}
	C.daveSessionSetExternalSender(s.h, (*C.uint8_t)(unsafe.Pointer(&data[0])), C.size_t(len(data)))
}

// GetMarshalledKeyPackage returns a marshalled MLS key package.
func (s *MLSSession) GetMarshalledKeyPackage() ([]byte, error) {
	var ptr *C.uint8_t
	var length C.size_t
	C.daveSessionGetMarshalledKeyPackage(s.h, &ptr, &length)
	if ptr == nil || length == 0 {
		return nil, fmt.Errorf("dave: failed to get marshalled key package")
	}
	defer cgoFree(unsafe.Pointer(ptr))
	return C.GoBytes(unsafe.Pointer(ptr), C.int(length)), nil
}

// ProcessProposals processes MLS proposals.
func (s *MLSSession) ProcessProposals(proposals []byte, recognizedUserIDs []string) ([]byte, error) {
	cUserIDs := make([]*C.char, len(recognizedUserIDs))
	for i, uid := range recognizedUserIDs {
		cUserIDs[i] = C.CString(uid)
		defer C.free(unsafe.Pointer(cUserIDs[i]))
	}

	var userIDsPtr **C.char
	if len(cUserIDs) > 0 {
		userIDsPtr = &cUserIDs[0]
	}

	var outPtr *C.uint8_t
	var outLen C.size_t

	C.daveSessionProcessProposals(
		s.h,
		(*C.uint8_t)(unsafe.Pointer(&proposals[0])),
		C.size_t(len(proposals)),
		userIDsPtr,
		C.size_t(len(recognizedUserIDs)),
		&outPtr,
		&outLen,
	)

	if outPtr == nil || outLen == 0 {
		return nil, nil
	}
	defer cgoFree(unsafe.Pointer(outPtr))
	return C.GoBytes(unsafe.Pointer(outPtr), C.int(outLen)), nil
}

// ProcessCommit processes an MLS commit message.
func (s *MLSSession) ProcessCommit(commit []byte) (CommitResultType, error) {
	result := C.daveSessionProcessCommit(
		s.h,
		(*C.uint8_t)(unsafe.Pointer(&commit[0])),
		C.size_t(len(commit)),
	)
	if result == nil {
		return CommitFailed, fmt.Errorf("dave: process commit returned nil")
	}
	defer C.daveCommitResultDestroy(result)

	if C.daveCommitResultIsFailed(result) {
		return CommitFailed, nil
	}
	if C.daveCommitResultIsIgnored(result) {
		return CommitIgnored, nil
	}
	return CommitOK, nil
}

// ProcessWelcome processes an MLS welcome message.
func (s *MLSSession) ProcessWelcome(welcome []byte, recognizedUserIDs []string) error {
	cUserIDs := make([]*C.char, len(recognizedUserIDs))
	for i, uid := range recognizedUserIDs {
		cUserIDs[i] = C.CString(uid)
		defer C.free(unsafe.Pointer(cUserIDs[i]))
	}

	var userIDsPtr **C.char
	if len(cUserIDs) > 0 {
		userIDsPtr = &cUserIDs[0]
	}

	result := C.daveSessionProcessWelcome(
		s.h,
		(*C.uint8_t)(unsafe.Pointer(&welcome[0])),
		C.size_t(len(welcome)),
		userIDsPtr,
		C.size_t(len(recognizedUserIDs)),
	)
	if result == nil {
		return fmt.Errorf("dave: process welcome failed")
	}
	C.daveWelcomeResultDestroy(result)
	return nil
}

// GetKeyRatchet returns a key ratchet for the given user ID.
func (s *MLSSession) GetKeyRatchet(userID string) *KeyRatchet {
	cUser := C.CString(userID)
	defer C.free(unsafe.Pointer(cUser))
	h := C.daveSessionGetKeyRatchet(s.h, cUser)
	if h == nil {
		return nil
	}
	return newKeyRatchet(h)
}

// Close destroys the underlying MLS session.
func (s *MLSSession) Close() {
	if s.h != nil {
		C.daveSessionDestroy(s.h)
		s.h = nil
		runtime.SetFinalizer(s, nil)
	}
}
