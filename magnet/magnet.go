// Package magnet builds BitTorrent magnet links.
//
// A magnet on a meta row is what makes the row useful without an API call at
// all: a client that recognises it hands the link straight to its BitTorrent
// engine, which resolves the metadata from the swarm. That is also why the
// crawler always emits one — a DHT-crawled torrent is trackerless by nature, so
// the magnet is the complete instruction.
//
// # Which hashes go in
//
// A hybrid torrent carries both xt values: btih for v1 and btmh for v2. The
// swarm identity stays the v1 hash (eMuleQt's BitTorrent research §7.1), but a
// v2-aware client that sees both can join the v2 swarm as well, and one that
// does not simply ignores the btmh.
package magnet

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// multihash prefix for a SHA-256 v2 infohash: 0x12 is the sha2-256 code and
// 0x20 is its 32-byte length (BEP 52).
const multihashSHA256Prefix = "1220"

// ErrNoHash is returned when a magnet would have nothing to identify.
var ErrNoHash = errors.New("magnet: a link needs at least one infohash")

// Link describes the magnet to build.
type Link struct {
	// V1 is the 20-byte v1 infohash, if the torrent has one.
	V1 []byte

	// V2 is the 32-byte v2 infohash, if the torrent has one.
	V2 []byte

	// Name is the display name (dn). Optional, and only a hint: a client shows
	// it until the real metadata arrives.
	Name string

	// Size is the exact length (xl), in bytes. Optional; 0 omits it.
	Size uint64

	// Trackers are announce URLs (tr). A DHT-crawled torrent has none.
	Trackers []string

	// SelectOnly lists file indexes to download (so, BEP 53). This is what lets
	// a per-file meta row mean "this file of that release" once it reaches a
	// BitTorrent engine.
	SelectOnly []uint32
}

// Build renders the magnet URI.
func (l Link) Build() (string, error) {
	if len(l.V1) != 20 && len(l.V2) != 32 {
		return "", ErrNoHash
	}

	var b strings.Builder
	b.WriteString("magnet:?")

	first := true
	add := func(key, value string) {
		if !first {
			b.WriteByte('&')
		}
		first = false

		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(value)
	}

	// The v1 hash goes first, so a client reading only the first xt joins the
	// swarm the row was keyed by.
	if len(l.V1) == 20 {
		add("xt", "urn:btih:"+strings.ToUpper(hex.EncodeToString(l.V1)))
	}
	if len(l.V2) == 32 {
		add("xt", "urn:btmh:"+multihashSHA256Prefix+strings.ToLower(hex.EncodeToString(l.V2)))
	}

	if name := strings.TrimSpace(l.Name); name != "" {
		add("dn", url.QueryEscape(name))
	}
	if l.Size > 0 {
		add("xl", strconv.FormatUint(l.Size, 10))
	}

	for _, tracker := range l.Trackers {
		if tracker = strings.TrimSpace(tracker); tracker != "" {
			add("tr", url.QueryEscape(tracker))
		}
	}

	if so := formatSelectOnly(l.SelectOnly); so != "" {
		add("so", so)
	}

	return b.String(), nil
}

// V1Link is the common case: one torrent, its name and size.
func V1Link(infohash [20]byte, name string, size uint64) string {
	link, err := Link{V1: infohash[:], Name: name, Size: size}.Build()
	if err != nil {
		// Unreachable: a [20]byte is always a valid v1 hash.
		return ""
	}

	return link
}

// -- internals ---------------------------------------------------------------

// formatSelectOnly renders BEP 53's index list, collapsing runs into ranges the
// way the specification does: "0-2,5" rather than "0,1,2,5".
func formatSelectOnly(indexes []uint32) string {
	if len(indexes) == 0 {
		return ""
	}

	sorted := make([]uint32, len(indexes))
	copy(sorted, indexes)
	sortUint32(sorted)

	var parts []string
	start, prev := sorted[0], sorted[0]

	flush := func() {
		if start == prev {
			parts = append(parts, strconv.FormatUint(uint64(start), 10))

			return
		}
		parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
	}

	for _, idx := range sorted[1:] {
		switch {
		case idx == prev:
			continue // a duplicate index says nothing new
		case idx == prev+1:
			prev = idx
		default:
			flush()
			start, prev = idx, idx
		}
	}
	flush()

	return strings.Join(parts, ",")
}

// sortUint32 is an insertion sort: the lists here are a handful of file indexes
// at most, so this avoids pulling in a dependency for the sake of a sort.
func sortUint32(s []uint32) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
