package contracts

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"regexp"
)

// Identifiers are version 4 UUIDs in lower-case canonical text
// ("3f2a7c1e-5b4d-4e8f-9a6b-1c2d3e4f5a6b").
type (
	// TorrentID identifies a library record; Go generates it when the import is confirmed.
	TorrentID string
	// PendingImportID identifies an import preview before confirmation; Go generates it.
	PendingImportID string
	// RuntimeID identifies one run of the Go application (not the installation, not analytics);
	// revisions are compared only within one runtime.
	RuntimeID string
	// OperationID identifies one user intention to mutate; the frontend generates it and repeats
	// it when it retries, which makes the mutation idempotent.
	OperationID string
)

var (
	uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	// A v1 infohash is the SHA-1 of the info dictionary, a v2 infohash the full SHA-256; both are
	// lower-case hex on the wire.
	infohashV1 = regexp.MustCompile(`^[0-9a-f]{40}$`)
	infohashV2 = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// IsUUID reports whether s is a version 4 UUID in canonical lower-case text.
func IsUUID(s string) bool { return uuidV4.MatchString(s) }

// IsInfohashV1 reports whether s is a full v1 (SHA-1) infohash in lower-case hex.
func IsInfohashV1(s string) bool { return infohashV1.MatchString(s) }

// IsInfohashV2 reports whether s is a full v2 (SHA-256) infohash in lower-case hex.
func IsInfohashV2(s string) bool { return infohashV2.MatchString(s) }

// NewUUID reads 16 bytes from random and formats them as a version 4 UUID (RFC 9562).
func NewUUID(random io.Reader) (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(random, b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// NewRuntimeID makes the identity of this Go run. crypto/rand.Reader does not fail (it aborts the
// program instead, since Go 1.24), so the error is impossible here.
func NewRuntimeID() RuntimeID {
	id, _ := NewUUID(rand.Reader)
	return RuntimeID(id)
}
