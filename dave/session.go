package dave

/*
#include "libdave_c.h"
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
	h *C.dave_session_t
}

// NewMLSSession creates a new MLS session with the given auth session ID.
func NewMLSSession(authSessionID string) (*MLSSession, error) {
	cAuth := C.CString(authSessionID)
	defer C.free(unsafe.Pointer(cAuth))

	h := C.dave_session_create(cAuth)
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
	C.dave_session_init(s.h, C.uint16_t(protocolVersion), C.uint64_t(groupID), cUser)
}

// Reset resets the MLS session state.
func (s *MLSSession) Reset() {
	C.dave_session_reset(s.h)
}

// SetProtocolVersion updates the protocol version.
func (s *MLSSession) SetProtocolVersion(version uint16) {
	C.dave_session_set_protocol_version(s.h, C.uint16_t(version))
}

// GetProtocolVersion returns the current protocol version.
func (s *MLSSession) GetProtocolVersion() uint16 {
	return uint16(C.dave_session_get_protocol_version(s.h))
}

// GetMarshalledKeyPackage returns a marshalled MLS key package.
// A new key package is generated each call (key packages are not reusable).
func (s *MLSSession) GetMarshalledKeyPackage() ([]byte, error) {
	var outLen C.size_t
	ptr := C.dave_session_get_marshalled_key_package(s.h, &outLen)
	if ptr == nil {
		return nil, fmt.Errorf("dave: failed to get marshalled key package")
	}
	defer cgoFree(unsafe.Pointer(ptr))
	return C.GoBytes(unsafe.Pointer(ptr), C.int(outLen)), nil
}

// ProcessProposals processes MLS proposals and returns a commit message if one
// was generated. Returns nil if no commit was produced.
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

	var outLen C.size_t
	ptr := C.dave_session_process_proposals(
		s.h,
		(*C.uint8_t)(unsafe.Pointer(&proposals[0])),
		C.size_t(len(proposals)),
		userIDsPtr,
		C.size_t(len(recognizedUserIDs)),
		&outLen,
	)
	if ptr == nil {
		return nil, nil
	}
	defer cgoFree(unsafe.Pointer(ptr))
	return C.GoBytes(unsafe.Pointer(ptr), C.int(outLen)), nil
}

// ProcessCommit processes an MLS commit message.
func (s *MLSSession) ProcessCommit(commit []byte) (CommitResultType, []RosterEntry, error) {
	var resultType C.int
	var rosterIDs *C.uint64_t
	var rosterKeys **C.uint8_t
	var rosterKeyLens *C.size_t
	var rosterCount C.size_t

	C.dave_session_process_commit(
		s.h,
		(*C.uint8_t)(unsafe.Pointer(&commit[0])),
		C.size_t(len(commit)),
		&resultType,
		&rosterIDs,
		&rosterKeys,
		&rosterKeyLens,
		&rosterCount,
	)

	rt := CommitResultType(resultType)
	if rt != CommitOK || rosterCount == 0 {
		return rt, nil, nil
	}

	defer func() {
		cgoFree(unsafe.Pointer(rosterIDs))
		// Free individual key buffers
		keySlice := unsafe.Slice(rosterKeys, rosterCount)
		for _, k := range keySlice {
			if k != nil {
				cgoFree(unsafe.Pointer(k))
			}
		}
		cgoFree(unsafe.Pointer(rosterKeys))
		cgoFree(unsafe.Pointer(rosterKeyLens))
	}()

	count := int(rosterCount)
	ids := unsafe.Slice(rosterIDs, count)
	keys := unsafe.Slice(rosterKeys, count)
	keyLens := unsafe.Slice(rosterKeyLens, count)

	entries := make([]RosterEntry, count)
	for i := 0; i < count; i++ {
		entries[i].UserID = uint64(ids[i])
		kLen := int(keyLens[i])
		if kLen > 0 && keys[i] != nil {
			entries[i].Key = C.GoBytes(unsafe.Pointer(keys[i]), C.int(kLen))
		}
	}

	return rt, entries, nil
}

// ProcessWelcome processes an MLS welcome message.
func (s *MLSSession) ProcessWelcome(welcome []byte, recognizedUserIDs []string) ([]RosterEntry, error) {
	cUserIDs := make([]*C.char, len(recognizedUserIDs))
	for i, uid := range recognizedUserIDs {
		cUserIDs[i] = C.CString(uid)
		defer C.free(unsafe.Pointer(cUserIDs[i]))
	}

	var userIDsPtr **C.char
	if len(cUserIDs) > 0 {
		userIDsPtr = &cUserIDs[0]
	}

	var rosterIDs *C.uint64_t
	var rosterKeys **C.uint8_t
	var rosterKeyLens *C.size_t
	var rosterCount C.size_t

	ok := C.dave_session_process_welcome(
		s.h,
		(*C.uint8_t)(unsafe.Pointer(&welcome[0])),
		C.size_t(len(welcome)),
		userIDsPtr,
		C.size_t(len(recognizedUserIDs)),
		&rosterIDs,
		&rosterKeys,
		&rosterKeyLens,
		&rosterCount,
	)

	if ok == 0 {
		return nil, fmt.Errorf("dave: process welcome failed")
	}

	if rosterCount == 0 {
		return nil, nil
	}

	defer func() {
		cgoFree(unsafe.Pointer(rosterIDs))
		keySlice := unsafe.Slice(rosterKeys, rosterCount)
		for _, k := range keySlice {
			if k != nil {
				cgoFree(unsafe.Pointer(k))
			}
		}
		cgoFree(unsafe.Pointer(rosterKeys))
		cgoFree(unsafe.Pointer(rosterKeyLens))
	}()

	count := int(rosterCount)
	ids := unsafe.Slice(rosterIDs, count)
	keys := unsafe.Slice(rosterKeys, count)
	keyLens := unsafe.Slice(rosterKeyLens, count)

	entries := make([]RosterEntry, count)
	for i := 0; i < count; i++ {
		entries[i].UserID = uint64(ids[i])
		kLen := int(keyLens[i])
		if kLen > 0 && keys[i] != nil {
			entries[i].Key = C.GoBytes(unsafe.Pointer(keys[i]), C.int(kLen))
		}
	}

	return entries, nil
}

// GetKeyRatchet returns a key ratchet for the given user ID.
func (s *MLSSession) GetKeyRatchet(userID string) *KeyRatchet {
	cUser := C.CString(userID)
	defer C.free(unsafe.Pointer(cUser))
	h := C.dave_session_get_key_ratchet(s.h, cUser)
	if h == nil {
		return nil
	}
	return newKeyRatchet(h)
}

// Close destroys the underlying MLS session.
func (s *MLSSession) Close() {
	if s.h != nil {
		C.dave_session_destroy(s.h)
		s.h = nil
		runtime.SetFinalizer(s, nil)
	}
}
