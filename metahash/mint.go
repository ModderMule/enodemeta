package metahash

import "fmt"

// MintInput is everything hash construction needs to know about one advertised
// row.
//
// It is a plain struct rather than the catalogue's own entry type so that hash
// construction stays in one place with no dependencies: eNode-go mints from a
// MetaEntry it received, this crawler mints from its own rows in tests, and
// both go through the same code (§6.5).
type MintInput struct {
	// Kind and Identity describe the release. Identity is the whole release's
	// identity, never the selected file's (§3.4).
	Kind     Kind
	Identity []byte

	// FileIndex is the selected file's ordinal, or FileIndexWholeSet32 for the
	// row that stands for the whole release.
	FileIndex uint32

	// FileCount is how many selectable, non-padding files the release holds.
	//
	// It comes from the metafile rather than from how many rows were emitted:
	// a release advertising four of its five hundred files is still multi-file,
	// and the flag must not change when the selection does, because that would
	// change the hash of rows that are otherwise the same.
	FileCount uint32

	// PathAuthoritative forces the flag on. It is implied for an NZB, whose
	// file order is not fixed, and for any index past 65535 (§3.5).
	PathAuthoritative bool

	// Protected marks a password-protected or obfuscated release.
	Protected bool
}

// Mint derives the kind, flags and index of a row and builds its hash.
//
// This is the entry point a server should call: Build takes the fields already
// decided, while Mint is what decides them, so the rules in §3.5 live in one
// place instead of at every call site.
func Mint(in MintInput) (Hash, error) {
	var h Hash

	if !in.Kind.Valid() {
		return h, fmt.Errorf("%w: %d", ErrKind, uint8(in.Kind))
	}

	// Only the uint32 marker means "the whole release". A bare 0xFFFF is file
	// number 65535, which Build refuses because its hash would be
	// indistinguishable from the whole-set row's — the caller skips that file
	// instead of renumbering it.
	wholeSet := in.FileIndex == FileIndexWholeSet32

	// A whole-set row for a release of one file says nothing a plain row does
	// not, and it would mint a second hash for the same thing.
	if wholeSet && in.FileCount <= 1 {
		return h, fmt.Errorf("metahash: a release of %d file(s) has no whole-set row", in.FileCount)
	}

	var flags Flags
	if in.FileCount > 1 {
		flags |= FlagMultiFile
	}
	if in.Protected {
		flags |= FlagProtected
	}

	// The flag is about whether the *index* can be trusted to find the file, so
	// the whole-set row — which selects no file — never needs it.
	tooWide := !wholeSet && in.FileIndex > uint32(^uint16(0))
	if in.PathAuthoritative || in.Kind == KindNZB || tooWide {
		flags |= FlagPathAuthoritative
	}

	return Build(in.Kind, flags, in.FileIndex, in.Identity)
}
