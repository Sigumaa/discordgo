// Discordgo - Discord bindings for Go
// Available at https://github.com/bwmarrin/discordgo

// Copyright 2015-2016 Bruce Marriner <bruce@sqls.net>.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file contains code related to Discord voice suppport

package discordgo

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo/dave"
	"github.com/gorilla/websocket"
)

// ------------------------------------------------------------------------------------------------
// Code related to both VoiceConnection Websocket and UDP connections.
// ------------------------------------------------------------------------------------------------

// A VoiceConnection struct holds all the data and functions related to a Discord Voice Connection.
type VoiceConnection struct {
	sync.RWMutex

	Debug        bool // If true, print extra logging -- DEPRECATED
	LogLevel     int
	Ready        bool // If true, voice is ready to send/receive audio
	UserID       string
	GuildID      string
	ChannelID    string
	deaf         bool
	mute         bool
	speaking     bool
	reconnecting bool // If true, voice connection is trying to reconnect

	OpusSend chan []byte  // Chan for sending opus audio
	OpusRecv chan *Packet // Chan for receiving opus audio

	wsConn  *websocket.Conn
	wsMutex sync.Mutex
	udpConn *net.UDPConn
	session *Session

	sessionID string
	token     string
	endpoint  string

	// Used to send a close signal to goroutines
	close chan struct{}

	// Used to allow blocking until connected
	connected chan bool

	// Used to pass the sessionid from onVoiceStateUpdate
	// sessionRecv chan string UNUSED ATM

	aead         cipher.AEAD
	nonceCounter uint32

	op4 voiceOP4
	op2 voiceOP2

	voiceSpeakingUpdateHandlers []VoiceSpeakingUpdateHandler

	// DAVE E2EE fields
	daveSession       *dave.DAVESession
	daveEnabled       bool
	daveProtoVersion  uint16
	daveTransitionID  uint16
	daveExternalReady bool
	pendingDaveBinary [][]byte
}

// VoiceSpeakingUpdateHandler type provides a function definition for the
// VoiceSpeakingUpdate event
type VoiceSpeakingUpdateHandler func(vc *VoiceConnection, vs *VoiceSpeakingUpdate)

// Speaking sends a speaking notification to Discord over the voice websocket.
// This must be sent as true prior to sending audio and should be set to false
// once finished sending audio.
// b : Send true if speaking, false if not.
func (v *VoiceConnection) Speaking(b bool) (err error) {

	v.log(LogDebug, "called (%t)", b)

	type voiceSpeakingData struct {
		Speaking bool `json:"speaking"`
		Delay    int  `json:"delay"`
	}

	type voiceSpeakingOp struct {
		Op   int               `json:"op"` // Always 5
		Data voiceSpeakingData `json:"d"`
	}

	if v.wsConn == nil {
		return fmt.Errorf("no VoiceConnection websocket")
	}

	data := voiceSpeakingOp{5, voiceSpeakingData{b, 0}}
	v.wsMutex.Lock()
	err = v.wsConn.WriteJSON(data)
	v.wsMutex.Unlock()

	v.Lock()
	defer v.Unlock()
	if err != nil {
		v.speaking = false
		v.log(LogError, "Speaking() write json error, %s", err)
		return
	}

	v.speaking = b

	return
}

// ChangeChannel sends Discord a request to change channels within a Guild
// !!! NOTE !!! This function may be removed in favour of just using ChannelVoiceJoin
func (v *VoiceConnection) ChangeChannel(channelID string, mute, deaf bool) (err error) {

	v.log(LogInformational, "called")

	data := voiceChannelJoinOp{4, voiceChannelJoinData{&v.GuildID, &channelID, mute, deaf}}
	v.session.wsMutex.Lock()
	err = v.session.wsConn.WriteJSON(data)
	v.session.wsMutex.Unlock()
	if err != nil {
		return
	}
	v.ChannelID = channelID
	v.deaf = deaf
	v.mute = mute
	v.speaking = false

	return
}

// Disconnect disconnects from this voice channel and closes the websocket
// and udp connections to Discord.
func (v *VoiceConnection) Disconnect() (err error) {

	// Send a OP4 with a nil channel to disconnect
	v.Lock()
	if v.sessionID != "" {
		data := voiceChannelJoinOp{4, voiceChannelJoinData{&v.GuildID, nil, true, true}}
		v.session.wsMutex.Lock()
		err = v.session.wsConn.WriteJSON(data)
		v.session.wsMutex.Unlock()
		v.sessionID = ""
	}
	v.Unlock()

	// Close websocket and udp connections
	v.Close()

	v.log(LogInformational, "Deleting VoiceConnection %s", v.GuildID)

	v.session.Lock()
	delete(v.session.VoiceConnections, v.GuildID)
	v.session.Unlock()

	return
}

// Close closes the voice ws and udp connections
func (v *VoiceConnection) Close() {

	v.log(LogInformational, "called")

	v.Lock()
	defer v.Unlock()

	v.Ready = false
	v.speaking = false

	if v.daveSession != nil {
		v.daveSession.Close()
		v.daveSession = nil
	}
	v.daveExternalReady = false

	if v.close != nil {
		v.log(LogInformational, "closing v.close")
		close(v.close)
		v.close = nil
	}

	if v.udpConn != nil {
		v.log(LogInformational, "closing udp")
		err := v.udpConn.Close()
		if err != nil {
			v.log(LogError, "error closing udp connection, %s", err)
		}
		v.udpConn = nil
	}

	if v.wsConn != nil {
		v.log(LogInformational, "sending close frame")

		// To cleanly close a connection, a client should send a close
		// frame and wait for the server to close the connection.
		v.wsMutex.Lock()
		err := v.wsConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		v.wsMutex.Unlock()
		if err != nil {
			v.log(LogError, "error closing websocket, %s", err)
		}

		// TODO: Wait for Discord to actually close the connection.
		time.Sleep(1 * time.Second)

		v.log(LogInformational, "closing websocket")
		err = v.wsConn.Close()
		if err != nil {
			v.log(LogError, "error closing websocket, %s", err)
		}

		v.wsConn = nil
	}
}

// AddHandler adds a Handler for VoiceSpeakingUpdate events.
func (v *VoiceConnection) AddHandler(h VoiceSpeakingUpdateHandler) {
	v.Lock()
	defer v.Unlock()

	v.voiceSpeakingUpdateHandlers = append(v.voiceSpeakingUpdateHandlers, h)
}

// VoiceSpeakingUpdate is a struct for a VoiceSpeakingUpdate event.
type VoiceSpeakingUpdate struct {
	UserID   string `json:"user_id"`
	SSRC     int    `json:"ssrc"`
	Speaking bool   `json:"speaking"`
}

// ------------------------------------------------------------------------------------------------
// Unexported Internal Functions Below.
// ------------------------------------------------------------------------------------------------

// A voiceOP4 stores the data for the voice operation 4 websocket event
// which provides us with the NaCl SecretBox encryption key
type voiceOP4 struct {
	SecretKey [32]byte `json:"secret_key"`
	Mode      string   `json:"mode"`
}

// A voiceOP2 stores the data for the voice operation 2 websocket event
// which is sort of like the voice READY packet
type voiceOP2 struct {
	SSRC              uint32        `json:"ssrc"`
	Port              int           `json:"port"`
	Modes             []string      `json:"modes"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
	IP                string        `json:"ip"`
}

// WaitUntilConnected waits for the Voice Connection to
// become ready, if it does not become ready it returns an err
func (v *VoiceConnection) waitUntilConnected() error {

	v.log(LogInformational, "called")

	i := 0
	for {
		v.RLock()
		ready := v.Ready
		v.RUnlock()
		if ready {
			return nil
		}

		if i > 10 {
			return fmt.Errorf("timeout waiting for voice")
		}

		time.Sleep(1 * time.Second)
		i++
	}
}

// Open opens a voice connection.  This should be called
// after VoiceChannelJoin is used and the data VOICE websocket events
// are captured.
func (v *VoiceConnection) open() (err error) {

	v.log(LogInformational, "called")

	v.Lock()
	defer v.Unlock()

	// Don't open a websocket if one is already open
	if v.wsConn != nil {
		v.log(LogWarning, "refusing to overwrite non-nil websocket")
		return
	}

	// TODO temp? loop to wait for the SessionID
	i := 0
	for {
		if v.sessionID != "" {
			break
		}

		if i > 20 { // only loop for up to 1 second total
			return fmt.Errorf("did not receive voice Session ID in time")
		}
		// Release the lock, so sessionID can be populated upon receiving a VoiceStateUpdate event.
		v.Unlock()
		time.Sleep(50 * time.Millisecond)
		i++
		v.Lock()
	}

	// Connect to VoiceConnection Websocket
	vg := "wss://" + strings.TrimSuffix(v.endpoint, ":80")
	v.log(LogInformational, "connecting to voice endpoint %s", vg)
	v.wsConn, _, err = v.session.Dialer.Dial(vg, nil)
	if err != nil {
		v.log(LogWarning, "error connecting to voice endpoint %s, %s", vg, err)
		v.log(LogDebug, "voice struct: %#v\n", v)
		return
	}

	type voiceHandshakeData struct {
		ServerID               string `json:"server_id"`
		UserID                 string `json:"user_id"`
		SessionID              string `json:"session_id"`
		Token                  string `json:"token"`
		MaxDaveProtocolVersion uint16 `json:"max_dave_protocol_version,omitempty"`
	}
	type voiceHandshakeOp struct {
		Op   int                `json:"op"` // Always 0
		Data voiceHandshakeData `json:"d"`
	}
	handshake := voiceHandshakeData{
		ServerID:  v.GuildID,
		UserID:    v.UserID,
		SessionID: v.sessionID,
		Token:     v.token,
	}
	if v.daveEnabled {
		handshake.MaxDaveProtocolVersion = dave.MaxSupportedProtocolVersion()
	}
	data := voiceHandshakeOp{0, handshake}

	v.wsMutex.Lock()
	err = v.wsConn.WriteJSON(data)
	v.wsMutex.Unlock()
	if err != nil {
		v.log(LogWarning, "error sending init packet, %s", err)
		return
	}

	v.close = make(chan struct{})
	go v.wsListen(v.wsConn, v.close)

	// add loop/check for Ready bool here?
	// then return false if not ready?
	// but then wsListen will also err.

	return
}

// wsListen listens on the voice websocket for messages and passes them
// to the voice event handler.  This is automatically called by the Open func
func (v *VoiceConnection) wsListen(wsConn *websocket.Conn, close <-chan struct{}) {

	v.log(LogInformational, "called")

	for {
		msgType, message, err := v.wsConn.ReadMessage()
		if err != nil {
			// 4014 indicates a manual disconnection by someone in the guild;
			// we shouldn't reconnect.
			if websocket.IsCloseError(err, 4014) {
				v.log(LogInformational, "received 4014 manual disconnection")

				// Abandon the voice WS connection
				v.Lock()
				v.wsConn = nil
				v.Unlock()

				// Wait for VOICE_SERVER_UPDATE.
				// When the bot is moved by the user to another voice channel,
				// VOICE_SERVER_UPDATE is received after the code 4014.
				for i := 0; i < 5; i++ { // TODO: temp, wait for VoiceServerUpdate.
					<-time.After(1 * time.Second)

					v.RLock()
					reconnected := v.wsConn != nil
					v.RUnlock()
					if !reconnected {
						continue
					}
					v.log(LogInformational, "successfully reconnected after 4014 manual disconnection")
					return
				}

				// When VOICE_SERVER_UPDATE is not received, disconnect as usual.
				v.log(LogInformational, "disconnect due to 4014 manual disconnection")

				v.session.Lock()
				delete(v.session.VoiceConnections, v.GuildID)
				v.session.Unlock()

				v.Close()

				return
			}

			// Detect if we have been closed manually. If a Close() has already
			// happened, the websocket we are listening on will be different to the
			// current session.
			v.RLock()
			sameConnection := v.wsConn == wsConn
			v.RUnlock()
			if sameConnection {

				v.log(LogError, "voice endpoint %s websocket closed unexpectedly, %s", v.endpoint, err)

				// Start reconnect goroutine then exit.
				go v.reconnect()
			}
			return
		}

		// Pass received message to voice event handler
		select {
		case <-close:
			return
		default:
			if msgType == websocket.BinaryMessage {
				go v.onDaveBinaryEvent(message)
			} else {
				go v.onEvent(message)
			}
		}
	}
}

// wsEvent handles any voice websocket events. This is only called by the
// wsListen() function.
func (v *VoiceConnection) onEvent(message []byte) {

	v.log(LogDebug, "received: %s", string(message))

	var e Event
	if err := json.Unmarshal(message, &e); err != nil {
		v.log(LogError, "unmarshall error, %s", err)
		return
	}

	switch e.Operation {

	case 2: // READY

		if err := json.Unmarshal(e.RawData, &v.op2); err != nil {
			v.log(LogError, "OP2 unmarshall error, %s, %s", err, string(e.RawData))
			return
		}

		// Start the voice websocket heartbeat to keep the connection alive
		go v.wsHeartbeat(v.wsConn, v.close, v.op2.HeartbeatInterval)
		// TODO monitor a chan/bool to verify this was successful

		// Start the UDP connection
		err := v.udpOpen()
		if err != nil {
			v.log(LogError, "error opening udp connection, %s", err)
			return
		}

		// Start the opusSender.
		// TODO: Should we allow 48000/960 values to be user defined?
		// answer: no, 48k is required as per discord documentaiton and 960 is the most optimal frame size (based on testing)
		if v.OpusSend == nil {
			v.OpusSend = make(chan []byte, 2)
		}
		go v.opusSender(v.udpConn, v.close, v.OpusSend, 48000, 960)

		// Start the opusReceiver
		if !v.deaf {
			if v.OpusRecv == nil {
				v.OpusRecv = make(chan *Packet, 2)
			}

			go v.opusReceiver(v.udpConn, v.close, v.OpusRecv)
		}

		return

	case 3: // HEARTBEAT response
		// add code to use this to track latency?
		// TODO: maybe actually implement this, seems cool
		return

	case 4: // udp encryption secret key
		v.Lock()
		defer v.Unlock()

		v.op4 = voiceOP4{}
		if err := json.Unmarshal(e.RawData, &v.op4); err != nil {
			v.log(LogError, "OP4 unmarshall error, %s, %s", err, string(e.RawData))
			return
		}

		// TODO: error handling? meh
		block, _ := aes.NewCipher(v.op4.SecretKey[:])
		v.aead, _ = cipher.NewGCM(block)

		// Check for DAVE protocol version in OP4 response
		if v.daveEnabled {
			var daveOP4 struct {
				DaveProtocolVersion uint16 `json:"dave_protocol_version"`
			}
			if err := json.Unmarshal(e.RawData, &daveOP4); err == nil && daveOP4.DaveProtocolVersion > 0 {
				v.daveProtoVersion = daveOP4.DaveProtocolVersion
				v.log(LogInformational, "DAVE protocol version %d negotiated", v.daveProtoVersion)

				if v.daveSession == nil {
					ds, err := dave.NewDAVESession(v.sessionID, v.UserID)
					if err != nil {
						v.log(LogError, "failed to create DAVE session: %s", err)
					} else {
						v.daveSession = ds
						v.daveExternalReady = false
						// Parse GuildID as uint64 for group ID
						gid, _ := strconv.ParseUint(v.GuildID, 10, 64)
						v.daveSession.Init(v.daveProtoVersion, gid)
						go v.flushPendingDaveBinary()
					}
				}
			}
		}

		return

	case 5:
		voiceSpeakingUpdate := &VoiceSpeakingUpdate{}
		if err := json.Unmarshal(e.RawData, voiceSpeakingUpdate); err != nil {
			v.log(LogError, "OP5 unmarshall error, %s, %s", err, string(e.RawData))
			return
		}

		// Register SSRC→userID mapping for DAVE decryptor key assignment
		if v.daveSession != nil && voiceSpeakingUpdate.SSRC != 0 && voiceSpeakingUpdate.UserID != "" {
			v.daveSession.RegisterSSRC(uint32(voiceSpeakingUpdate.SSRC), voiceSpeakingUpdate.UserID)
		}

		for _, h := range v.voiceSpeakingUpdateHandlers {
			h(v, voiceSpeakingUpdate)
		}

	case 21: // DAVE MLS Prepare Transition
		if v.daveSession == nil {
			return
		}
		var d struct {
			TransitionID    uint16 `json:"transition_id"`
			ProtocolVersion uint16 `json:"protocol_version"`
		}
		if err := json.Unmarshal(e.RawData, &d); err != nil {
			v.log(LogError, "OP21 unmarshall error: %s", err)
			return
		}
		v.log(LogInformational, "DAVE prepare transition: id=%d proto=%d", d.TransitionID, d.ProtocolVersion)
		v.Lock()
		v.daveTransitionID = d.TransitionID
		v.Unlock()
		return

	case 22: // DAVE MLS Execute Transition
		if v.daveSession == nil {
			return
		}
		result, err := v.daveSession.HandleExecuteTransition(e.RawData)
		if err != nil {
			v.log(LogError, "DAVE execute transition error: %s", err)
			return
		}
		v.log(LogInformational, "DAVE execute transition: id=%d, sender keys activated", result.TransitionID)
		return

	case 24: // DAVE Prepare Epoch
		if v.daveSession == nil {
			return
		}
		if err := v.daveSession.HandlePrepareEpoch(e.RawData); err != nil {
			v.log(LogError, "DAVE prepare epoch error: %s", err)
		}
		return

	default:
		v.log(LogDebug, "unknown voice operation, %d, %s", e.Operation, string(e.RawData))
	}

	return
}

// onDaveBinaryEvent handles binary websocket messages for DAVE MLS protocol.
// Server-to-client binary frames may use either:
//   - [uint8 opcode][payload] — observed in practice
//   - [uint16 seq][uint8 opcode][payload] — per protocol spec
//
// We detect the format by checking if byte 0 is a known DAVE opcode (25-30).
func (v *VoiceConnection) onDaveBinaryEvent(message []byte) {
	if len(message) < 1 {
		return
	}

	v.Lock()
	hasSession := v.daveSession != nil
	if !hasSession {
		v.pendingDaveBinary = append(v.pendingDaveBinary, append([]byte(nil), message...))
		queued := len(v.pendingDaveBinary)
		v.Unlock()
		v.log(LogWarning, "queued DAVE binary message until session is ready (%d queued)", queued)
		return
	}
	v.Unlock()

	var opcode byte
	var payload []byte

	// Detect binary frame format
	if message[0] >= dave.BinaryOpcodeExternalSender && message[0] <= dave.BinaryOpcodeWelcome {
		// Format: [opcode][payload]
		opcode = message[0]
		payload = message[1:]
	} else if len(message) >= 3 && message[2] >= dave.BinaryOpcodeExternalSender && message[2] <= dave.BinaryOpcodeWelcome {
		// Format: [seq(2)][opcode][payload]
		opcode = message[2]
		payload = message[3:]
	} else {
		v.log(LogWarning, "DAVE: unrecognized binary frame (first bytes: %x)", message[:min(4, len(message))])
		return
	}

	v.log(LogDebug, "DAVE binary opcode %d, payload length %d", opcode, len(payload))

	switch opcode {
	case dave.BinaryOpcodeExternalSender: // 25
		v.log(LogInformational, "DAVE: received external sender (%d bytes)", len(payload))
		keyPackage, err := v.daveSession.HandleExternalSender(payload)
		if err != nil {
			v.log(LogError, "DAVE external sender error: %s", err)
			return
		}
		v.Lock()
		v.daveExternalReady = true
		v.Unlock()
		if err := v.sendDaveBinary(dave.BinaryOpcodeKeyPackage, keyPackage); err != nil {
			v.log(LogError, "DAVE: error sending key package: %s", err)
		} else {
			v.log(LogInformational, "DAVE: sent key package (%d bytes)", len(keyPackage))
		}
		go v.flushPendingDaveBinary()

	case dave.BinaryOpcodeProposals: // 27
		v.RLock()
		externalReady := v.daveExternalReady
		v.RUnlock()
		if !externalReady {
			v.Lock()
			v.pendingDaveBinary = append(v.pendingDaveBinary, append([]byte(nil), message...))
			queued := len(v.pendingDaveBinary)
			v.Unlock()
			v.log(LogWarning, "queued DAVE proposals until external sender is ready (%d queued)", queued)
			return
		}
		v.log(LogInformational, "DAVE: received proposals (%d bytes)", len(payload))
		commitWelcome, err := v.daveSession.HandleProposalsBinary(payload)
		if err != nil {
			v.log(LogError, "DAVE proposals error: %s", err)
			return
		}
		if len(commitWelcome) > 0 {
			if err := v.sendDaveBinary(dave.BinaryOpcodeCommitWelcome, commitWelcome); err != nil {
				v.log(LogError, "DAVE: error sending commit+welcome: %s", err)
			} else {
				v.log(LogInformational, "DAVE: sent commit+welcome (%d bytes)", len(commitWelcome))
			}
			// Committer: send transition ready after generating commit
			v.RLock()
			tid := v.daveTransitionID
			v.RUnlock()
			if tid != 0 {
				v.sendDaveTransitionReady(tid)
			} else {
				v.log(LogInformational, "DAVE initial transition activated from proposals")
			}
		}

	case dave.BinaryOpcodeAnnounceCommit: // 29
		v.log(LogInformational, "DAVE: received announce commit (%d bytes)", len(payload))
		transitionID, err := v.daveSession.HandleCommitBinary(payload)
		if err != nil {
			v.log(LogError, "DAVE commit error: %s", err)
			return
		}
		// After processing commit, send transition ready
		if transitionID != 0 {
			v.sendDaveTransitionReady(transitionID)
		} else {
			v.log(LogInformational, "DAVE initial transition activated from commit")
		}

	case dave.BinaryOpcodeWelcome: // 30
		v.log(LogInformational, "DAVE: received welcome (%d bytes)", len(payload))
		transitionID, err := v.daveSession.HandleWelcomeBinary(payload)
		if err != nil {
			v.log(LogError, "DAVE welcome error: %s", err)
			return
		}
		// After processing welcome, send transition ready
		if transitionID != 0 {
			v.sendDaveTransitionReady(transitionID)
		} else {
			v.log(LogInformational, "DAVE initial transition activated from welcome")
		}

	case dave.BinaryOpcodeCommitWelcome: // 28 (shouldn't normally be received by client)
		v.log(LogWarning, "DAVE: received unexpected commit_welcome opcode 28 (%d bytes)", len(payload))

	default:
		v.log(LogWarning, "DAVE: unknown binary opcode %d", opcode)
	}
}

func (v *VoiceConnection) flushPendingDaveBinary() {
	v.Lock()
	pending := v.pendingDaveBinary
	v.pendingDaveBinary = nil
	v.Unlock()
	if len(pending) == 0 {
		return
	}
	pending = prioritizeDaveBinaryMessages(pending)
	v.log(LogInformational, "replaying %d queued DAVE binary messages", len(pending))
	for _, message := range pending {
		v.onDaveBinaryEvent(message)
	}
}

func prioritizeDaveBinaryMessages(messages [][]byte) [][]byte {
	if len(messages) < 2 {
		return messages
	}
	ordered := make([][]byte, 0, len(messages))
	appendByOpcode := func(target byte) {
		for _, message := range messages {
			if detectDaveBinaryOpcode(message) == target {
				ordered = append(ordered, message)
			}
		}
	}
	appendByOpcode(dave.BinaryOpcodeExternalSender)
	appendByOpcode(dave.BinaryOpcodeProposals)
	appendByOpcode(dave.BinaryOpcodeAnnounceCommit)
	appendByOpcode(dave.BinaryOpcodeWelcome)
	appendByOpcode(dave.BinaryOpcodeCommitWelcome)
	for _, message := range messages {
		opcode := detectDaveBinaryOpcode(message)
		switch opcode {
		case dave.BinaryOpcodeExternalSender,
			dave.BinaryOpcodeProposals,
			dave.BinaryOpcodeAnnounceCommit,
			dave.BinaryOpcodeWelcome,
			dave.BinaryOpcodeCommitWelcome:
			continue
		default:
			ordered = append(ordered, message)
		}
	}
	return ordered
}

func detectDaveBinaryOpcode(message []byte) byte {
	if len(message) < 1 {
		return 0
	}
	if message[0] >= dave.BinaryOpcodeExternalSender && message[0] <= dave.BinaryOpcodeWelcome {
		return message[0]
	}
	if len(message) >= 3 && message[2] >= dave.BinaryOpcodeExternalSender && message[2] <= dave.BinaryOpcodeWelcome {
		return message[2]
	}
	return 0
}

// sendDaveTransitionReady sends voice opcode 23 (transition ready) to the server.
func (v *VoiceConnection) sendDaveTransitionReady(transitionID uint16) {
	type daveTransitionReadyOp struct {
		Op   int                    `json:"op"`
		Data *dave.TransitionResult `json:"d"`
	}
	result := &dave.TransitionResult{TransitionID: transitionID}
	v.wsMutex.Lock()
	err := v.wsConn.WriteJSON(daveTransitionReadyOp{23, result})
	v.wsMutex.Unlock()
	if err != nil {
		v.log(LogError, "error sending DAVE transition ready: %s", err)
	} else {
		v.log(LogInformational, "DAVE: sent transition ready (id=%d)", transitionID)
	}
}

// sendDaveBinary sends a DAVE binary message over the voice websocket.
func (v *VoiceConnection) sendDaveBinary(opcode byte, data []byte) error {
	msg := make([]byte, 1+len(data))
	msg[0] = opcode
	copy(msg[1:], data)

	v.wsMutex.Lock()
	defer v.wsMutex.Unlock()

	if v.wsConn == nil {
		return fmt.Errorf("no voice websocket connection")
	}
	return v.wsConn.WriteMessage(websocket.BinaryMessage, msg)
}

type voiceHeartbeatOp struct {
	Op   int `json:"op"` // Always 3
	Data int `json:"d"`
}

// NOTE :: When a guild voice server changes how do we shut this down
// properly, so a new connection can be setup without fuss?
//
// wsHeartbeat sends regular heartbeats to voice Discord so it knows the client
// is still connected.  If you do not send these heartbeats Discord will
// disconnect the websocket connection after a few seconds.
func (v *VoiceConnection) wsHeartbeat(wsConn *websocket.Conn, close <-chan struct{}, i time.Duration) {

	if close == nil || wsConn == nil {
		return
	}

	var err error
	ticker := time.NewTicker(i * time.Millisecond)
	defer ticker.Stop()
	for {
		v.log(LogDebug, "sending heartbeat packet")
		v.wsMutex.Lock()
		err = wsConn.WriteJSON(voiceHeartbeatOp{3, int(time.Now().Unix())})
		v.wsMutex.Unlock()
		if err != nil {
			v.log(LogError, "error sending heartbeat to voice endpoint %s, %s", v.endpoint, err)
			return
		}

		select {
		case <-ticker.C:
			// continue loop and send heartbeat
		case <-close:
			return
		}
	}
}

// ------------------------------------------------------------------------------------------------
// Code related to the VoiceConnection UDP connection
// ------------------------------------------------------------------------------------------------

type voiceUDPData struct {
	Address string `json:"address"` // Public IP of machine running this code
	Port    uint16 `json:"port"`    // UDP Port of machine running this code
	Mode    string `json:"mode"`    // always "xsalsa20_poly1305"
}

type voiceUDPD struct {
	Protocol string       `json:"protocol"` // Always "udp" ?
	Data     voiceUDPData `json:"data"`
}

type voiceUDPOp struct {
	Op   int       `json:"op"` // Always 1
	Data voiceUDPD `json:"d"`
}

// udpOpen opens a UDP connection to the voice server and completes the
// initial required handshake.  This connection is left open in the session
// and can be used to send or receive audio.  This should only be called
// from voice.wsEvent OP2
func (v *VoiceConnection) udpOpen() (err error) {

	v.Lock()
	defer v.Unlock()

	if v.wsConn == nil {
		return fmt.Errorf("nil voice websocket")
	}

	if v.udpConn != nil {
		return fmt.Errorf("udp connection already open")
	}

	if v.close == nil {
		return fmt.Errorf("nil close channel")
	}

	if v.endpoint == "" {
		return fmt.Errorf("empty endpoint")
	}

	host := v.op2.IP + ":" + strconv.Itoa(v.op2.Port)
	addr, err := net.ResolveUDPAddr("udp", host)
	if err != nil {
		v.log(LogWarning, "error resolving udp host %s, %s", host, err)
		return
	}

	v.log(LogInformational, "connecting to udp addr %s", addr.String())
	v.udpConn, err = net.DialUDP("udp", nil, addr)
	if err != nil {
		v.log(LogWarning, "error connecting to udp addr %s, %s", addr.String(), err)
		return
	}

	// Create a 74 byte array to store the packet data
	sb := make([]byte, 74)
	binary.BigEndian.PutUint16(sb, 1)              // Packet type (0x1 is request, 0x2 is response)
	binary.BigEndian.PutUint16(sb[2:], 70)         // Packet length (excluding type and length fields)
	binary.BigEndian.PutUint32(sb[4:], v.op2.SSRC) // The SSRC code from the Op 2 VoiceConnection event

	// And send that data over the UDP connection to Discord.
	_, err = v.udpConn.Write(sb)
	if err != nil {
		v.log(LogWarning, "udp write error to %s, %s", addr.String(), err)
		return
	}

	// Create a 74-byte array and listen for the initial handshake response
	// from Discord.  Once we get it parse the IP and PORT information out
	// of the response.  This should be our public IP and PORT as Discord
	// saw us.
	rb := make([]byte, 74)
	rlen, _, err := v.udpConn.ReadFromUDP(rb)
	if err != nil {
		v.log(LogWarning, "udp read error, %s, %s", addr.String(), err)
		return
	}

	if rlen < 74 {
		v.log(LogWarning, "received udp packet too small")
		return fmt.Errorf("received udp packet too small")
	}

	// Loop over position 8 through 71 to grab the IP address.
	var ip string
	for i := 8; i < len(rb)-2; i++ {
		if rb[i] == 0 {
			break
		}
		ip += string(rb[i])
	}

	// Grab port from position 72 and 73
	port := binary.BigEndian.Uint16(rb[len(rb)-2:])

	// Take the data from above and send it back to Discord to finalize
	// the UDP connection handshake.

	// AEAD AES256-GCM (RTP Size)	aead_aes256_gcm_rtpsize	32-bit incremental integer value, appended to payload	Available (Preferred)
	data := voiceUDPOp{1, voiceUDPD{"udp", voiceUDPData{ip, port, "aead_aes256_gcm_rtpsize"}}}

	v.wsMutex.Lock()
	err = v.wsConn.WriteJSON(data)
	v.wsMutex.Unlock()
	if err != nil {
		v.log(LogWarning, "udp write error, %#v, %s", data, err)
		return
	}

	// start udpKeepAlive
	go v.udpKeepAlive(v.udpConn, v.close, 5*time.Second)
	// TODO: find a way to check that it fired off okay

	return
}

// udpKeepAlive sends a udp packet to keep the udp connection open
// This is still a bit of a "proof of concept"
func (v *VoiceConnection) udpKeepAlive(udpConn *net.UDPConn, close <-chan struct{}, i time.Duration) {

	if udpConn == nil || close == nil {
		return
	}

	var err error
	var sequence uint64

	packet := make([]byte, 8)

	ticker := time.NewTicker(i)
	defer ticker.Stop()
	for {

		binary.LittleEndian.PutUint64(packet, sequence)
		sequence++

		_, err = udpConn.Write(packet)
		if err != nil {
			v.log(LogError, "write error, %s", err)
			return
		}

		select {
		case <-ticker.C:
			// continue loop and send keepalive
		case <-close:
			return
		}
	}
}

// opusSender will listen on the given channel and send any
// pre-encoded opus audio to Discord.  Supposedly.
func (v *VoiceConnection) opusSender(udpConn *net.UDPConn, close <-chan struct{}, opus <-chan []byte, rate, size int) {

	if udpConn == nil || close == nil {
		return
	}

	// VoiceConnection is now ready to receive audio packets
	// TODO: this needs reviewing as I think there must be a better way.
	v.Lock()
	v.Ready = true
	v.Unlock()
	defer func() {
		v.Lock()
		v.Ready = false
		v.Unlock()
	}()

	var sequence uint16
	var timestamp uint32
	var recvbuf []byte
	var ok bool
	udpHeader := make([]byte, 12)
	nonce := make([]byte, 12)

	// build the parts that don't change in the udpHeader
	udpHeader[0] = 0x80
	udpHeader[1] = 0x78
	binary.BigEndian.PutUint32(udpHeader[8:], v.op2.SSRC)

	// start a send loop that loops until buf chan is closed
	ticker := time.NewTicker(time.Millisecond * time.Duration(size/(rate/1000)))
	defer ticker.Stop()
	for {

		// Get data from chan.  If chan is closed, return.
		select {
		case <-close:
			return
		case recvbuf, ok = <-opus:
			if !ok {
				return
			}
			// else, continue loop
		}

		v.RLock()
		speaking := v.speaking
		v.RUnlock()
		if !speaking {
			err := v.Speaking(true)
			if err != nil {
				v.log(LogError, "error sending speaking packet, %s", err)
			}
		}

		// Add sequence and timestamp to udpPacket
		binary.BigEndian.PutUint16(udpHeader[2:], sequence)
		binary.BigEndian.PutUint32(udpHeader[4:], timestamp)

		// DAVE E2EE: encrypt opus frame before transport encryption
		if v.daveSession != nil {
			var daveErr error
			recvbuf, daveErr = v.daveSession.EncryptOpusFrame(v.op2.SSRC, recvbuf)
			if daveErr != nil {
				v.log(LogError, "DAVE encrypt error: %s", daveErr)
				continue
			}
		}

		// encrypt the opus data
		// add incrementing nonce counter as per discord's requirements
		binary.LittleEndian.PutUint32(nonce[:4], v.nonceCounter)
		v.nonceCounter++

		sendbuf := v.aead.Seal(nil, nonce, recvbuf, udpHeader)
		sendbuf = append(sendbuf, nonce[:4]...) // 4 byte nonce to ciphertext appended
		sendbuf = append(udpHeader, sendbuf...) // final

		// block here until we're exactly at the right time :)
		// Then send rtp audio packet to Discord over UDP
		select {
		case <-close:
			return
		case <-ticker.C:
			// continue
		}
		_, err := udpConn.Write(sendbuf)

		if err != nil {
			v.log(LogError, "udp write error, %s", err)
			v.log(LogDebug, "voice struct: %#v\n", v)
			return
		}

		if (sequence) == 0xFFFF {
			sequence = 0
		} else {
			sequence++
		}

		if (timestamp + uint32(size)) >= 0xFFFFFFFF {
			timestamp = 0
		} else {
			timestamp += uint32(size)
		}
	}
}

// A Packet contains the headers and content of a received voice packet.
type Packet struct {
	SSRC      uint32
	Sequence  uint16
	Timestamp uint32
	Type      []byte
	Opus      []byte
	PCM       []int16
}

// opusReceiver listens on the UDP socket for incoming packets
// and sends them across the given channel
// NOTE :: This function may change names later.
func (v *VoiceConnection) opusReceiver(udpConn *net.UDPConn, close <-chan struct{}, c chan *Packet) {

	if udpConn == nil || close == nil {
		return
	}

	recvbuf := make([]byte, 2048)
	var nonce [12]byte
	var udpReads int
	var rtpReads int
	var nonRTPDrops int
	var shortHeaderDrops int
	var shortPayloadDrops int
	var noAEADDrops int
	var transportDecryptDrops int
	var daveDecryptDrops int

	for {
		rlen, err := udpConn.Read(recvbuf)
		if err != nil {
			// Detect if we have been closed manually. If a Close() has already
			// happened, the udp connection we are listening on will be different
			// to the current session.
			v.RLock()
			sameConnection := v.udpConn == udpConn
			v.RUnlock()
			if sameConnection {

				v.log(LogError, "udp read error, %s, %s", v.endpoint, err)
				v.log(LogDebug, "voice struct: %#v\n", v)

				go v.reconnect()
			}
			return
		}
		udpReads++

		select {
		case <-close:
			return
		default:
			// continue loop
		}

		// For now, skip anything except RTP v2 packets (audio).
		// RTP v2 => top two bits are 10 (0x80).
		if rlen < 12 || (recvbuf[0]&0xC0) != 0x80 {
			nonRTPDrops++
			if nonRTPDrops <= 3 {
				v.log(LogDebug, "voice udp packet skipped: not RTP audio (len=%d first_byte=0x%02x udp_reads=%d count=%d)", rlen, recvbuf[0], udpReads, nonRTPDrops)
			}
			continue
		}
		rtpReads++
		if rtpReads == 1 {
			v.log(LogInformational, "voice first RTP packet received (%d bytes)", rlen)
		}

		// build a audio packet struct
		p := Packet{}
		p.Type = recvbuf[0:2]
		p.Sequence = binary.BigEndian.Uint16(recvbuf[2:4])
		p.Timestamp = binary.BigEndian.Uint32(recvbuf[4:8])
		p.SSRC = binary.BigEndian.Uint32(recvbuf[8:12])

		// RTP header parsing for *_rtpsize AEAD modes:
		// - base RTP header is 12 bytes + 4 bytes per CSRC (CC).
		// - if extension bit (X) is set, ONLY the 4-byte extension preamble is unencrypted/AAD;
		//   the extension payload is encrypted and must be stripped after decryption.
		cc := int(recvbuf[0] & 0x0F)
		hasExt := (recvbuf[0] & 0x10) != 0

		baseHeaderLen := 12 + (4 * cc)
		if rlen < baseHeaderLen {
			shortHeaderDrops++
			if shortHeaderDrops <= 3 {
				v.log(LogDebug, "voice udp packet skipped: short RTP header (len=%d need=%d count=%d)", rlen, baseHeaderLen, shortHeaderDrops)
			}
			continue
		}

		aadLen := baseHeaderLen
		extPayloadBytes := 0
		if hasExt {
			if rlen < baseHeaderLen+4 {
				shortHeaderDrops++
				if shortHeaderDrops <= 3 {
					v.log(LogDebug, "voice udp packet skipped: short RTP extension preamble (len=%d need=%d count=%d)", rlen, baseHeaderLen+4, shortHeaderDrops)
				}
				continue
			}
			// Extension length is in 32-bit words at the end of the extension preamble.
			extLenWords := int(binary.BigEndian.Uint16(recvbuf[baseHeaderLen+2 : baseHeaderLen+4]))
			extPayloadBytes = extLenWords * 4
			aadLen = baseHeaderLen + 4
		}

		if rlen < aadLen+4 {
			shortPayloadDrops++
			if shortPayloadDrops <= 3 {
				v.log(LogDebug, "voice udp packet skipped: short encrypted payload (len=%d aad=%d count=%d)", rlen, aadLen, shortPayloadDrops)
			}
			continue
		}

		// decrypt opus data
		payload := recvbuf[aadLen:rlen]
		if len(payload) < 4 {
			shortPayloadDrops++
			if shortPayloadDrops <= 3 {
				v.log(LogDebug, "voice udp packet skipped: payload missing nonce suffix (len=%d count=%d)", len(payload), shortPayloadDrops)
			}
			continue
		}
		nonceCounter := payload[len(payload)-4:]
		cipherTextPayload := payload[:len(payload)-4]

		binary.LittleEndian.PutUint32(nonce[:4], binary.LittleEndian.Uint32(nonceCounter))

		if v.aead == nil {
			noAEADDrops++
			if noAEADDrops <= 3 {
				v.log(LogDebug, "voice udp packet skipped: transport AEAD not ready (count=%d)", noAEADDrops)
			}
			continue
		}
		// AAD must cover the unencrypted header portion.
		if plain, err := v.aead.Open(nil, nonce[:], cipherTextPayload, recvbuf[:aadLen]); err == nil {
			// If header extensions are present, strip decrypted extension payload to get to Opus.
			if extPayloadBytes > 0 {
				if len(plain) < extPayloadBytes {
					continue
				}
				plain = plain[extPayloadBytes:]
			}

			// DAVE E2EE: decrypt after transport decryption
			if v.daveSession != nil {
				decrypted, daveErr := v.daveSession.DecryptOpusFrame(p.SSRC, plain)
				if daveErr != nil {
					daveDecryptDrops++
					if daveDecryptDrops <= 5 {
						v.log(LogDebug, "DAVE decrypt error for SSRC %d: %s (count=%d)", p.SSRC, daveErr, daveDecryptDrops)
					}
					continue
				}
				plain = decrypted
			}

			p.Opus = plain
		} else {
			continue
		}

		if c != nil {
			select {
			case c <- &p:
			case <-close:
				return
			}
		}
		transportDecryptDrops++
		if transportDecryptDrops <= 5 {
			v.log(LogDebug, "voice transport decrypt failed for SSRC %d (count=%d)", p.SSRC, transportDecryptDrops)
		}
	}
}

// Reconnect will close down a voice connection then immediately try to
// reconnect to that session.
// NOTE : This func is messy and a WIP while I find what works.
// It will be cleaned up once a proven stable option is flushed out.
// aka: this is ugly shit code, please don't judge too harshly.
func (v *VoiceConnection) reconnect() {

	v.log(LogInformational, "called")

	v.Lock()
	if v.reconnecting {
		v.log(LogInformational, "already reconnecting to channel %s, exiting", v.ChannelID)
		v.Unlock()
		return
	}
	v.reconnecting = true
	v.Unlock()

	defer func() {
		v.Lock()
		v.reconnecting = false
		v.Unlock()
	}()

	// Close any currently open connections
	v.Close()

	wait := time.Duration(1)
	for {

		<-time.After(wait * time.Second)
		wait *= 2
		if wait > 600 {
			wait = 600
		}

		if v.session.DataReady == false || v.session.wsConn == nil {
			v.log(LogInformational, "cannot reconnect to channel %s with unready session", v.ChannelID)
			continue
		}

		v.log(LogInformational, "trying to reconnect to channel %s", v.ChannelID)

		_, err := v.session.ChannelVoiceJoin(v.GuildID, v.ChannelID, v.mute, v.deaf)
		if err == nil {
			v.log(LogInformational, "successfully reconnected to channel %s", v.ChannelID)
			return
		}

		v.log(LogInformational, "error reconnecting to channel %s, %s", v.ChannelID, err)

		// if the reconnect above didn't work lets just send a disconnect
		// packet to reset things.
		// Send a OP4 with a nil channel to disconnect
		data := voiceChannelJoinOp{4, voiceChannelJoinData{&v.GuildID, nil, true, true}}
		v.session.wsMutex.Lock()
		err = v.session.wsConn.WriteJSON(data)
		v.session.wsMutex.Unlock()
		if err != nil {
			v.log(LogError, "error sending disconnect packet, %s", err)
		}

	}
}
