package weblinks

import (
	"crypto/sha256"
	"sort"
	"strings"
)

// IdentityPrefix is the domain separator every pre-image starts with, so that a
// digest is never a bare SHA-256 of something else and a version 2 of these
// rules cannot collide with this one.
const IdentityPrefix = "web1\n"

// IdentityLen is the digest's length in bytes, and the length metahash.KindWeb
// expects.
const IdentityLen = sha256.Size

// Identity is the canonical link-set digest.
//
// The pre-image is built like this, and the rules below are the whole
// specification:
//
//		identity_bytes := "web1\n"
//		for each distinct canonical link, sorted ascending by its bytes:
//		    identity_bytes += link + "\n"
//		digest := SHA-256(identity_bytes)
//
//	 1. A container is split at '\n'. Spaces, tabs and carriage returns are
//	    trimmed from both ends of a line, so CRLF and LF read the same.
//	 2. An empty line and a line starting with '#' emit nothing. Hints are
//	    therefore outside the identity: a size learned later, or a title
//	    corrected, leaves the release the same release.
//	 3. The scheme is everything before the first "://", lowered. It must be
//	    http or https.
//	 4. The fragment, everything from the first '#', is part of the link and
//	    is kept byte for byte: some hosters keep a file's decryption key
//	    there, and a link without it opens nothing. Only an empty fragment, a
//	    '#' that is the link's last byte, is removed. A producer removes a
//	    fragment that does not matter before it builds the container; this
//	    package cannot know which those are.
//	 5. The authority runs from the "://" to the first '/', '?' or '#'. It is
//	    lowered. One with an '@' is refused: a link with credentials in it is
//	    not one to publish.
//	 6. A port of 80 on http, of 443 on https, or an empty one, is removed. The
//	    colons of an IPv6 literal are not a port.
//	 7. When nothing follows the authority, or a '?' or a '#' does, a '/' is
//	    put between.
//	 8. The rest is kept byte for byte. Nothing is percent-decoded, no case
//	    changes and no query is reordered: hosters compare those bytes, and a
//	    rule that was almost right would merge links that are different files.
//	 9. A link holding a byte at or below 0x20, or 0x7F, is refused.
//	 10. Lowering changes A to Z only, and "ascending" compares bytes.
//	 11. The same link twice counts once.
//	 12. A container that emits no link has no identity and is refused with
//	    ErrNoLinks, as is one with a line that is neither comment nor link.
//	 13. The digest covers the whole set, never one file of it, which is what
//	    lets a client group the rows of one release by bytes 6..15 of their
//	    meta hashes.
//
// Rule 2 has a consequence worth stating. A mirror added or lost changes the
// set, so it changes the identity: the daemon retracts the old release and
// publishes the new one. That is the price of a metafile a client can verify.
func Identity(d *Document) ([IdentityLen]byte, error) {
	preimage, err := IdentityPreimage(d)
	if err != nil {
		var zero [IdentityLen]byte

		return zero, err
	}

	return sha256.Sum256(preimage), nil
}

// IdentityOf parses a container and returns its identity. It is what
// enodemeta.IdentityOf calls for metahash.KindWeb, and therefore what
// VerifyMetaFile checks a served container with.
func IdentityOf(raw []byte) ([IdentityLen]byte, error) {
	doc, err := Parse(raw)
	if err != nil {
		var zero [IdentityLen]byte

		return zero, err
	}

	return Identity(doc)
}

// IdentityPreimage returns the exact bytes the digest is taken over. It is
// exported for the same reason nzbmeta's is: a digest mismatch says nothing and
// the byte a pre-image differs at says everything.
//
// It canonicalises each link again rather than trusting Link.URL, so a document
// a daemon built by hand has the identity the same links would have once
// written and read back. Canonical is idempotent, which is what makes that safe.
func IdentityPreimage(d *Document) ([]byte, error) {
	links := make([]string, 0, len(d.Links))
	seen := make(map[string]struct{}, len(d.Links))

	for _, link := range d.Links {
		canonical, err := Canonical(link.URL)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[canonical]; dup {
			continue
		}

		seen[canonical] = struct{}{}
		links = append(links, canonical)
	}

	if len(links) == 0 {
		return nil, ErrNoLinks
	}

	sort.Strings(links)

	var sb strings.Builder
	sb.WriteString(IdentityPrefix)

	for _, link := range links {
		sb.WriteString(link)
		sb.WriteByte('\n')
	}

	return []byte(sb.String()), nil
}
