// Package model holds the contract's domain types.
//
// They exist because of one rule from the specification (§6.3): the service is
// defined in Go types, and the .proto is a representation of it rather than the
// other way round. A generated protobuf struct must not leak past the transport
// layer — the moment a *metav1.MetaEntry appears in a catalogue or a storage
// engine, adding a second representation stops being an adapter and becomes a
// rewrite.
//
// So nothing here imports the generated code. Conversion lives in pbconv, which
// imports both.
package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ModderMule/enodemeta/metahash"
)

// Errors returned by Validate.
var (
	ErrInvalidEntry = errors.New("model: invalid entry")
)

// Entry is one advertised row: a release, or one file inside it.
type Entry struct {
	// MetaHash is the 16-byte pseudo-hash. A daemon leaves it empty; the server
	// mints it with metahash.Mint.
	MetaHash []byte

	Kind metahash.Kind

	// FileIndex is the libtorrent file_index_t, or
	// metahash.FileIndexWholeSet32 for the whole-release row.
	FileIndex uint32
	FilePath  string

	Name      string
	Size      uint64
	TotalSize uint64
	Type      string

	Seeders uint32
	Peers   uint32
	AgeDays uint32

	Indexer   string
	CatalogID string
	Magnet    string
	Flags     uint32

	// Identity is the pre-fold digest: the infohash, or an NZB's canonical
	// digest.
	Identity []byte

	// FileCount is how many selectable files the release holds, from the
	// metafile rather than from how many rows were produced.
	FileCount uint32

	PathAuthoritative bool
}

// WholeSet reports whether this row stands for the whole release.
func (e Entry) WholeSet() bool {
	return e.FileIndex == metahash.FileIndexWholeSet32
}

// MintInput is what a server needs to build this row's hash.
func (e Entry) MintInput() metahash.MintInput {
	return metahash.MintInput{
		Kind:              e.Kind,
		Identity:          e.Identity,
		FileIndex:         e.FileIndex,
		FileCount:         e.FileCount,
		PathAuthoritative: e.PathAuthoritative,
		Protected:         e.Flags&FlagPasswordProtected != 0,
	}
}

// Mint fills MetaHash. It is the server's half of the contract, kept here so
// there is exactly one way to do it.
func (e *Entry) Mint() error {
	hash, err := metahash.Mint(e.MintInput())
	if err != nil {
		return err
	}
	e.MetaHash = hash.Bytes()

	return nil
}

// FT_META_FLAGS bits, mirrored from the tags package so a caller working with
// entries does not need both imports.
const (
	FlagPasswordProtected = 1 << 0
	FlagNeedsPAR2         = 1 << 1
	FlagPrivateTracker    = 1 << 2
	FlagMagnetOnly        = 1 << 3
	FlagV2Available       = 1 << 4
)

// Validate checks what a receiving server would otherwise have to check itself.
//
// A daemon runs it before publishing, so a malformed row is a bug caught in the
// process that produced it rather than a stream rejected halfway.
func (e Entry) Validate() error {
	if !e.Kind.Valid() {
		return fmt.Errorf("%w: kind %d", ErrInvalidEntry, uint8(e.Kind))
	}
	if want := e.Kind.IdentityLen(); len(e.Identity) != want {
		return fmt.Errorf("%w: %s takes a %d-byte identity, got %d", ErrInvalidEntry, e.Kind, want, len(e.Identity))
	}
	if strings.TrimSpace(e.CatalogID) == "" {
		return fmt.Errorf("%w: no catalog id", ErrInvalidEntry)
	}
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("%w: no name", ErrInvalidEntry)
	}
	if e.TotalSize > 0 && e.Size > e.TotalSize {
		return fmt.Errorf("%w: the selected file is %d bytes of a %d-byte release", ErrInvalidEntry, e.Size, e.TotalSize)
	}

	// Minting is where the index rules are enforced, so running it here catches
	// an unmintable row at the source.
	if _, err := metahash.Mint(e.MintInput()); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEntry, err)
	}

	return nil
}

// MetaFile is the bytes behind a release.
type MetaFile struct {
	Kind        metahash.Kind
	Content     []byte
	ContentType string
}

// ChangeOp is what happened to a release.
type ChangeOp uint8

// The operations a change can carry.
const (
	OpUnspecified ChangeOp = iota
	OpUpsert
	OpRetract
)

// String names the operation.
func (o ChangeOp) String() string {
	switch o {
	case OpUpsert:
		return "upsert"
	case OpRetract:
		return "retract"
	default:
		return "unspecified"
	}
}

// RetractReason says why a release left the feed.
type RetractReason uint8

// The reasons a release is retracted.
const (
	ReasonUnspecified RetractReason = iota

	// ReasonExpired: the network stopped mentioning it.
	ReasonExpired

	// ReasonBlocked: an operator filter now excludes it.
	ReasonBlocked

	// ReasonBelowThreshold: it fell out of the capped published set. It is
	// still catalogued and still reachable through Search.
	ReasonBelowThreshold
)

// String names the reason.
func (r RetractReason) String() string {
	switch r {
	case ReasonExpired:
		return "expired"
	case ReasonBlocked:
		return "blocked"
	case ReasonBelowThreshold:
		return "below-threshold"
	default:
		return "unspecified"
	}
}

// ReleaseChange is one release's change, which is the unit the feed carries:
// all the rows of a release move together, so a consumer never holds half of
// one.
type ReleaseChange struct {
	Seq       uint64
	CatalogID string
	Op        ChangeOp
	Entries   []Entry
	Reason    RetractReason
}

// SearchQuery asks for rows outside the published set.
type SearchQuery struct {
	Query   string
	Exclude []string
	Kinds   []metahash.Kind

	MinSize    uint64
	MaxSize    uint64
	MinSeeders uint32
	MaxAgeDays uint32
	Type       string

	Limit uint32
}

// SearchResult is what a search returned.
type SearchResult struct {
	Entries []Entry

	// Total is how many rows matched before the limit, when the engine can say
	// cheaply. Zero means "not counted", not "none".
	Total uint64
}

// DaemonInfo describes a catalogue daemon and where its feed stands.
type DaemonInfo struct {
	Daemon          string
	Version         string
	ContractVersion uint32
	Kinds           []metahash.Kind

	// LastSeq is the newest change; PurgedThroughSeq is the oldest still
	// resumable.
	LastSeq          uint64
	PurgedThroughSeq uint64

	Published  uint64
	Catalogued uint64

	Indexer         string
	SearchAvailable bool
}
