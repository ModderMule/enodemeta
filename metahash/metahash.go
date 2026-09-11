// Package metahash implements the eD2K meta hash — the 16-byte pseudo-hash
// that lets a torrent or an NZB release occupy the MD4 slot of an ordinary eD2K
// search result.
//
// The scheme is normative across three repositories: eNode-go mints these
// hashes, eMuleQt verifies them, and this crawler supplies the identity they
// are folded from. It is specified in eNode-go's
// docs/meta-search-torrent-usenet-plan.local.md §3, with the client side in §8.
// testdata/meta-hash-vectors.json is the cross-repo check: both implementations
// must reproduce that file exactly.
//
// # Layout (§3.1)
//
//	offset  size  field
//	     0     2  magic 0xED 0x2B
//	     2     1  version
//	     3     1  kind (high nibble) | flags (low nibble)
//	     4     2  file index, uint16 little-endian
//	     6    10  fold10 of the release identity
//
// # What a meta hash is not (§3.7)
//
// It is not an eMule file hash and can never be checked against file content.
// A capable client must never publish one to Kad, never offer one to a server,
// never write one to known.met, and never use one as a transfer id: the engine
// mints its own bt:v1: / bt:v2: / nzb: id and keeps the meta hash only as
// provenance. RejectOfferedFile is the server-side half of that rule.
package metahash

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Size is the length of a meta hash: the MD4 slot it occupies in a search
// result is positional and unconditional, so it is exactly 16 bytes.
const Size = 16

// DigestSize is the folded identity's length, in bytes.
const DigestSize = 10

// The magic marking a hash slot as a meta hash (§3.1).
const (
	Magic0 = 0xED
	Magic1 = 0x2B
)

// Version1 is the scheme version this package implements. A client must ignore
// a row whose version it does not know — that rule is what makes a version 2
// deployable at all.
const Version1 = 0x01

// FileIndexWholeSet marks a row that stands for the whole release rather than
// one file inside it.
//
// In the hash it is the uint16 0xFFFF; in a MetaEntry, whose file_index is a
// uint32, it is 0xFFFFFFFF. Build accepts either spelling.
const (
	FileIndexWholeSet   = uint16(0xFFFF)
	FileIndexWholeSet32 = uint32(0xFFFFFFFF)
)

// Kind is the network a meta row stands for (§3.1). It is carried both in the
// hash and, authoritatively, in the FT_META_KIND tag.
type Kind uint8

// The kinds defined by version 1.
const (
	KindUnspecified Kind = 0
	KindBTV1        Kind = 1 // BitTorrent v1 or hybrid; identity is the 20-byte v1 infohash
	KindBTV2        Kind = 2 // BitTorrent v2 only; identity is the 32-byte v2 infohash
	KindNZB         Kind = 3 // Usenet; identity is the 32-byte canonical NZB digest (§3.4)
)

// String names the kind.
func (k Kind) String() string {
	switch k {
	case KindBTV1:
		return "bt-v1"
	case KindBTV2:
		return "bt-v2"
	case KindNZB:
		return "nzb"
	default:
		return fmt.Sprintf("kind(%d)", uint8(k))
	}
}

// Valid reports whether the kind is one this version defines.
func (k Kind) Valid() bool {
	return k == KindBTV1 || k == KindBTV2 || k == KindNZB
}

// IdentityLen is the exact length of the identity a kind folds, in bytes.
func (k Kind) IdentityLen() int {
	switch k {
	case KindBTV1:
		return 20
	case KindBTV2, KindNZB:
		return 32
	default:
		return 0
	}
}

// Flags is the low nibble of byte 3 (§3.1).
type Flags uint8

// The flags defined by version 1.
const (
	// FlagMultiFile marks a release whose metafile holds more than one
	// selectable file.
	FlagMultiFile Flags = 0x1

	// FlagPathAuthoritative says FT_META_FILEPATH is authoritative over the
	// file index. Set it whenever the index cannot be trusted on its own: an
	// NZB's file order is not fixed, and a release with more than 65535 files
	// cannot be addressed by the hash's 16-bit index at all.
	FlagPathAuthoritative Flags = 0x2

	// FlagProtected marks a password-protected or obfuscated release.
	FlagProtected Flags = 0x4

	// FlagReserved must be zero. Parse rejects a hash that sets it, which is
	// what reserves it for a later version.
	FlagReserved Flags = 0x8

	flagMask Flags = 0x0F
)

// String lists the set flags.
func (f Flags) String() string {
	if f == 0 {
		return "none"
	}

	var set []string
	if f&FlagMultiFile != 0 {
		set = append(set, "multi-file")
	}
	if f&FlagPathAuthoritative != 0 {
		set = append(set, "path-authoritative")
	}
	if f&FlagProtected != 0 {
		set = append(set, "protected")
	}
	if f&FlagReserved != 0 {
		set = append(set, "reserved")
	}

	return strings.Join(set, "|")
}

// Errors returned by this package. They are distinguishable because callers act
// differently on each: an unknown version is dropped silently (§8.1), while a
// bad magic means the hash was never a meta hash in the first place.
var (
	ErrLength          = errors.New("metahash: a meta hash is 16 bytes")
	ErrMagic           = errors.New("metahash: wrong magic")
	ErrVersion         = errors.New("metahash: unknown scheme version")
	ErrKind            = errors.New("metahash: unknown kind")
	ErrReservedFlag    = errors.New("metahash: reserved flag is set")
	ErrIdentityLength  = errors.New("metahash: identity has the wrong length for its kind")
	ErrFileIndex       = errors.New("metahash: file index collides with the whole-set marker")
	ErrIdentityMissing = errors.New("metahash: identity is required")
)

// Hash is a meta hash.
type Hash [Size]byte

// Parsed is a meta hash taken apart (§3.1).
type Parsed struct {
	Version   uint8
	Kind      Kind
	Flags     Flags
	FileIndex uint16
	Digest    [DigestSize]byte
}

// WholeSet reports whether this row stands for the whole release.
func (p Parsed) WholeSet() bool {
	return p.FileIndex == FileIndexWholeSet
}

// Fold XOR-folds a digest down to n bytes (§3.2).
//
//	out[i mod n] ^= d[i]
//
// One function for any input length: for a 20-byte SHA-1 and n=10 it degenerates
// to "XOR the two halves", and for a 32-byte SHA-256 it is three passes, the
// last partial. XOR-folding a cryptographic digest preserves uniformity, since
// each output byte is the XOR of independent uniform bytes, so the result
// behaves as a random tag of that width.
func Fold(d []byte, n int) []byte {
	if n <= 0 {
		return nil
	}

	out := make([]byte, n)
	for i, b := range d {
		out[i%n] ^= b
	}

	return out
}

// Fold10 is Fold with n = 10, the width the digest field holds.
func Fold10(d []byte) [DigestSize]byte {
	var out [DigestSize]byte
	for i, b := range d {
		out[i%DigestSize] ^= b
	}

	return out
}

// Build mints a meta hash (§3.1).
//
// The identity must be the whole release's identity — the torrent's infohash or
// the NZB's canonical digest — never the selected file's. Two rows pointing at
// two files of one torrent therefore share their digest and differ only in the
// index, which is what lets a client group them before fetching anything (§3.4).
//
// fileIndex takes either the uint32 spelling a MetaEntry carries or the uint16
// the hash holds. For a release with more than 65535 files the hash keeps the
// low 16 bits and FT_META_FILEINDEX carries the truth, so the caller must also
// set FlagPathAuthoritative — Build enforces that rather than minting a hash
// that addresses the wrong file.
func Build(kind Kind, flags Flags, fileIndex uint32, identity []byte) (Hash, error) {
	var h Hash

	if !kind.Valid() {
		return h, fmt.Errorf("%w: %d", ErrKind, uint8(kind))
	}
	if len(identity) == 0 {
		return h, ErrIdentityMissing
	}
	if want := kind.IdentityLen(); len(identity) != want {
		return h, fmt.Errorf("%w: %s takes %d bytes, got %d", ErrIdentityLength, kind, want, len(identity))
	}
	if flags&FlagReserved != 0 {
		return h, ErrReservedFlag
	}

	index16, err := indexFor(fileIndex, flags)
	if err != nil {
		return h, err
	}

	h[0] = Magic0
	h[1] = Magic1
	h[2] = Version1
	h[3] = byte(kind)<<4 | byte(flags&flagMask)
	binary.LittleEndian.PutUint16(h[4:6], index16)

	digest := Fold10(identity)
	copy(h[6:], digest[:])

	return h, nil
}

// Parse takes a meta hash apart, rejecting anything that is not one.
//
// The hash marker is a self-consistency check and a recovery path, not the
// authority: an MD4 file hash is effectively uniform, so roughly one real file
// in a million looks like a meta hash by chance (§3.3). FT_META_KIND decides,
// and CrossCheck is how the two are reconciled.
func Parse(b []byte) (Parsed, error) {
	var p Parsed

	if len(b) != Size {
		return p, fmt.Errorf("%w: got %d", ErrLength, len(b))
	}
	if b[0] != Magic0 || b[1] != Magic1 {
		return p, fmt.Errorf("%w: %02x %02x", ErrMagic, b[0], b[1])
	}
	if b[2] != Version1 {
		return p, fmt.Errorf("%w: %d", ErrVersion, b[2])
	}

	kind := Kind(b[3] >> 4)
	if !kind.Valid() {
		return p, fmt.Errorf("%w: %d", ErrKind, uint8(kind))
	}

	flags := Flags(b[3]) & flagMask
	if flags&FlagReserved != 0 {
		return p, ErrReservedFlag
	}

	p = Parsed{
		Version:   b[2],
		Kind:      kind,
		Flags:     flags,
		FileIndex: binary.LittleEndian.Uint16(b[4:6]),
	}
	copy(p.Digest[:], b[6:])

	return p, nil
}

// IsMetaHash reports whether a 16-byte hash parses as a meta hash of a version
// and kind this package knows.
//
// Use it as a marker test — "might this row be a meta row?" — and never as the
// decision itself. Two callers need exactly this: a client cross-checking a row
// it was sent, and a server refusing to let one be offered as a real file.
func IsMetaHash(b []byte) bool {
	_, err := Parse(b)

	return err == nil
}

// RejectOfferedFile reports whether a server must refuse this hash in
// OP_OFFERFILES (§9.1).
//
// A stock client cannot send one — a pseudo-hash download has no hashset and
// never enters the shared-file map — but a modified one can, and the result
// would be a catalogue row acquiring "sources" that serve nothing.
func RejectOfferedFile(hash []byte) bool {
	return IsMetaHash(hash)
}

// Digest returns the folded identity carried in the hash.
func (h Hash) Digest() [DigestSize]byte {
	var d [DigestSize]byte
	copy(d[:], h[6:])

	return d
}

// SameRelease reports whether two meta hashes stand for the same release.
//
// It compares the digest alone, so two rows selecting different files of one
// torrent match. A client can group its results this way before fetching
// anything (§3.4).
func (h Hash) SameRelease(other Hash) bool {
	return h.Digest() == other.Digest()
}

// VerifyIdentity reports whether an identity folds to this hash's digest.
//
// This is the check that binds a row to the bytes behind it. It cannot catch a
// server that lies consistently, since the same server supplies both halves;
// what it catches is a *different* origin — a cache, a CDN, a proxy, or a man
// in the middle on a plaintext fetch — serving something other than what was
// advertised (§8.4).
func (h Hash) VerifyIdentity(identity []byte) bool {
	return h.Digest() == Fold10(identity)
}

// Bytes returns the hash as a slice.
func (h Hash) Bytes() []byte {
	out := make([]byte, Size)
	copy(out, h[:])

	return out
}

// String is the uppercase hex spelling. eD2K hashes are uppercase everywhere in
// this project's ecosystem, and a lowercase one has silently failed a string
// comparison in eMuleQt before.
func (h Hash) String() string {
	return strings.ToUpper(hex.EncodeToString(h[:]))
}

// FromBytes builds a Hash from a 16-byte slice without interpreting it.
func FromBytes(b []byte) (Hash, error) {
	var h Hash
	if len(b) != Size {
		return h, fmt.Errorf("%w: got %d", ErrLength, len(b))
	}
	copy(h[:], b)

	return h, nil
}

// ParseHex reads a hash from its hex spelling, in either case.
func ParseHex(s string) (Hash, error) {
	var h Hash

	raw, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return h, fmt.Errorf("metahash: %w", err)
	}

	return FromBytes(raw)
}

// CrossCheck compares a parsed hash against the tags a row carried (§8.1).
//
// A capable client drops a row whose tags and hash disagree: it means either a
// buggy server or a real MD4 that happened to look like a meta hash. The tags
// are authoritative, so a mismatch is about trust in the row as a whole rather
// than about which value to believe.
func CrossCheck(h Hash, kind Kind, version uint8, flags Flags, fileIndex uint32) error {
	parsed, err := Parse(h[:])
	if err != nil {
		return err
	}

	if version != parsed.Version {
		return fmt.Errorf("%w: tag says %d, hash says %d", ErrVersion, version, parsed.Version)
	}
	if kind != parsed.Kind {
		return fmt.Errorf("%w: tag says %s, hash says %s", ErrKind, kind, parsed.Kind)
	}
	if flags&flagMask != parsed.Flags {
		return fmt.Errorf("metahash: flags disagree: tag says %s, hash says %s", flags, parsed.Flags)
	}

	// The hash carries the low 16 bits, so a wide index agrees when its
	// truncation does — and that is exactly the case where the path is
	// authoritative rather than the index.
	if uint16(fileIndex) != parsed.FileIndex {
		return fmt.Errorf("metahash: file index disagrees: tag says %d, hash says %d", fileIndex, parsed.FileIndex)
	}

	return nil
}

// -- internals ---------------------------------------------------------------

// indexFor narrows a MetaEntry's uint32 index to the hash's uint16 field.
func indexFor(fileIndex uint32, flags Flags) (uint16, error) {
	if fileIndex == FileIndexWholeSet32 {
		return FileIndexWholeSet, nil
	}

	narrowed := uint16(fileIndex)

	// A file at index 65535, or at any index whose low 16 bits are 0xFFFF,
	// would mint the whole-set hash for a single file. Two different rows would
	// then carry the same hash and collapse into one in the client's result
	// grouping. The caller skips such a file instead.
	if narrowed == FileIndexWholeSet {
		return 0, fmt.Errorf("%w: index %d truncates to 0xFFFF", ErrFileIndex, fileIndex)
	}

	// Above 65535 the hash cannot address the file on its own, so the path has
	// to be authoritative (§3.5).
	if fileIndex > uint32(^uint16(0)) && flags&FlagPathAuthoritative == 0 {
		return 0, fmt.Errorf("metahash: file index %d needs FlagPathAuthoritative: the hash holds only its low 16 bits", fileIndex)
	}

	return narrowed, nil
}
