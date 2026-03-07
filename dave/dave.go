package dave

import (
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
	epoch           uint64
	protocolVersion uint16
	passthrough     bool
	selfUserID      string
	groupID         uint64
	authSessionID   string

	// cleanupTimer cleans up old decryptors after epoch transition
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
	ds.passthrough = false
	ds.encryptor.SetPassthroughMode(false)
}

// Reset resets the session state (e.g., on reconnect).
func (ds *DAVESession) Reset() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.mlsSession.Reset()
	ds.encryptor.SetPassthroughMode(true)
	ds.passthrough = true

	for _, d := range ds.decryptors {
		d.TransitionToPassthroughMode(true, DefaultTransitionExpiryMs)
	}
}

// GetMarshalledKeyPackage returns the marshalled key package for sending
// to Discord (used in opcode 22 response / opcode 23).
func (ds *DAVESession) GetMarshalledKeyPackage() ([]byte, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.mlsSession.GetMarshalledKeyPackage()
}

// EncryptOpusFrame encrypts an opus frame using DAVE E2EE.
// If passthrough mode is active, returns the frame unchanged.
func (ds *DAVESession) EncryptOpusFrame(ssrc uint32, frame []byte) ([]byte, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if ds.passthrough {
		return frame, nil
	}

	return ds.encryptor.Encrypt(MediaTypeAudio, ssrc, frame)
}

// DecryptOpusFrame decrypts a DAVE-encrypted opus frame.
// If passthrough mode is active, returns the frame unchanged.
func (ds *DAVESession) DecryptOpusFrame(ssrc uint32, frame []byte) ([]byte, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if ds.passthrough {
		return frame, nil
	}

	d, ok := ds.decryptors[ssrc]
	if !ok {
		// Try old decryptors during transition
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

	if d, ok := ds.decryptors[ssrc]; ok {
		return d
	}
	d := NewDecryptor()
	if ds.passthrough {
		d.TransitionToPassthroughMode(true, 0)
	}
	ds.decryptors[ssrc] = d
	return d
}

// voicePrepareEpochData represents the data from opcode 24 (DAVE Prepare Epoch).
type voicePrepareEpochData struct {
	ProtocolVersion uint16 `json:"protocol_version"`
	Epoch           uint64 `json:"epoch"`
}

// HandlePrepareEpoch handles voice opcode 24 (DAVE_PREPARE_EPOCH).
// It prepares the session for a new epoch.
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

	return nil
}

// voiceExecuteTransitionData represents the data from opcode 22
// (DAVE MLS External Sender Package / Execute Transition).
type voiceExecuteTransitionData struct {
	TransitionID uint16 `json:"transition_id"`
	Proposals    []byte `json:"proposals,omitempty"`
	Commit       []byte `json:"commit,omitempty"`
	Welcome      []byte `json:"welcome,omitempty"`
	// Recognized user IDs for MLS verification
	RecognizedUserIDs []string `json:"recognized_user_ids,omitempty"`
}

// TransitionResult contains the response to send back as opcode 23.
type TransitionResult struct {
	TransitionID uint16 `json:"transition_id"`
	KeyPackage   []byte `json:"key_package,omitempty"`
}

// HandleExecuteTransition handles voice opcode 22 (DAVE MLS Execute Transition).
// It processes MLS proposals/commit/welcome and returns the transition response
// to be sent as opcode 23.
func (ds *DAVESession) HandleExecuteTransition(data json.RawMessage) (*TransitionResult, error) {
	var d voiceExecuteTransitionData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("dave: unmarshal execute transition: %w", err)
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	result := &TransitionResult{
		TransitionID: d.TransitionID,
	}

	// Process proposals if present
	if len(d.Proposals) > 0 {
		commitMsg, err := ds.mlsSession.ProcessProposals(d.Proposals, d.RecognizedUserIDs)
		if err != nil {
			return nil, fmt.Errorf("dave: process proposals: %w", err)
		}
		_ = commitMsg // commit is sent by the server, not by us
	}

	// Process commit if present
	if len(d.Commit) > 0 {
		resultType, _, err := ds.mlsSession.ProcessCommit(d.Commit)
		if err != nil {
			return nil, fmt.Errorf("dave: process commit: %w", err)
		}
		if resultType == CommitFailed {
			return nil, fmt.Errorf("dave: commit processing failed")
		}
		if resultType == CommitOK {
			ds.updateEncryptionKeys()
		}
	}

	// Process welcome if present
	if len(d.Welcome) > 0 {
		_, err := ds.mlsSession.ProcessWelcome(d.Welcome, d.RecognizedUserIDs)
		if err != nil {
			return nil, fmt.Errorf("dave: process welcome: %w", err)
		}
		ds.updateEncryptionKeys()
	}

	// Get key package for the response
	kp, err := ds.mlsSession.GetMarshalledKeyPackage()
	if err != nil {
		return nil, fmt.Errorf("dave: get key package: %w", err)
	}
	result.KeyPackage = kp

	return result, nil
}

// updateEncryptionKeys updates the encryptor and decryptors with new keys
// from the current MLS epoch. Must be called with ds.mu held.
func (ds *DAVESession) updateEncryptionKeys() {
	// Update encryptor with our own key ratchet
	kr := ds.mlsSession.GetKeyRatchet(ds.selfUserID)
	if kr != nil {
		ds.encryptor.SetKeyRatchet(kr)
		ds.encryptor.SetPassthroughMode(false)
	}
	ds.passthrough = false

	// Move current decryptors to old (for transition period)
	if ds.cleanupTimer != nil {
		ds.cleanupTimer.Stop()
	}

	for ssrc, d := range ds.oldDecryptors {
		d.Close()
		delete(ds.oldDecryptors, ssrc)
	}

	ds.oldDecryptors = ds.decryptors
	ds.decryptors = make(map[uint32]*Decryptor)

	// Schedule cleanup of old decryptors
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

	d := ds.getOrCreateDecryptorLocked(ssrc)
	d.TransitionToKeyRatchet(kr, DefaultTransitionExpiryMs)
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

	if ds.mlsSession != nil {
		ds.mlsSession.Close()
		ds.mlsSession = nil
	}
}
