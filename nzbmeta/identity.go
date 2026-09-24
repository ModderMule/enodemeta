package nzbmeta

import (
	"crypto/sha256"
	"sort"
	"strconv"
	"strings"

	"github.com/ModderMule/enodemeta/metahash"
)

// IdentityPrefix is the domain separator every pre-image starts with.
//
// It is there so that a digest can never be confused with a bare SHA-256 of
// something else, and so that a version 2 of these rules can be introduced
// without the two colliding.
const IdentityPrefix = "nzb1\n"

// IdentityLen is the digest's length in bytes, and the length metahash.KindNZB
// expects.
const IdentityLen = sha256.Size

// Identity is the canonical NZB digest (§3.4).
//
// The pre-image is built like this, and the eleven rules below are the whole
// specification:
//
//		identity_bytes := "nzb1\n"
//		for each <file> in document order:
//		    for each <segment> sorted ascending by @number (stable):
//		        identity_bytes += decimal(@number) + ":" + <segment text> + "\n"
//		digest := SHA-256(identity_bytes)
//
//	 1. Files are taken in document order. Our own writer puts them in
//	    CanonicalOrder first, so a release assembled twice from the same articles
//	    in any arrival order mints the same id; an imported third-party document
//	    is never reordered.
//	 2. Segments are sorted ascending by number, and the sort is stable, so two
//	    segments claiming the same number keep their document order.
//	 3. decimal is canonical base-10: "007" becomes "7".
//	 4. A number that is absent, unparseable or negative is 0. It still emits a
//	    line — the article exists whatever the document says about its position.
//	 5. Segment text has XML entities resolved, is whitespace-trimmed, and then
//	    has one leading '<' and one trailing '>' removed independently. That is
//	    NormalizeMessageID, and it matches eMuleQt's normalizeMessageId byte for
//	    byte.
//	 6. A segment whose message-id is empty emits no line. This is the
//	    load-bearing rule: it makes a <file> with no usable segment contribute
//	    nothing, so a reader that drops such a file and one that keeps it agree —
//	    which removes the only real disagreement two conforming parsers had.
//	 7. The separator and the terminator are a literal ':' and '\n'. No other
//	    whitespace is written.
//	 8. <head>, every <meta>, <groups>, @poster, @date, @subject and @bytes are
//	    excluded. A repost of the same articles to another group under another
//	    subject is deliberately the same identity.
//	 9. The bytes are UTF-8, and only '\n' is a line ending.
//	 10. A document that emits zero lines has no identity and is refused with
//	    ErrNoSegments — otherwise every HTML error page in the world would share
//	    one.
//	 11. The digest covers the whole release, never a selected file, which is what
//	    lets a client group every row of one release by comparing bytes 6..15 of
//	    their meta hashes before fetching anything.
//
// Note what rule 5 implies about where normalisation happens: exactly once, at
// the boundary. Identity uses Segment.MessageID verbatim, because
// NormalizeMessageID is not idempotent — "<<id>>" normalises to "<id>" and then
// to "id" — so applying it twice would make a re-read disagree with the read.
func Identity(d *Document) ([IdentityLen]byte, error) {
	preimage, err := IdentityPreimage(d)
	if err != nil {
		var zero [IdentityLen]byte

		return zero, err
	}

	return sha256.Sum256(preimage), nil
}

// IdentityOf parses a document and returns its identity.
//
// This is what enodemeta.IdentityOf calls for metahash.KindNZB, and therefore
// what VerifyMetaFile checks a served metafile with.
func IdentityOf(raw []byte) ([IdentityLen]byte, error) {
	doc, err := Parse(raw)
	if err != nil {
		var zero [IdentityLen]byte

		return zero, err
	}

	return Identity(doc)
}

// IdentityPreimage returns the exact bytes the digest is taken over.
//
// It is exported because a port that gets the digest wrong learns nothing from a
// digest mismatch and everything from the byte the pre-image differs at. The
// vectors carry it for the same reason.
func IdentityPreimage(d *Document) ([]byte, error) {
	var sb strings.Builder
	sb.WriteString(IdentityPrefix)

	lines := 0

	for i := range d.Files {
		for _, s := range sortedSegments(d.Files[i].Segments) {
			// Rule 6. An empty id is the whole reason two readers can disagree
			// about whether a file exists and still agree on the identity.
			if s.MessageID == "" {
				continue
			}

			sb.WriteString(strconv.Itoa(max(s.Number, 0)))
			sb.WriteByte(':')
			sb.WriteString(s.MessageID)
			sb.WriteByte('\n')
			lines++
		}
	}

	if lines == 0 {
		return nil, ErrNoSegments
	}

	return []byte(sb.String()), nil
}

// NormalizeMessageID trims a message-id and removes one leading '<' and one
// trailing '>', independently.
//
// The brackets are the wire form: NNTP's ARTICLE takes "<id>", while an NZB may
// or may not carry them, and an id stored with them would produce "<<id>>" and a
// silent 430. eMuleQt's normalizeMessageId is this function, and the two must
// stay identical because they are both inputs to the same digest.
//
// It is deliberately *not* idempotent, and the two brackets are removed
// independently rather than as a pair, because that is what the other
// implementation does. Everything that reads a document therefore applies it
// exactly once, at the boundary — see Identity's note.
func NormalizeMessageID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "<")
	id = strings.TrimSuffix(id, ">")

	return id
}

// CatalogID is the digest's canonical hex spelling, without the "nzb:" prefix
// that btid adds.
func CatalogID(digest [IdentityLen]byte) string {
	const hexDigits = "0123456789ABCDEF"

	out := make([]byte, 0, 2*IdentityLen)
	for _, b := range digest {
		out = append(out, hexDigits[b>>4], hexDigits[b&0x0F])
	}

	return string(out)
}

// MintInput describes a document to metahash, so the rules for an NZB's flags
// live in one place rather than at every call site.
//
// fileIndex is the selected file's Index, or metahash.FileIndexWholeSet32 for
// the row that stands for the whole release. Mint sets FlagPathAuthoritative for
// every NZB by itself — an NZB's file order is not fixed across generators — so
// this does not have to.
func (d *Document) MintInput(fileIndex uint32) (metahash.MintInput, error) {
	identity, err := Identity(d)
	if err != nil {
		return metahash.MintInput{}, err
	}

	return metahash.MintInput{
		Kind:      metahash.KindNZB,
		Identity:  identity[:],
		FileIndex: fileIndex,
		FileCount: uint32(len(d.Files)),
		Protected: d.Protected(),
	}, nil
}

// -- internals ---------------------------------------------------------------

// sortedSegments returns the segments ordered ascending by number, stably.
//
// A copy is made rather than sorting in place: the caller's document is an
// input, and reordering it would make a second Identity call on the same
// document depend on the first.
func sortedSegments(in []Segment) []Segment {
	out := make([]Segment, len(in))
	copy(out, in)

	sort.SliceStable(out, func(i, j int) bool {
		return max(out[i].Number, 0) < max(out[j].Number, 0)
	})

	return out
}
