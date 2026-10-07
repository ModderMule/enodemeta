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
//
// A native row (metahash.Kind.Native) is not minted: its hash slot takes the
// file's own hash, which is the identity unchanged.
func (e *Entry) Mint() error {
	if e.Kind.Native() {
		if err := e.validateNative(); err != nil {
			return err
		}
		e.MetaHash = append([]byte(nil), e.Identity...)

		return nil
	}

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

	// A Usenet release has no magnet, and nothing but these two checks says so.
	// They only bite once a second kind exists: a daemon that copied the torrent
	// crawler's row builder would carry a magnet field over, and a client that
	// saw FlagMagnetOnly on an NZB row would offer a download that cannot start,
	// because the only way to fetch the articles is the metafile itself.
	if e.Kind == metahash.KindNZB && e.Magnet != "" {
		return fmt.Errorf("%w: an nzb row cannot carry a magnet", ErrInvalidEntry)
	}
	if e.Kind == metahash.KindNZB && e.Flags&FlagMagnetOnly != 0 {
		return fmt.Errorf("%w: an nzb row cannot be magnet-only", ErrInvalidEntry)
	}

	// A native row has no hash to mint, and its own rules instead.
	if e.Kind.Native() {
		return e.validateNative()
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

	// Limit and Offset count releases, not rows: a multi-file release is
	// several rows.
	Limit  uint32
	Offset uint32

	// Sort orders the results; Ascending reverses an attribute sort. A daemon
	// whose network lacks the sort's key answers in relevance order.
	Sort      SearchSort
	Ascending bool

	// Usenet only. MinCompletion is in hundredths of a percent.
	Categories        []uint16
	Groups            []string
	MinCompletion     uint16
	MinGrabs          uint32
	IndexedWithinDays uint32

	// Torrent only.
	Alive          bool
	MinLeechers    uint32
	MinPopularity  uint64
	SeenWithinDays uint32

	// Both. Zero is no bound.
	MinFiles uint32
	MaxFiles uint32
}

// SearchSort is a result order. The values are the wire enum's, so converting
// is a cast.
type SearchSort uint8

// The orders a search can ask for.
const (
	SortUnspecified SearchSort = iota
	SortRelevance
	SortBest
	SortDate
	SortSize
	SortFiles
	SortSeeders
	SortLeechers
	SortPopularity
	SortLastSeen
	SortGrabs
	SortCompletion
	SortIndexed

	sortCount
)

// sortNames are the stable spellings a web form or a CLI flag uses.
var sortNames = [sortCount]string{
	SortUnspecified: "",
	SortRelevance:   "relevance",
	SortBest:        "best",
	SortDate:        "date",
	SortSize:        "size",
	SortFiles:       "files",
	SortSeeders:     "seeders",
	SortLeechers:    "leechers",
	SortPopularity:  "popularity",
	SortLastSeen:    "seen",
	SortGrabs:       "grabs",
	SortCompletion:  "completion",
	SortIndexed:     "indexed",
}

// String is the sort's stable name, "" for SortUnspecified.
func (s SearchSort) String() string {
	if s >= sortCount {
		return ""
	}

	return sortNames[s]
}

// Valid reports whether s is a known sort.
func (s SearchSort) Valid() bool {
	return s < sortCount
}

// Ranked reports whether the sort is a relevance order, which has no direction.
func (s SearchSort) Ranked() bool {
	return s == SortUnspecified || s == SortRelevance || s == SortBest
}

// ParseSort reads a sort by its stable name, ignoring case. An unknown name is
// SortUnspecified and false.
func ParseSort(name string) (SearchSort, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return SortUnspecified, true
	}

	for s, n := range sortNames {
		if n == name {
			return SearchSort(s), true
		}
	}

	return SortUnspecified, false
}

// SearchResult is what a search returned.
type SearchResult struct {
	Entries []Entry

	// Total is how many releases matched before the limit, when the engine can
	// say cheaply. Zero means "not counted", not "none".
	Total uint64

	// NextOffset is where the next page starts. Zero means there is none.
	NextOffset uint32

	// TotalExact is true when Total counts every match, false when it is a lower
	// bound or was not counted.
	TotalExact bool

	// Window is how many releases paging reaches. Zero means not stated.
	Window uint32
}

// Pages says which page a result at offset is and how many pages paging reaches,
// for pages of limit releases. exact is false when pages is an estimate: the
// total was a lower bound and the window did not cap it. pages is zero when the
// result does not say enough to count them — no limit, or no total.
//
// It trusts NextOffset over the arithmetic: a result that has a next page always
// has more pages than the one it is.
func (r SearchResult) Pages(offset, limit uint32) (page, pages uint32, exact bool) {
	if limit == 0 {
		return 0, 0, false
	}
	page = offset/limit + 1

	if r.Total == 0 && r.NextOffset == 0 && len(r.Entries) == 0 && offset == 0 {
		// Nothing matched, or nothing was counted; either way there is no page
		// to number.
		return page, 0, r.TotalExact
	}

	count, exact := r.Total, r.TotalExact
	if r.Window > 0 && count > uint64(r.Window) {
		// Paging stops at the window whatever the total is, so this count is
		// certain even when the total is not.
		count, exact = uint64(r.Window), true
	}

	pages = uint32((count + uint64(limit) - 1) / uint64(limit))
	if pages < page {
		pages, exact = page, false
	}
	if r.NextOffset > 0 && pages <= page {
		pages, exact = page+1, false
	}

	return page, pages, exact
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
	// Files is how many files the catalogued releases hold.
	Files uint64

	Indexer         string
	SearchAvailable bool

	// EnhancedSearchAvailable says SearchEnhanced answers. False, it reports
	// unimplemented.
	EnhancedSearchAvailable bool

	// NetworkUsers estimates the users of the network the daemon crawls from
	// the density of its routing table, and NetworkUsersExperimental from the
	// closest node of each lookup. NetworkFiles estimates the files the whole
	// network holds. Zero is no estimate.
	NetworkUsers             uint64
	NetworkUsersExperimental uint64
	NetworkFiles             uint64

	// NetworkUsersSeen is how many users the daemon has seen over
	// NetworkUsersSeenWindow, and NetworkUsersSeenDay over the last 24 hours:
	// distinct node ids, counted the way NetworkUsers is. The window is in
	// seconds, and zero says the daemon does not count them.
	// NetworkUsersSeenSince is when the count began in Unix seconds, zero when
	// unknown.
	NetworkUsersSeen       uint64
	NetworkUsersSeenDay    uint64
	NetworkUsersSeenWindow uint64
	NetworkUsersSeenSince  uint64
}

// -- internals ---------------------------------------------------------------

// validateNative checks the rules a native row adds: it is one file, and it
// carries nothing that only a metafile-backed release has.
func (e Entry) validateNative() error {
	if want := e.Kind.IdentityLen(); len(e.Identity) != want {
		return fmt.Errorf("%w: %s takes a %d-byte identity, got %d", ErrInvalidEntry, e.Kind, want, len(e.Identity))
	}

	// Roughly one MD4 in a million carries the meta-hash marker by chance
	// (§3.3). Published as a native row it would sit in the hash slot looking
	// like a torrent or an NZB to every capable client, so the daemon skips
	// that file instead.
	if metahash.IsMetaHash(e.Identity) {
		return fmt.Errorf("%w: the %s hash %X reads as a meta hash", ErrInvalidEntry, e.Kind, e.Identity)
	}

	if e.FileIndex != 0 || e.FileCount > 1 {
		return fmt.Errorf("%w: an %s row is a single file, got index %d of %d", ErrInvalidEntry, e.Kind, e.FileIndex, e.FileCount)
	}
	if e.PathAuthoritative {
		return fmt.Errorf("%w: an %s row has no file path to be authoritative", ErrInvalidEntry, e.Kind)
	}
	if e.TotalSize != 0 && e.TotalSize != e.Size {
		return fmt.Errorf("%w: an %s row's total size is its size, got %d and %d", ErrInvalidEntry, e.Kind, e.TotalSize, e.Size)
	}
	if e.Magnet != "" {
		return fmt.Errorf("%w: an %s row cannot carry a magnet", ErrInvalidEntry, e.Kind)
	}
	if e.Flags&(FlagMagnetOnly|FlagPrivateTracker|FlagV2Available|FlagNeedsPAR2) != 0 {
		return fmt.Errorf("%w: an %s row cannot carry torrent or usenet flags (%#x)", ErrInvalidEntry, e.Kind, e.Flags)
	}

	return nil
}
