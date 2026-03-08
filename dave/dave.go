package dave

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// DAVESession coordinates MLS session management with per-SSRC
// encrypt/decrypt operations for DAVE E2EE on Discord voice.
type DAVESession struct {
	mu sync.RWMutex

	mlsSession      *MLSSession
	encryptor       *Encryptor
	decryptors      map[uint32]*Decryptor // per-SSRC
	oldDecryptors   map[uint32]*Decryptor // kept during epoch transition
	keyRatchets     []*KeyRatchet         // kept alive while enc/dec reference them
	epoch           uint64
	protocolVersion uint16
	passthrough     bool
	selfUserID      string
	groupID         uint64
	authSessionID   string

	// pendingEncryptorKR holds the key ratchet for our encryptor,
	// obtained after commit/welcome but not activated until opcode 22.
	pendingEncryptorKR *KeyRatchet

	// ssrcToUserID maps SSRC to Discord user ID (from opcode 5 speaking events).
	ssrcToUserID map[uint32]string

	cleanupTimer *time.Timer
}

// NewDAVESession creates a new DAVESession for managing DAVE E2EE.
func NewDAVESession(authSessionID string, selfUserID string) (*DAVESession, error) {
	mls, err := NewMLSSession(authSessionID)
	if err != nil {
		return nil, err
	}

	return &DAVESession{
		mlsSession:    mls,
		encryptor:     NewEncryptor(),
		decryptors:    make(map[uint32]*Decryptor),
		oldDecryptors: make(map[uint32]*Decryptor),
		ssrcToUserID:  make(map[uint32]string),
		selfUserID:    selfUserID,
		authSessionID: authSessionID,
	}, nil
}

// Init initializes the DAVE session for a voice channel.
func (ds *DAVESession) Init(protocolVersion uint16, groupID uint64) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.protocolVersion = protocolVersion
	ds.groupID = groupID
	ds.mlsSession.Init(protocolVersion, groupID, ds.selfUserID)
	ds.passthrough = true
	ds.encryptor.SetPassthroughMode(true)
}

// Reset resets the session state (e.g., on reconnect).
func (ds *DAVESession) Reset() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.mlsSession.Reset()
	ds.encryptor.SetPassthroughMode(true)
	ds.passthrough = true
	ds.pendingEncryptorKR = nil

	for _, d := range ds.decryptors {
		d.TransitionToPassthroughMode(true)
	}
}

// RegisterSSRC registers an SSRC→userID mapping (from voice speaking events).
// This enables decryptor key ratchet assignment.
func (ds *DAVESession) RegisterSSRC(ssrc uint32, userID string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.ssrcToUserID[ssrc] = userID

	// If we have an active MLS session (not passthrough), set up the decryptor
	if !ds.passthrough {
		kr := ds.mlsSession.GetKeyRatchet(userID)
		if kr != nil {
			ds.keyRatchets = append(ds.keyRatchets, kr)
			d := ds.getOrCreateDecryptorLocked(ssrc)
			d.TransitionToKeyRatchet(kr)
		}
	}
}

// GetMarshalledKeyPackage returns the marshalled key package for sending
// to Discord (opcode 26).
func (ds *DAVESession) GetMarshalledKeyPackage() ([]byte, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.mlsSession.GetMarshalledKeyPackage()
}

// EncryptOpusFrame encrypts an opus frame using DAVE E2EE.
func (ds *DAVESession) EncryptOpusFrame(ssrc uint32, frame []byte) ([]byte, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if ds.passthrough {
		return frame, nil
	}

	return ds.encryptor.Encrypt(MediaTypeAudio, ssrc, frame)
}

// DecryptOpusFrame decrypts a DAVE-encrypted opus frame.
func (ds *DAVESession) DecryptOpusFrame(ssrc uint32, frame []byte) ([]byte, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if ds.passthrough {
		return frame, nil
	}

	d, ok := ds.decryptors[ssrc]
	if !ok {
		d, ok = ds.oldDecryptors[ssrc]
		if !ok {
			return nil, fmt.Errorf("dave: no decryptor for SSRC %d", ssrc)
		}
	}

	return d.Decrypt(MediaTypeAudio, frame)
}

// GetOrCreateDecryptor returns the decryptor for a given SSRC, creating one
// if it doesn't exist.
func (ds *DAVESession) GetOrCreateDecryptor(ssrc uint32) *Decryptor {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.getOrCreateDecryptorLocked(ssrc)
}

// voicePrepareEpochData represents the data from opcode 24.
type voicePrepareEpochData struct {
	ProtocolVersion uint16 `json:"protocol_version"`
	Epoch           uint64 `json:"epoch"`
}

// HandlePrepareEpoch handles voice opcode 24 (DAVE_PREPARE_EPOCH).
func (ds *DAVESession) HandlePrepareEpoch(data json.RawMessage) error {
	var d voicePrepareEpochData
	if err := json.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("dave: unmarshal prepare epoch: %w", err)
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.protocolVersion = d.ProtocolVersion
	ds.epoch = d.Epoch
	ds.mlsSession.SetProtocolVersion(d.ProtocolVersion)

	// epoch=1 means sole member reset: discard established group state
	if d.Epoch == 1 {
		ds.mlsSession.Reset()
		ds.mlsSession.Init(d.ProtocolVersion, ds.groupID, ds.selfUserID)
	}

	return nil
}

// HandleExternalSender processes binary opcode 25 payload.
// Sets the external sender credential and returns the key package to send as opcode 26.
func (ds *DAVESession) HandleExternalSender(data []byte) ([]byte, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.mlsSession.SetExternalSender(data)

	kp, err := ds.mlsSession.GetMarshalledKeyPackage()
	if err != nil {
		return nil, fmt.Errorf("dave: get key package after external sender: %w", err)
	}
	return kp, nil
}

// HandleProposalsBinary processes binary opcode 27 payload.
// Discord voice gateway already sends raw serialized proposal bytes.
// Returns commit+welcome bytes to send as opcode 28 (if we are the committer), or nil.
func (ds *DAVESession) HandleProposalsBinary(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("dave: proposals data too short")
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	commitWelcome, err := ds.mlsSession.ProcessProposals(data, nil)
	if err != nil {
		return nil, fmt.Errorf("dave: process proposals: %w", err)
	}
	if len(commitWelcome) > 0 {
		// We are the committer: prepare receiver keys now
		ds.prepareReceiverKeys()
	}
	return commitWelcome, nil
}

// HandleCommitBinary processes binary opcode 29 payload (announce_commit_transition).
// The payload format is: [transition_id (2 bytes LE)][commit_message].
// Returns the transition_id for sending opcode 23.
func (ds *DAVESession) HandleCommitBinary(data []byte) (uint16, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("dave: commit data too short")
	}

	transitionID := binary.LittleEndian.Uint16(data[0:2])
	commitData := data[2:]

	if len(commitData) == 0 {
		return transitionID, fmt.Errorf("dave: empty commit message")
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	resultType, err := ds.mlsSession.ProcessCommit(commitData)
	if err != nil {
		return transitionID, fmt.Errorf("dave: process commit: %w", err)
	}
	if resultType == CommitFailed {
		return transitionID, fmt.Errorf("dave: commit processing failed")
	}
	if resultType == CommitOK {
		ds.prepareReceiverKeys()
		if transitionID == 0 {
			ds.activateSenderKeysLocked()
		}
	}
	return transitionID, nil
}

// HandleWelcomeBinary processes binary opcode 30 payload.
// The payload format is: [transition_id (2 bytes LE)][welcome_message].
// Returns the transition_id for sending opcode 23.
func (ds *DAVESession) HandleWelcomeBinary(data []byte) (uint16, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("dave: welcome data too short")
	}

	transitionID := binary.LittleEndian.Uint16(data[0:2])
	welcomeData := data[2:]

	if len(welcomeData) == 0 {
		return transitionID, fmt.Errorf("dave: empty welcome message")
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	err := ds.mlsSession.ProcessWelcome(welcomeData, nil)
	if err != nil {
		return transitionID, fmt.Errorf("dave: process welcome: %w", err)
	}
	ds.prepareReceiverKeys()
	if transitionID == 0 {
		ds.activateSenderKeysLocked()
	}
	return transitionID, nil
}

// TransitionResult contains the response to send back as opcode 23.
type TransitionResult struct {
	TransitionID uint16 `json:"transition_id"`
}

// ActivateSenderKeys activates the pending encryptor key ratchet.
// Called when opcode 22 (execute_transition) is received.
func (ds *DAVESession) ActivateSenderKeys() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.activateSenderKeysLocked()
}

func (ds *DAVESession) activateSenderKeysLocked() {
	if ds.pendingEncryptorKR != nil {
		ds.encryptor.SetKeyRatchet(ds.pendingEncryptorKR)
		ds.encryptor.SetPassthroughMode(false)
		ds.pendingEncryptorKR = nil
	}
	ds.passthrough = false
}

// HandleExecuteTransition handles voice opcode 22 (execute transition signal).
// Activates sender-side encryption and returns the transition data.
func (ds *DAVESession) HandleExecuteTransition(data json.RawMessage) (*TransitionResult, error) {
	var d struct {
		TransitionID uint16 `json:"transition_id"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("dave: unmarshal execute transition: %w", err)
	}

	ds.ActivateSenderKeys()

	return &TransitionResult{
		TransitionID: d.TransitionID,
	}, nil
}

// prepareReceiverKeys prepares decryptor keys from the current MLS epoch.
// The encryptor key ratchet is stored as pending until opcode 22.
// Must be called with ds.mu held.
func (ds *DAVESession) prepareReceiverKeys() {
	// Release old key ratchets
	for _, kr := range ds.keyRatchets {
		kr.Close()
	}
	ds.keyRatchets = nil

	// Get our own key ratchet for the encryptor (stored as pending)
	kr := ds.mlsSession.GetKeyRatchet(ds.selfUserID)
	if kr != nil {
		ds.pendingEncryptorKR = kr
		ds.keyRatchets = append(ds.keyRatchets, kr)
	}

	// Move current decryptors to old (for transition period ~10s)
	if ds.cleanupTimer != nil {
		ds.cleanupTimer.Stop()
	}

	for ssrc, d := range ds.oldDecryptors {
		d.Close()
		delete(ds.oldDecryptors, ssrc)
	}

	ds.oldDecryptors = ds.decryptors
	ds.decryptors = make(map[uint32]*Decryptor)

	// Set up decryptors for known SSRCs with their key ratchets
	for ssrc, userID := range ds.ssrcToUserID {
		ukr := ds.mlsSession.GetKeyRatchet(userID)
		if ukr != nil {
			ds.keyRatchets = append(ds.keyRatchets, ukr)
			d := ds.getOrCreateDecryptorLocked(ssrc)
			d.TransitionToKeyRatchet(ukr)
		}
	}

	ds.cleanupTimer = time.AfterFunc(time.Duration(DefaultTransitionExpiryMs)*time.Millisecond, func() {
		ds.mu.Lock()
		defer ds.mu.Unlock()
		for ssrc, d := range ds.oldDecryptors {
			d.Close()
			delete(ds.oldDecryptors, ssrc)
		}
	})
}

// TransitionDecryptor transitions a specific SSRC's decryptor to use the key
// ratchet from the given user ID.
func (ds *DAVESession) TransitionDecryptor(ssrc uint32, userID string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	kr := ds.mlsSession.GetKeyRatchet(userID)
	if kr == nil {
		return
	}
	ds.keyRatchets = append(ds.keyRatchets, kr)

	d := ds.getOrCreateDecryptorLocked(ssrc)
	d.TransitionToKeyRatchet(kr)
}

func (ds *DAVESession) getOrCreateDecryptorLocked(ssrc uint32) *Decryptor {
	if d, ok := ds.decryptors[ssrc]; ok {
		return d
	}
	d := NewDecryptor()
	ds.decryptors[ssrc] = d
	return d
}

// Close cleans up all resources.
func (ds *DAVESession) Close() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.cleanupTimer != nil {
		ds.cleanupTimer.Stop()
		ds.cleanupTimer = nil
	}

	if ds.encryptor != nil {
		ds.encryptor.Close()
		ds.encryptor = nil
	}

	for _, d := range ds.decryptors {
		d.Close()
	}
	ds.decryptors = nil

	for _, d := range ds.oldDecryptors {
		d.Close()
	}
	ds.oldDecryptors = nil

	for _, kr := range ds.keyRatchets {
		kr.Close()
	}
	ds.keyRatchets = nil
	ds.pendingEncryptorKR = nil

	if ds.mlsSession != nil {
		ds.mlsSession.Close()
		ds.mlsSession = nil
	}
}
