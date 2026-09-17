// Package magnet builds and parses BitTorrent magnet links.
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
	"encoding/base32"
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

// maxSelectOnly bounds how many file indexes Parse expands an so list into. A
// range is a few characters of text, and "0-4294967295" would otherwise be a
// sixteen-gigabyte allocation on the word of whoever wrote the link.
const maxSelectOnly = 1 << 20

// Errors Build and Parse return.
var (
	// ErrNoHash is returned when a magnet would have nothing to identify.
	ErrNoHash = errors.New("magnet: a link needs at least one infohash")

	// ErrNotMagnet is text that is not a magnet URI at all.
	ErrNotMagnet = errors.New("magnet: not a magnet link")
)

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

// Parse reads a magnet URI back into a Link, the inverse of Build.
//
// It is lenient about everything that does not identify the torrent, because
// the links it reads were written by every client and index there is: an
// unknown parameter is ignored, a value that does not unescape is taken as it
// stands, and a malformed xl or so is dropped rather than failing the link. The
// hash is the one thing it is strict about. A btih is 40 hex or 32 base32
// characters and a btmh is a sha2-256 multihash; anything else is not a hash,
// and a link with no hash is ErrNoHash.
//
// The first valid hash of each version wins, and the indexed form the magnet
// scheme allows for repeating a key (xt.1, xt.2) is read the same as the plain
// one.
func Parse(uri string) (Link, error) {
	var link Link

	text := strings.TrimSpace(uri)
	if len(text) < len("magnet:?") || !strings.EqualFold(text[:len("magnet:?")], "magnet:?") {
		return link, ErrNotMagnet
	}

	for _, param := range strings.Split(text[len("magnet:?"):], "&") {
		key, value, _ := strings.Cut(param, "=")
		key, _, _ = strings.Cut(strings.ToLower(key), ".")

		if unescaped, err := url.QueryUnescape(value); err == nil {
			value = unescaped
		}
		value = strings.TrimSpace(value)

		switch key {
		case "xt":
			parseExactTopic(&link, value)

		case "dn":
			if link.Name == "" {
				link.Name = value
			}

		case "xl":
			if size, err := strconv.ParseUint(value, 10, 64); err == nil {
				link.Size = size
			}

		case "tr":
			if value != "" {
				link.Trackers = append(link.Trackers, value)
			}

		case "so":
			link.SelectOnly = parseSelectOnly(value)
		}
	}

	if link.V1 == nil && link.V2 == nil {
		return link, fmt.Errorf("%w: %s", ErrNoHash, text)
	}

	return link, nil
}

// -- internals ---------------------------------------------------------------

// parseExactTopic fills in the hash an xt value names, if it names one this
// package understands and the link does not already have one of that version.
func parseExactTopic(link *Link, value string) {
	lower := strings.ToLower(value)

	switch {
	case strings.HasPrefix(lower, "urn:btih:") && link.V1 == nil:
		link.V1 = parseBTIH(value[len("urn:btih:"):])

	case strings.HasPrefix(lower, "urn:btmh:") && link.V2 == nil:
		digest := value[len("urn:btmh:"):]
		if len(digest) != len(multihashSHA256Prefix)+64 || !strings.EqualFold(digest[:4], multihashSHA256Prefix) {
			return
		}
		if raw, err := hex.DecodeString(digest[4:]); err == nil {
			link.V2 = raw
		}
	}
}

// parseBTIH decodes a v1 infohash in either of the encodings magnets use: 40
// hex characters, or the 32 base32 characters older clients wrote.
func parseBTIH(text string) []byte {
	switch len(text) {
	case 40:
		if raw, err := hex.DecodeString(text); err == nil {
			return raw
		}

	case 32:
		if raw, err := base32.StdEncoding.DecodeString(strings.ToUpper(text)); err == nil && len(raw) == 20 {
			return raw
		}
	}

	return nil
}

// parseSelectOnly expands BEP 53's index list, "0-2,5", into the indexes it
// names. A list that does not parse, or that names more than maxSelectOnly
// indexes, is dropped whole: half a selection selects the wrong files.
func parseSelectOnly(text string) []uint32 {
	var out []uint32

	for _, part := range strings.Split(text, ",") {
		first, last, isRange := strings.Cut(strings.TrimSpace(part), "-")

		start, err := strconv.ParseUint(first, 10, 32)
		if err != nil {
			return nil
		}
		end := start
		if isRange {
			if end, err = strconv.ParseUint(last, 10, 32); err != nil || end < start {
				return nil
			}
		}

		if uint64(len(out))+end-start+1 > maxSelectOnly {
			return nil
		}
		for idx := start; idx <= end; idx++ {
			out = append(out, uint32(idx))
		}
	}

	return out
}

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
