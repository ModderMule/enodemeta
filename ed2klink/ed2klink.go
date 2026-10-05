// Package ed2klink builds and parses eD2K file links.
//
//	ed2k://|file|<name>|<size>|<32 hex MD4>|/
//	ed2k://|file|<name>|<size>|<32 hex MD4>|h=<32 base32 AICH>|/
//
// It is to a Kad catalogue what magnet is to a torrent one: the complete
// instruction for a client, needing no metafile and no API call. Unlike a
// magnet it is not carried on the row — name, size and hash are already there,
// so a consumer builds the link rather than being sent it.
package ed2klink

import (
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// HashSize is the length of an eD2K file hash (MD4), and AICHSize that of an
// AICH root hash (SHA-1), in bytes.
const (
	HashSize = 16
	AICHSize = 20
)

const (
	scheme     = "ed2k://"
	aichPrefix = "h="
)

// Errors Build and Parse return.
var (
	// ErrNotLink is text that is not an eD2K file link at all.
	ErrNotLink = errors.New("ed2klink: not an ed2k file link")

	// ErrHash is a file hash or an AICH hash of the wrong length or spelling.
	ErrHash = errors.New("ed2klink: bad hash")

	// ErrSize is a size that is not a decimal number.
	ErrSize = errors.New("ed2klink: bad size")

	// ErrNoName is a link with nothing to call the file.
	ErrNoName = errors.New("ed2klink: a link needs a file name")
)

// Link describes one file.
type Link struct {
	// Name is the file name. A link cannot hold a '|' or a '/', so Build
	// escapes them and Parse restores them.
	Name string

	// Size is the file's length in bytes.
	Size uint64

	// Hash is the 16-byte MD4 file hash.
	Hash []byte

	// AICH is the 20-byte AICH root hash. Optional.
	AICH []byte
}

// Build formats the link.
func Build(l Link) (string, error) {
	if strings.TrimSpace(l.Name) == "" {
		return "", ErrNoName
	}
	if len(l.Hash) != HashSize {
		return "", fmt.Errorf("%w: a file hash is %d bytes, got %d", ErrHash, HashSize, len(l.Hash))
	}
	if len(l.AICH) != 0 && len(l.AICH) != AICHSize {
		return "", fmt.Errorf("%w: an AICH hash is %d bytes, got %d", ErrHash, AICHSize, len(l.AICH))
	}

	var b strings.Builder
	b.WriteString(scheme)
	b.WriteString("|file|")
	b.WriteString(url.PathEscape(l.Name))
	b.WriteByte('|')
	b.WriteString(strconv.FormatUint(l.Size, 10))
	b.WriteByte('|')
	b.WriteString(strings.ToUpper(hex.EncodeToString(l.Hash)))
	b.WriteByte('|')
	if len(l.AICH) != 0 {
		b.WriteString(aichPrefix)
		b.WriteString(base32.StdEncoding.EncodeToString(l.AICH))
		b.WriteByte('|')
	}
	b.WriteByte('/')

	return b.String(), nil
}

// Parse reads a file link. Fields it does not know — sources, a part hashset —
// are skipped, since a link written by a newer client must still open.
func Parse(s string) (Link, error) {
	var l Link

	s = strings.TrimSpace(s)
	if len(s) < len(scheme) || !strings.EqualFold(s[:len(scheme)], scheme) {
		return l, ErrNotLink
	}

	fields := strings.Split(s[len(scheme):], "|")

	// "|file|name|size|hash|/" splits into "", file, name, size, hash, "/".
	if len(fields) < 6 || fields[0] != "" || !strings.EqualFold(fields[1], "file") {
		return l, ErrNotLink
	}

	name, err := url.PathUnescape(fields[2])
	if err != nil {
		// A name with a stray '%' was written by a client that did not escape;
		// it is still a name.
		name = fields[2]
	}
	if strings.TrimSpace(name) == "" {
		return l, ErrNoName
	}
	l.Name = name

	size, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return l, fmt.Errorf("%w: %q", ErrSize, fields[3])
	}
	l.Size = size

	hash, err := hex.DecodeString(fields[4])
	if err != nil || len(hash) != HashSize {
		return l, fmt.Errorf("%w: %q is not %d hex bytes", ErrHash, fields[4], HashSize)
	}
	l.Hash = hash

	for _, field := range fields[5:] {
		if len(field) <= len(aichPrefix) || !strings.EqualFold(field[:len(aichPrefix)], aichPrefix) {
			continue
		}

		aich, err := base32.StdEncoding.DecodeString(strings.ToUpper(field[len(aichPrefix):]))
		if err != nil || len(aich) != AICHSize {
			return l, fmt.Errorf("%w: %q is not a base32 AICH hash", ErrHash, field)
		}
		l.AICH = aich
	}

	return l, nil
}
