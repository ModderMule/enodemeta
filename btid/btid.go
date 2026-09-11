// Package btid formats and parses the cross-network transfer ids eMuleQt's
// BitTorrent research §7.1 defines.
//
//	ed2k:<32 hex>      an eD2K MD4
//	bt:v1:<40 hex>     a v1 or hybrid torrent — the v1 hash stays the swarm identity
//	bt:v2:<64 hex>     a v2-only torrent
//	nzb:<uuid>         a Usenet release, minted when it is added
//
// Two rules travel with the format, and both are here because they were learned
// the hard way:
//
//  1. **Uppercase hex everywhere.** The daemon emits uppercase and hand-rolled
//     GUI hex is lowercase; a mismatch fails a string comparison silently
//     rather than loudly. Formatting and parsing live only in this package so
//     that bug cannot be reintroduced one call site at a time.
//  2. **A hybrid torrent is its v1 hash.** That is what the swarm, the trackers
//     and every magnet link key on. The v2 hash is a detail for the details
//     dialog.
//
// The crawler uses these ids as its catalog_id: the identifier eNode-go echoes
// back when it asks for a metafile.
package btid

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Scheme prefixes.
const (
	PrefixED2K = "ed2k:"
	PrefixBTV1 = "bt:v1:"
	PrefixBTV2 = "bt:v2:"
	PrefixNZB  = "nzb:"
)

// Network is which network an id belongs to.
type Network uint8

// The networks an id can name.
const (
	NetworkUnknown Network = iota
	NetworkED2K
	NetworkBTV1
	NetworkBTV2
	NetworkNZB
)

// String names the network.
func (n Network) String() string {
	switch n {
	case NetworkED2K:
		return "ed2k"
	case NetworkBTV1:
		return "bt:v1"
	case NetworkBTV2:
		return "bt:v2"
	case NetworkNZB:
		return "nzb"
	default:
		return "unknown"
	}
}

// Errors returned by this package.
var (
	ErrFormat = errors.New("btid: not a transfer id")
	ErrLength = errors.New("btid: wrong hash length for the scheme")
)

// ID is a parsed transfer id.
type ID struct {
	Network Network

	// Hash is the raw hash bytes: 16 for ed2k, 20 for bt:v1, 32 for bt:v2.
	// Empty for nzb, whose id is an opaque uuid.
	Hash []byte

	// Value is the part after the prefix, as written.
	Value string
}

// String reassembles the id.
func (id ID) String() string {
	switch id.Network {
	case NetworkED2K:
		return PrefixED2K + upperHex(id.Hash)
	case NetworkBTV1:
		return PrefixBTV1 + upperHex(id.Hash)
	case NetworkBTV2:
		return PrefixBTV2 + upperHex(id.Hash)
	case NetworkNZB:
		return PrefixNZB + id.Value
	default:
		return ""
	}
}

// V1 formats a v1 or hybrid torrent's id.
func V1(infohash [20]byte) string {
	return PrefixBTV1 + upperHex(infohash[:])
}

// V2 formats a v2-only torrent's id.
func V2(infohash [32]byte) string {
	return PrefixBTV2 + upperHex(infohash[:])
}

// NZB formats a Usenet release's id.
func NZB(uuid string) string {
	return PrefixNZB + uuid
}

// Torrent formats whichever id a torrent should carry, following rule 2: a
// torrent with a v1 hash is named by it, hybrid or not.
func Torrent(v1 *[20]byte, v2 *[32]byte) (string, error) {
	switch {
	case v1 != nil:
		return V1(*v1), nil
	case v2 != nil:
		return V2(*v2), nil
	default:
		return "", fmt.Errorf("%w: a torrent needs at least one infohash", ErrFormat)
	}
}

// Parse reads a transfer id.
//
// An id with no colon is an eD2K MD4, which is what keeps the pre-namespace IPC
// calls working: that backwards compatibility is one check, not a layer.
func Parse(s string) (ID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ID{}, fmt.Errorf("%w: empty", ErrFormat)
	}

	switch {
	case strings.HasPrefix(s, PrefixBTV1):
		return parseHashID(NetworkBTV1, s[len(PrefixBTV1):], 20)

	case strings.HasPrefix(s, PrefixBTV2):
		return parseHashID(NetworkBTV2, s[len(PrefixBTV2):], 32)

	case strings.HasPrefix(s, PrefixED2K):
		return parseHashID(NetworkED2K, s[len(PrefixED2K):], 16)

	case strings.HasPrefix(s, PrefixNZB):
		value := s[len(PrefixNZB):]
		if value == "" {
			return ID{}, fmt.Errorf("%w: an nzb id needs a value", ErrFormat)
		}

		return ID{Network: NetworkNZB, Value: value}, nil

	case !strings.Contains(s, ":"):
		// A bare MD4, from before the namespace existed.
		return parseHashID(NetworkED2K, s, 16)

	default:
		return ID{}, fmt.Errorf("%w: unknown scheme in %q", ErrFormat, s)
	}
}

// -- internals ---------------------------------------------------------------

func parseHashID(network Network, value string, want int) (ID, error) {
	raw, err := hex.DecodeString(value)
	if err != nil {
		return ID{}, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	if len(raw) != want {
		return ID{}, fmt.Errorf("%w: %s takes %d bytes, got %d", ErrLength, network, want, len(raw))
	}

	return ID{Network: network, Hash: raw, Value: upperHex(raw)}, nil
}

func upperHex(b []byte) string {
	return strings.ToUpper(hex.EncodeToString(b))
}
