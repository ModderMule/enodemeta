// Package enodemeta is the shared contract between an eNode-go server, the
// crawler daemons that feed its catalogue, and the eMuleQt client that acts on
// the rows it publishes.
//
// The pieces live in subpackages — metahash for the eD2K pseudo-hash, btid for
// transfer ids, torrentmeta and bencode for BitTorrent metadata, tags for the
// protocol numbers — and this file holds the one operation that spans them:
// checking a fetched metafile against the hash a row advertised.
package enodemeta

import (
	"errors"
	"fmt"

	"github.com/ModderMule/torrent-crawler/pkg/enodemeta/metahash"
	"github.com/ModderMule/torrent-crawler/pkg/enodemeta/torrentmeta"
)

// Errors returned when a metafile cannot be checked against a hash.
var (
	// ErrKindUnsupported means this build cannot derive an identity for that
	// kind. Today that is only NZB.
	ErrKindUnsupported = errors.New("enodemeta: no identity function for this kind")

	// ErrVerification means the metafile is not the one the hash described.
	ErrVerification = errors.New("enodemeta: the metafile does not match its meta hash")
)

// VerifyMetaFile checks fetched metafile bytes against the meta hash that
// advertised them (§8.4).
//
//	bytes  := GetMetaFile(metaHash)
//	ident  := kind == nzb ? canonicalNzbDigest(bytes) : infoHashOf(bytes)
//	if fold10(ident) != metaHash[6..15] { reject }
//
// Be clear about what this buys. The same server supplies both the row and the
// metafile, so it cannot catch a server that lies consistently. What it catches
// is a *different* origin — a cache, a CDN, a reverse proxy, or a man in the
// middle on a plaintext fetch — serving something other than what was
// advertised. It also covers the case this architecture creates on purpose: the
// row arrives over eD2K and the bytes over HTTPS from a separately addressed
// listener, so the fold is what binds two halves of a transaction that crossed
// two transports.
//
// The crawler runs the same check on its own fetches, where it catches a peer
// that answered with a torrent other than the one that was asked for.
func VerifyMetaFile(hash metahash.Hash, metafile []byte) error {
	parsed, err := metahash.Parse(hash[:])
	if err != nil {
		return err
	}

	identity, err := IdentityOf(parsed.Kind, metafile)
	if err != nil {
		return err
	}

	if !hash.VerifyIdentity(identity) {
		return fmt.Errorf("%w: %s advertised digest %x, the bytes fold to %x",
			ErrVerification, hash, parsed.Digest, metahash.Fold10(identity))
	}

	return nil
}

// IdentityOf derives the identity a meta hash folds, from the metafile bytes.
//
// For a torrent it accepts either a whole .torrent or the bare info dictionary,
// because both spellings occur: BEP 9 transfers the info dictionary alone,
// while a metafile served over the API is a complete file.
func IdentityOf(kind metahash.Kind, metafile []byte) ([]byte, error) {
	switch kind {
	case metahash.KindBTV1, metahash.KindBTV2:
		info, err := parseTorrentOrInfo(metafile)
		if err != nil {
			return nil, err
		}

		gotKind, identity := info.Identity()
		if gotKind != kind {
			return nil, fmt.Errorf("%w: the row says %s, the metafile is %s", ErrVerification, kind, gotKind)
		}

		return identity, nil

	case metahash.KindNZB:
		// ToDo: implement the canonical NZB digest of §3.4 when the usenet
		// sister daemon lands. It belongs in a nzbmeta subpackage here, not in
		// that repository, because eNode-go has to verify NZB rows too.
		return nil, fmt.Errorf("%w: %s", ErrKindUnsupported, kind)

	default:
		return nil, fmt.Errorf("%w: %s", ErrKindUnsupported, kind)
	}
}

// -- internals ---------------------------------------------------------------

// parseTorrentOrInfo accepts a .torrent or a bare info dictionary.
func parseTorrentOrInfo(metafile []byte) (*torrentmeta.Info, error) {
	if torrent, err := torrentmeta.ParseTorrentFile(metafile); err == nil {
		return torrent.Info, nil
	}

	info, err := torrentmeta.ParseInfo(metafile)
	if err != nil {
		return nil, fmt.Errorf("enodemeta: %w", err)
	}

	return info, nil
}
