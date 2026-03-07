package dave

// DAVE frame constants (matching discord::dave::common.h)
const (
	MediaTypeAudio = 0
	MediaTypeVideo = 1

	AesGcm128KeyBytes                = 16
	AesGcm128NonceBytes              = 12
	AesGcm128TruncatedSyncNonceBytes = 4
	AesGcm128TruncatedTagBytes       = 8

	MarkerBytes = 0xFAFA

	// kSupplementalBytes = truncated_tag(8) + supplemental_bytes_size(1) + magic_marker(2)
	SupplementalBytes = AesGcm128TruncatedTagBytes + 1 + 2

	DefaultTransitionExpiryMs = 10000 // 10 seconds
)

// DAVE voice gateway binary opcodes
const (
	BinaryOpcodeExternalSender  = 25
	BinaryOpcodeKeyPackage      = 26
	BinaryOpcodeProposals       = 27
	BinaryOpcodeCommitWelcome   = 28
	BinaryOpcodeAnnounceCommit  = 29
	BinaryOpcodeWelcome         = 30
)

// CommitResultType represents the result of processing an MLS commit.
type CommitResultType int

const (
	CommitFailed  CommitResultType = 0
	CommitIgnored CommitResultType = 1
	CommitOK      CommitResultType = 2
)

// RosterEntry represents a single roster entry from commit/welcome processing.
type RosterEntry struct {
	UserID uint64
	Key    []byte // empty = user removed
}
