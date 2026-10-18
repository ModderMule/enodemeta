// Package linkcontainer is the identity of a link container: a .dlc, .ccf or
// .rsdf file that a web page offers in place of its links (amendment 27).
//
// These files are encrypted, and the keys belong to the services that made
// them, so nothing here reads one. A container is named by the SHA-256 of its
// bytes: a daemon serves the bytes unchanged, and a client checks them against
// the row with enodemeta.VerifyMetaFile.
package linkcontainer

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path"
	"strings"
)

// IdentityLen is the identity's length in bytes, and the length
// metahash.KindContainer folds.
const IdentityLen = sha256.Size

// MaxSize is the largest container a daemon serves and a client accepts. Real
// containers are a few KiB; a larger file is no container.
const MaxSize = 1 << 20

// The container formats, by their file extension.
const (
	FormatDLC  = "dlc"
	FormatCCF  = "ccf"
	FormatRSDF = "rsdf"
)

// The content types a daemon answers FetchMetaFile with. None is registered,
// so these are the x- spellings.
const (
	ContentTypeDLC  = "application/x-dlc"
	ContentTypeCCF  = "application/x-ccf"
	ContentTypeRSDF = "application/x-rsdf"
)

// Errors returned by this package.
var (
	ErrEmpty    = errors.New("linkcontainer: a container has bytes")
	ErrTooLarge = errors.New("linkcontainer: a container is at most 1 MiB")
)

// Identity is the SHA-256 of the container's bytes.
func Identity(content []byte) [IdentityLen]byte {
	return sha256.Sum256(content)
}

// IdentityOf is Identity for bytes that came from elsewhere: it refuses what
// cannot be a container.
func IdentityOf(content []byte) ([IdentityLen]byte, error) {
	switch {
	case len(content) == 0:
		return [IdentityLen]byte{}, ErrEmpty
	case len(content) > MaxSize:
		return [IdentityLen]byte{}, fmt.Errorf("%w: got %d bytes", ErrTooLarge, len(content))
	}

	return Identity(content), nil
}

// Format is the container's format from a file name or address, "" when the
// name ends in none of the three extensions.
func Format(name string) string {
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}

	switch ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), ".")); ext {
	case FormatDLC, FormatCCF, FormatRSDF:
		return ext
	default:
		return ""
	}
}

// ContentType is the content type for a container's file name or address, ""
// when it is no container.
func ContentType(name string) string {
	switch Format(name) {
	case FormatDLC:
		return ContentTypeDLC
	case FormatCCF:
		return ContentTypeCCF
	case FormatRSDF:
		return ContentTypeRSDF
	default:
		return ""
	}
}

// FormatOf is the format a content type names, "" when it names none. A
// client uses it to give a fetched container its file extension.
func FormatOf(contentType string) string {
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = contentType[:i]
	}

	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case ContentTypeDLC:
		return FormatDLC
	case ContentTypeCCF:
		return FormatCCF
	case ContentTypeRSDF:
		return FormatRSDF
	default:
		return ""
	}
}
