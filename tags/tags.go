// Package tags holds the eD2K protocol numbers the meta-search feature adds.
//
// They live here, in the shared module, because three codebases have to agree
// on them and none of the three owns them: eNode-go writes these tags, eMuleQt
// reads them, and the crawler daemons decide what goes in them. The values are
// specified in eNode-go's docs/meta-search-torrent-usenet-plan.local.md §4,
// §5.1 and §6.4.
//
// # Why these numbers
//
// The eD2K tag name-id space is a single byte and roughly two thirds allocated.
// The range 0x60-0x6F was verified clean across every surveyed tree — eMule's
// srchybrid, eMuleQt and eNode-go — which is why the meta tags sit there. The
// server-tag numbers were picked the same way: srchybrid stops at 0x98,
// eMuleQt adds 0x99, and eNode-go's own ST_NAT_PORT is 0x9D.
//
// This package deliberately does not restate the classic tags (FT_FILENAME,
// FT_FILESIZE and the rest). Those belong to each implementation's existing
// protocol constants, and copying them here would create two places to change.
package tags

// The meta tags carried on a search row (§4). Every one of these is a numeric
// name id, because eNode-go's tag encoder writes only those.
const (
	// FTMetaKind is 1 = bt-v1 or hybrid, 2 = bt-v2, 3 = nzb.
	//
	// This tag, not the hash's kind nibble, is authoritative. A real MD4 looks
	// like a meta hash about once in a million files, so a client that switched
	// behaviour on the hash shape alone would eventually mis-handle a genuine
	// file.
	FTMetaKind = 0x60

	// FTMetaVersion is the scheme version. A client must ignore a row whose
	// version it does not know — that rule is what makes a version 2
	// deployable.
	FTMetaVersion = 0x61

	// FTMetaFileIndex is the full-width ordinal, for the releases whose index
	// does not fit the hash's 16 bits.
	FTMetaFileIndex = 0x62

	// FTMetaFilePath is the path inside the torrent, or an NZB's subject. It is
	// authoritative over the index when hash flag 0x2 is set.
	FTMetaFilePath = 0x63

	// FTMetaTotalSize is the whole release's size; FT_FILESIZE stays the
	// selected file's.
	FTMetaTotalSize = 0x64

	// FTMetaID is the opaque catalogue id, echoed back when the client asks for
	// the metafile. It lets the server look a release up without trusting the
	// hash.
	FTMetaID = 0x65

	// FTMetaReserved held a direct metafile URL in an earlier draft. It is
	// permanently unused: a server-supplied per-row URL would turn every client
	// into an SSRF probe, and the client now derives the fetch from the API
	// base it already trusts.
	FTMetaReserved = 0x66

	FTMetaSeeders = 0x67
	FTMetaPeers   = 0x68

	// FTMetaAge is days since posting for Usenet, or the torrent's age.
	FTMetaAge = 0x69

	// FTMetaIndexer is which catalogue produced the row: the "who says so"
	// column.
	FTMetaIndexer = 0x6A

	// FTMetaFlags is the bitfield below.
	FTMetaFlags = 0x6B

	// FTMetaMagnet is a BitTorrent magnet URI. A row carrying one needs no API
	// call at all: the client can hand it straight to its engine.
	FTMetaMagnet = 0x6C
)

// MetaTagRangeStart and MetaTagRangeEnd bound the range reserved for this
// feature. A test asserts that nothing this project emits falls outside it.
const (
	MetaTagRangeStart = 0x60
	MetaTagRangeEnd   = 0x6F
)

// The FT_META_FLAGS bits (§4).
const (
	// MetaFlagPasswordProtected marks a release behind a password.
	MetaFlagPasswordProtected = 1 << 0

	// MetaFlagNeedsPAR2 marks a Usenet release that needs repair blocks.
	MetaFlagNeedsPAR2 = 1 << 1

	// MetaFlagPrivateTracker marks a torrent whose swarm is tracker-gated. Such
	// a torrent does not belong on the DHT and a public client cannot join it.
	MetaFlagPrivateTracker = 1 << 2

	// MetaFlagMagnetOnly marks a release with no fetchable metafile, where the
	// magnet is all there is. Verification then degenerates to "the infohash
	// matches", which is what BitTorrent's own trust model already provides.
	MetaFlagMagnetOnly = 1 << 3

	// MetaFlagV2Available marks a hybrid torrent: the row is keyed by v1, and a
	// v2 hash also exists.
	MetaFlagV2Available = 1 << 4
)

// The server tags in OP_SERVERIDENT that tell a client where the metadata API
// is (§6.4).
const (
	// STMetaAPIFingerprint is "sha256/<base64>" of the API certificate's
	// SubjectPublicKeyInfo, so a server on a bare IP can be pinned without a
	// certificate authority. It is a string tag rather than a hash tag because
	// the eD2K hash tag type is exactly 16 bytes and a SHA-256 fingerprint is
	// 32.
	STMetaAPIFingerprint = 0x9C

	// STMetaAPI is the API's base URL.
	STMetaAPI = 0x9E

	// STMetaAPIVersion is the highest contract major version served.
	STMetaAPIVersion = 0x9F
)

// Capability bits (§5.1, §5.2).
const (
	// SrvCapMetaSearch is the client's bit in the CT_SERVER_FLAGS login tag,
	// saying it can act on a meta row. 0x1000 is already eMuleQt's IPv6 bit.
	SrvCapMetaSearch = 0x2000

	// FlagMetaSearch is the server's bit in its own flags word, saying it
	// serves a catalogue. eNode-go's own flags stop at 0x8000.
	FlagMetaSearch = 0x10000

	// SrvCapUDPMetaSearch is the opt-in inside OP_GLOBSEARCHREQ3's client tag
	// block. The older global-search opcodes have no tag block and therefore
	// never receive meta rows, which is what keeps the amplification surface
	// restricted to clients that asked.
	SrvCapUDPMetaSearch = 0x02
)

// MaxSources caps the source count a meta row may advertise (§4.2).
//
// eMule's spam heuristic fires on more than 100 sources when no non-spam server
// answered, so staying at or below 99 keeps a catalogue row out of it. A meta
// row has no source addresses at all, so the heuristic's other branch — a
// source sharing a /24 with the answering server — cannot fire either.
const MaxSources = 99

// LegacyNameSuffix is the default marker appended to the filename of a row
// broadcast to clients that cannot act on it (§4.3).
//
// The filename is the only field either client tree renders from a search
// result: neither parses a comment or a description tag. So this is the only
// place a warning can go.
const LegacyNameSuffix = " [{kind}]"
