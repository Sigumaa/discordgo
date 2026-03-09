package discordgo

import "testing"

func TestTrimDecryptedVoicePayload(t *testing.T) {
	t.Run("no extension", func(t *testing.T) {
		got, ok := trimDecryptedVoicePayload([]byte{1, 2, 3}, 0)
		if !ok {
			t.Fatal("expected success without extension payload")
		}
		if len(got) != 3 {
			t.Fatalf("expected unchanged payload length, got %d", len(got))
		}
	})

	t.Run("strip extension", func(t *testing.T) {
		got, ok := trimDecryptedVoicePayload([]byte{1, 2, 3, 4, 5}, 2)
		if !ok {
			t.Fatal("expected success when extension payload is present")
		}
		if len(got) != 3 || got[0] != 3 {
			t.Fatalf("expected stripped payload, got %#v", got)
		}
	})

	t.Run("fallback when extension length is invalid", func(t *testing.T) {
		plain := []byte{1, 2, 3}
		got, ok := trimDecryptedVoicePayload(plain, 8)
		if ok {
			t.Fatal("expected invalid extension length to report failure")
		}
		if len(got) != len(plain) {
			t.Fatalf("expected original payload on fallback, got %d bytes", len(got))
		}
	})
}

func TestInferSingleRemoteUserID(t *testing.T) {
	session, err := New("Bot test")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	if err := session.State.GuildAdd(&Guild{
		ID: "guild",
		VoiceStates: []*VoiceState{
			{GuildID: "guild", ChannelID: "voice", UserID: "bot"},
			{GuildID: "guild", ChannelID: "voice", UserID: "user1"},
		},
	}); err != nil {
		t.Fatalf("guild add: %v", err)
	}

	vc := &VoiceConnection{
		UserID:    "bot",
		GuildID:   "guild",
		ChannelID: "voice",
		session:   session,
	}

	if got := vc.inferSingleRemoteUserID(); got != "user1" {
		t.Fatalf("expected single remote user, got %q", got)
	}
}

func TestInferSingleRemoteUserIDReturnsEmptyForMultipleCandidates(t *testing.T) {
	session, err := New("Bot test")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	if err := session.State.GuildAdd(&Guild{
		ID: "guild",
		VoiceStates: []*VoiceState{
			{GuildID: "guild", ChannelID: "voice", UserID: "bot"},
			{GuildID: "guild", ChannelID: "voice", UserID: "user1"},
			{GuildID: "guild", ChannelID: "voice", UserID: "user2"},
		},
	}); err != nil {
		t.Fatalf("guild add: %v", err)
	}

	vc := &VoiceConnection{
		UserID:    "bot",
		GuildID:   "guild",
		ChannelID: "voice",
		session:   session,
	}

	if got := vc.inferSingleRemoteUserID(); got != "" {
		t.Fatalf("expected no candidate for multiple users, got %q", got)
	}
}

func TestShouldRetryDaveDecryptInference(t *testing.T) {
	if !shouldRetryDaveDecryptInference(assertErr("dave: no decryptor for SSRC 42")) {
		t.Fatal("expected no-decryptor error to allow inference retry")
	}
	if shouldRetryDaveDecryptInference(assertErr("dave: decrypt failed")) {
		t.Fatal("expected generic decrypt error to skip inference retry")
	}
}

func TestShouldPassthroughDaveDecrypt(t *testing.T) {
	if !shouldPassthroughDaveDecrypt(assertErr("dave: decrypt failed with code 1"), []byte{1, 2, 3, 4}) {
		t.Fatal("expected decrypt failure code 1 to allow passthrough for audio frame")
	}
	if shouldPassthroughDaveDecrypt(assertErr("dave: decrypt failed with code 1"), []byte{1, 2, 3}) {
		t.Fatal("expected comfort-noise frame not to passthrough")
	}
	if shouldPassthroughDaveDecrypt(assertErr("dave: no decryptor for SSRC 42"), []byte{1, 2, 3, 4}) {
		t.Fatal("expected missing decryptor to avoid passthrough fallback")
	}
}

type assertErr string

func (e assertErr) Error() string {
	return string(e)
}
