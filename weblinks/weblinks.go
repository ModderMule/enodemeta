// Package weblinks is the metafile of a web release: the links found on one
// page, to file hosters, stream hosts and direct media.
//
// A web row carries no link. The links are what FetchMetaFile returns, as a
// text/uri-list (RFC 2483), and a client fetches that only when its user acts
// on the row. That is the same shape as a torrent or an NZB, and it is what
// keeps a server from handing every searching client a URL to open: see the
// note on FT_META_RESERVED in the tags package.
//
// # The container
//
//	#title: Some Release 1080p
//	#file: 0
//	#name: some.release.part1.rar
//	#size: 1073741824
//	#hoster: examplehost
//	https://examplehost.test/f/abc123
//	#file: 0
//	#hoster: otherhost
//	https://otherhost.test/d/xyz789
//
// A line that starts with '#' is a comment, as RFC 2483 has it, and a comment
// of the form "#key: value" is a hint. A hint describes the next link and is
// forgotten after it; title and protected describe the whole set and may stand
// anywhere. Two links with the same file number are mirrors of one file.
//
// Hints are advice. They are not covered by the identity, so a daemon can
// learn a size or lose a mirror's name without the release becoming another
// release. The links are what the identity covers: see Identity.
package weblinks

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ModderMule/enodemeta/metahash"
)

// ContentType is what a daemon answers FetchMetaFile with for a web release.
const ContentType = "text/uri-list"

// MaxLinks is the most links one container may hold. A page with more than
// this is a directory, not a release, and a reader that accepted any number
// would let one row cost a client an unbounded parse.
const MaxLinks = 4096

// Errors returned by this package.
var (
	// ErrNoLinks is a container with no link in it, which has no identity.
	ErrNoLinks = errors.New("weblinks: the container holds no link")

	// ErrLink is a line that is neither a comment nor a link this version
	// accepts.
	ErrLink = errors.New("weblinks: not an http or https link")

	// ErrTooManyLinks is a container past MaxLinks.
	ErrTooManyLinks = errors.New("weblinks: too many links")
)

// Link is one link of a set.
type Link struct {
	// URL is the link in canonical form: see Canonical.
	URL string

	// File is the ordinal of the file this link downloads, which is the
	// file_index its row carries. Mirrors of one file share it.
	File uint32

	// Name is the file's name and Size its length in bytes, when the daemon
	// knows them. Hoster names the site the link points at.
	Name   string
	Size   uint64
	Hoster string
}

// Document is a parsed container.
type Document struct {
	// Title is the release's name as the page gave it.
	Title string

	// Protected marks a release whose files need a password.
	Protected bool

	Links []Link
}

// Parse reads a container.
//
// It refuses the whole document on one bad line. A reader that skipped what it
// did not understand would compute an identity over fewer links than the
// daemon did, and the mismatch would say nothing about which line caused it.
func Parse(raw []byte) (*Document, error) {
	doc := &Document{}

	var next Link

	for n, line := range bytes.Split(raw, []byte("\n")) {
		text := trim(string(line))
		if text == "" {
			continue
		}

		if strings.HasPrefix(text, "#") {
			key, value, ok := hint(text)
			if !ok {
				continue
			}

			switch key {
			case "title":
				doc.Title = value
			case "protected":
				doc.Protected = value == "1"
			case "file":
				next.File = uint32(parseUint(value, 32))
			case "name":
				next.Name = value
			case "size":
				next.Size = parseUint(value, 64)
			case "hoster":
				next.Hoster = value
			}

			continue
		}

		canonical, err := Canonical(text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		if len(doc.Links) == MaxLinks {
			return nil, fmt.Errorf("%w: more than %d", ErrTooManyLinks, MaxLinks)
		}

		next.URL = canonical
		doc.Links = append(doc.Links, next)
		next = Link{}
	}

	if len(doc.Links) == 0 {
		return nil, ErrNoLinks
	}

	return doc, nil
}

// Bytes writes the container.
//
// The output is deterministic: links are ordered by file and then by URL, so
// the same set is the same bytes whatever order a page listed it in. Lines end
// in CRLF, which is what RFC 2483 asks for; Parse accepts a bare LF as well.
func (d *Document) Bytes() []byte {
	var sb strings.Builder

	writeHint(&sb, "title", d.Title)
	if d.Protected {
		writeHint(&sb, "protected", "1")
	}

	for _, link := range d.sorted() {
		writeHint(&sb, "file", strconv.FormatUint(uint64(link.File), 10))
		writeHint(&sb, "name", link.Name)
		if link.Size > 0 {
			writeHint(&sb, "size", strconv.FormatUint(link.Size, 10))
		}
		writeHint(&sb, "hoster", link.Hoster)

		sb.WriteString(link.URL)
		sb.WriteString("\r\n")
	}

	return []byte(sb.String())
}

// FileCount is how many files the set downloads: the distinct file numbers,
// however many mirrors each has. It is the file_count of the release's rows.
func (d *Document) FileCount() uint32 {
	seen := make(map[uint32]struct{}, len(d.Links))
	for _, link := range d.Links {
		seen[link.File] = struct{}{}
	}

	return uint32(len(seen))
}

// MintInput describes the set to metahash, so the rules for a web row's flags
// live in one place rather than at every call site.
//
// fileIndex is a link's File, or metahash.FileIndexWholeSet32 for the row that
// stands for the whole release. Mint sets FlagPathAuthoritative for every web
// row by itself, because nothing fixes the order of files on a page.
func (d *Document) MintInput(fileIndex uint32) (metahash.MintInput, error) {
	identity, err := Identity(d)
	if err != nil {
		return metahash.MintInput{}, err
	}

	return metahash.MintInput{
		Kind:      metahash.KindWeb,
		Identity:  identity[:],
		FileIndex: fileIndex,
		FileCount: d.FileCount(),
		Protected: d.Protected,
	}, nil
}

// Canonical puts one link into the form the identity is taken over.
//
// The rules are written out in Identity. They are string operations on purpose:
// a URL library normalises more than this and differently in every language,
// and three implementations have to agree on the bytes.
func Canonical(link string) (string, error) {
	link = trim(link)

	for i := 0; i < len(link); i++ {
		if link[i] <= ' ' || link[i] == 0x7F {
			return "", fmt.Errorf("%w: a control character or space at byte %d", ErrLink, i)
		}
	}

	scheme, rest, ok := strings.Cut(link, "://")
	if !ok {
		return "", fmt.Errorf("%w: no scheme", ErrLink)
	}

	scheme = lowerASCII(scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%w: scheme %q", ErrLink, scheme)
	}

	if cut := strings.IndexByte(rest, '#'); cut >= 0 {
		rest = rest[:cut]
	}

	authority, tail := rest, ""
	if cut := strings.IndexAny(rest, "/?"); cut >= 0 {
		authority, tail = rest[:cut], rest[cut:]
	}

	if strings.Contains(authority, "@") {
		return "", fmt.Errorf("%w: a link with credentials", ErrLink)
	}

	authority = stripDefaultPort(lowerASCII(authority), scheme)
	if authority == "" {
		return "", fmt.Errorf("%w: no host", ErrLink)
	}

	if !strings.HasPrefix(tail, "/") {
		tail = "/" + tail
	}

	return scheme + "://" + authority + tail, nil
}

// -- internals ---------------------------------------------------------------

// sorted returns the links ordered by file and then by URL. It copies, because
// the caller's document is an input.
func (d *Document) sorted() []Link {
	out := make([]Link, len(d.Links))
	copy(out, d.Links)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}

		return out[i].URL < out[j].URL
	})

	return out
}

// hint reads "#key: value". A comment that is not one is not an error.
func hint(line string) (key, value string, ok bool) {
	key, value, ok = strings.Cut(strings.TrimPrefix(line, "#"), ":")
	if !ok {
		return "", "", false
	}

	key = lowerASCII(trim(key))
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}

	return key, trim(value), true
}

// writeHint writes one hint line, or nothing for an empty value. A line break
// inside a value would end the comment and start a link, so it becomes a space.
func writeHint(sb *strings.Builder, key, value string) {
	value = trim(strings.NewReplacer("\r", " ", "\n", " ").Replace(value))
	if value == "" {
		return
	}

	sb.WriteByte('#')
	sb.WriteString(key)
	sb.WriteString(": ")
	sb.WriteString(value)
	sb.WriteString("\r\n")
}

// stripDefaultPort removes ":80" from an http authority, ":443" from an https
// one, and an empty port from either. An IPv6 literal ends in ']', and the
// colons inside it are not a port.
func stripDefaultPort(authority, scheme string) string {
	colon := strings.LastIndexByte(authority, ':')
	if colon < 0 || colon < strings.LastIndexByte(authority, ']') {
		return authority
	}

	port := authority[colon+1:]
	if port == "" || (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		return authority[:colon]
	}

	return authority
}

// parseUint reads a decimal hint, zero when it is not one: a hint is advice,
// and a bad one is no reason to refuse the links beside it.
func parseUint(value string, bits int) uint64 {
	n, err := strconv.ParseUint(value, 10, bits)
	if err != nil {
		return 0
	}

	return n
}

// trim removes ASCII spaces, tabs and carriage returns from both ends. It is
// not strings.TrimSpace, which also removes Unicode spaces a port would have to
// enumerate.
func trim(s string) string {
	return strings.Trim(s, " \t\r")
}

// lowerASCII lowers A to Z and nothing else, for the same reason.
func lowerASCII(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + ('a' - 'A')
		}
	}

	return string(out)
}
