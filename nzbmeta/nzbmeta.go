// Package nzbmeta parses an NZB document and derives the identity a meta row of
// kind NZB is built from.
//
// It is the Usenet counterpart of torrentmeta, and it keeps that package's
// rules: describe what is there or refuse it, never normalise it into something
// that would hash differently; repair only for display, and say so when you
// did; bound every allocation. Its only dependency is metahash plus the
// standard library, so the module's dependency budget — protobuf and connect —
// is untouched.
//
// # The identity rule
//
// An NZB has no infohash, and hashing the XML would make a re-generated
// document with different whitespace, a different generator comment or a
// reordered <head> into a different release. So the identity is a digest over
// the one thing that actually names the content on Usenet, the article
// message-ids. It is specified in eNode-go's
// docs/meta-search-torrent-usenet-plan.local.md §3.4 and in identity.go, and
// pinned by testdata/nzb-identity-vectors.json.
//
// Two properties follow, and both are wanted. A repost of the same articles
// under a different group and subject is deliberately the *same* release. And
// stripping <head> does not move the digest, so a daemon may serve a
// password-redacted copy that still passes a client's fold check.
//
// # Why there is a subject parser here as well as in the crawler
//
// This one is the client's: it mirrors eMuleQt's SubjectParser so that a row's
// filename and part counter read the same on both sides of the wire, and it
// runs over an NZB that already exists. The crawler's pkg/subject and
// pkg/namematch run over NNTP overview lines instead, mirror nZEDb's corpus, and
// decide what a release is *called*. They are two different questions asked of
// two different inputs; merging them would make each answer worse.
package nzbmeta

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits that keep a hostile or broken document from turning into an
// allocation. They are far above anything real: the largest releases in the
// wild are a few thousand files and a few hundred thousand articles.
const (
	MaxDocumentBytes   = 64 << 20
	MaxFiles           = 100_000
	MaxSegments        = 4_000_000
	MaxSegmentsPerFile = 500_000
	MaxGroupsPerFile   = 64
	MaxSubjectBytes    = 8192
	MaxMessageIDBytes  = 1000
	MaxMetaEntries     = 256
)

// The NZB 1.1 document identifiers. They are written on output and ignored on
// input: a crawl meets documents with this namespace, with a wrong one, and
// with none at all, and refusing any of those helps nobody — so elements are
// matched on their local name, exactly as eMuleQt's reader does.
const (
	Namespace  = "http://www.newzbin.com/DTD/2003/nzb"
	DoctypeID  = "-//newzBin//DTD NZB 1.1//EN"
	DoctypeURL = "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd"
)

// The <meta type> values with a defined meaning.
const (
	MetaName     = "name"
	MetaPassword = "password"
	MetaCategory = "category"
)

// Errors returned by this package. They are distinguishable because callers act
// differently on each: an empty document is a fetch that failed, while one that
// parses and lists nothing is almost always an indexer's HTML error page.
var (
	ErrEmpty      = errors.New("nzbmeta: the document is empty")
	ErrTooLarge   = errors.New("nzbmeta: the document is larger than MaxDocumentBytes")
	ErrNotNZB     = errors.New("nzbmeta: the document lists no files — is it really an NZB?")
	ErrNoSegments = errors.New("nzbmeta: the document has no usable segment, so it has no identity")
	ErrTooMany    = errors.New("nzbmeta: the document exceeds a structural limit")
	ErrCharset    = errors.New("nzbmeta: the document declares a character set this package cannot read")
	ErrSyntax     = errors.New("nzbmeta: the document is not well-formed XML")
)

// Segment is one article of one file.
//
// Bytes is the *encoded* size — yEnc overhead and line breaks included — and
// never a file offset. Decoded offsets cannot be derived from an NZB at all;
// only the article's own "=ypart begin/end" is authoritative. eMuleQt states the
// same trap at the top of NzbInfo.h, and it is the field consumers get wrong.
type Segment struct {
	// MessageID is the id without angle brackets. The wire form adds them back.
	//
	// It is normalised exactly once, where the document is read — see
	// NormalizeMessageID, which is deliberately not idempotent.
	MessageID string

	Bytes uint64

	// Number is the part number as the document states it, 1-based by
	// convention. Zero means the document did not say.
	Number int
}

// File is one posted file: one <file> element.
//
// It mirrors eMuleQt's NzbFileInfo field for field, so the C++ side is
// checkable against the same vectors, with three named divergences: qint64
// bytes becomes uint64, the epoch becomes a time.Time, and Index is new —
// model.Entry.FileIndex needs an ordinal and the C++ side reads the position in
// its own list.
type File struct {
	// Index is this file's position in Document.Files, 0-based.
	//
	// It counts only files that survived parsing: a <file> with no usable
	// segment is dropped, as eMuleQt drops it, because such a file contributes
	// nothing to the identity and can never be fetched — so numbering it would
	// give the two implementations different indexes for the same release.
	Index int

	Subject string

	// SubjectValidUTF8 is false when the document held bytes that are not valid
	// UTF-8 and Subject is therefore a repaired version of it. A row built from
	// such a subject must not claim its path is authoritative: another reader
	// repairs the same bytes differently.
	SubjectValidUTF8 bool

	Poster string

	// Date is the posting date, or the zero time when the document did not say.
	Date time.Time

	Groups   []string
	Segments []Segment

	// FileName is the filename recovered from the subject. Empty is a normal
	// outcome, not an error: an obfuscated post carries no readable name, and
	// the real one only appears in the first article's "=ybegin name=".
	FileName string

	// PartsTotal is how many articles the subject's (n/m) counter claims. Zero
	// when there is no counter — also normal, and not the same as "no parts".
	PartsTotal int
}

// Meta is one <head><meta> entry, kept in document order.
type Meta struct {
	Type  string
	Value string
}

// Document is one NZB.
//
// eMuleQt's NzbInfo carries name and password as fields; here they are derived
// from Meta, because re-encoding a document byte for byte needs the entries in
// the order they were written and a pair of fields cannot express that.
type Document struct {
	Meta  []Meta
	Files []File

	// Repaired is true when the raw bytes were not valid UTF-8 and were
	// repaired before parsing.
	Repaired bool

	// DroppedFiles counts <file> elements with no usable segment. They are not
	// an error — a generator that emits one is common — but a document that is
	// mostly these is worth noticing.
	DroppedFiles int
}

// MetaValue returns the first <meta> of that type, compared case-insensitively
// as eMuleQt compares it, or "".
func (d *Document) MetaValue(typ string) string {
	for _, m := range d.Meta {
		if strings.EqualFold(m.Type, typ) {
			return m.Value
		}
	}

	return ""
}

// Name is the release's own name from <meta type="name">, or "".
func (d *Document) Name() string { return d.MetaValue(MetaName) }

// Password is the archive password from <meta type="password">, or "".
func (d *Document) Password() string { return d.MetaValue(MetaPassword) }

// Category is the indexer's category from <meta type="category">, or "".
func (d *Document) Category() string { return d.MetaValue(MetaCategory) }

// Protected reports whether the document says the release is password
// protected. It is what sets metahash's FlagProtected.
func (d *Document) Protected() bool { return d.Password() != "" }

// SegmentCount is how many articles the document lists.
func (d *Document) SegmentCount() int {
	total := 0
	for i := range d.Files {
		total += len(d.Files[i].Segments)
	}

	return total
}

// TotalEncodedBytes sums every listed article's encoded size.
func (d *Document) TotalEncodedBytes() uint64 {
	var total uint64
	for i := range d.Files {
		total += d.Files[i].EncodedBytes()
	}

	return total
}

// ValidUTF8 reports whether every subject came through unrepaired.
func (d *Document) ValidUTF8() bool {
	for i := range d.Files {
		if !d.Files[i].SubjectValidUTF8 {
			return false
		}
	}

	return true
}

// WithoutHead returns a copy with no <meta> entries at all.
//
// It is what catalog.serve_password_meta uses to redact a password while
// leaving the release verifiable: the identity is a digest over segments, so
// removing the head cannot move it, and a client that folds the redacted bytes
// still gets the advertised digest. A test asserts exactly that.
func (d *Document) WithoutHead() *Document {
	out := *d
	out.Meta = nil

	return &out
}

// EncodedBytes sums this file's articles.
func (f File) EncodedBytes() uint64 {
	var total uint64
	for _, s := range f.Segments {
		total += s.Bytes
	}

	return total
}

// MeanSegmentBytes is the mean encoded article size, used to price a shortfall
// in bytes. Zero for a file with no segments, which the parser drops anyway.
func (f File) MeanSegmentBytes() uint64 {
	if len(f.Segments) == 0 {
		return 0
	}

	return f.EncodedBytes() / uint64(len(f.Segments))
}

// HasAllSegments reports whether the segment numbers cover 1..PartsTotal
// exactly.
//
// Only meaningful when PartsTotal is known; false here means the *document* is
// short, which is a different thing from an article being missing on a server.
// Duplicates are caught: a size check alone would pass a list that has a hole.
func (f File) HasAllSegments() bool {
	if f.PartsTotal <= 0 {
		return len(f.Segments) > 0
	}
	if len(f.Segments) != f.PartsTotal {
		return false
	}

	seen := make([]bool, f.PartsTotal+1)
	for _, s := range f.Segments {
		if s.Number < 1 || s.Number > f.PartsTotal || seen[s.Number] {
			return false
		}
		seen[s.Number] = true
	}

	return true
}

// MissingSegmentCount is how many articles the subject counter claims that this
// file does not list.
//
// Zero when PartsTotal is 0 — not because nothing is missing, but because an
// obfuscated post carries no counter and the question cannot be answered.
// Shortfall.UnknownFiles is what carries the fact that no opinion was
// available. Also zero when the counter is *lower* than the segment list, which
// real posts do: some posters put the file count in the subject rather than the
// part count.
func (f File) MissingSegmentCount() int {
	if f.PartsTotal <= 0 {
		return 0
	}
	if listed := len(f.Segments); f.PartsTotal > listed {
		return f.PartsTotal - listed
	}

	return 0
}

// Parse reads an NZB.
//
// Elements are matched on their local name, so the newzbin namespace, a wrong
// one and none at all all parse. A <file> with no usable segment is dropped and
// counted. A document that parses but lists no file at all is refused with
// ErrNotNZB: it is almost always an indexer's HTML error page served under a
// .nzb name, and calling that an empty success puts a permanently stalled item
// in a queue.
//
// Invalid UTF-8 is repaired rather than refused, because that is what the other
// implementation does — Qt's codec substitutes U+FFFD where Go's XML decoder
// hard-errors — and a release whose poster wrote latin-1 is still a release.
// Document.Repaired and File.SubjectValidUTF8 record that it happened.
func Parse(raw []byte) (*Document, error) {
	if len(raw) == 0 {
		return nil, ErrEmpty
	}
	if len(raw) > MaxDocumentBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, len(raw))
	}

	// The declared charset is settled before validity is judged: repairing first
	// would destroy the very bytes a latin-1 document means. An unreadable
	// charset is refused here rather than inside the decoder, because the
	// decoder reports a CharsetReader's error with %v and the sentinel would not
	// survive to the caller.
	declared := declaredEncoding(raw)

	repaired := false
	switch {
	case isUTF8Name(declared):
		if !utf8.Valid(raw) {
			raw = bytes.ToValidUTF8(raw, []byte(string(utf8.RuneError)))
			repaired = true
		}
	case isLatin1Name(declared):
		// charsetReader transcodes it.
	default:
		return nil, fmt.Errorf("%w: %q", ErrCharset, declared)
	}

	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.CharsetReader = charsetReader

	doc := &Document{Repaired: repaired}

	var (
		current File
		inFile  bool
		total   int
	)

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSyntax, err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "file":
				file, err := fileFromAttrs(t.Attr, repaired)
				if err != nil {
					return nil, err
				}
				current, inFile = file, true

			case "group":
				text, err := elementText(dec)
				if err != nil {
					return nil, err
				}
				if text = strings.TrimSpace(text); inFile && text != "" {
					if len(current.Groups) >= MaxGroupsPerFile {
						return nil, fmt.Errorf("%w: more than %d groups on one file", ErrTooMany, MaxGroupsPerFile)
					}
					current.Groups = append(current.Groups, text)
				}

			case "segment":
				text, err := elementText(dec)
				if err != nil {
					return nil, err
				}
				if !inFile {
					continue
				}
				if total >= MaxSegments {
					return nil, fmt.Errorf("%w: more than %d segments", ErrTooMany, MaxSegments)
				}

				// An empty id is skipped rather than refused, and that is the
				// identity's rule 6 rather than leniency: it is what makes a
				// reader that drops such a file and one that keeps it agree.
				id := NormalizeMessageID(text)
				if id == "" {
					continue
				}
				if len(id) > MaxMessageIDBytes {
					return nil, fmt.Errorf("%w: a message-id of %d bytes", ErrTooMany, len(id))
				}
				if len(current.Segments) >= MaxSegmentsPerFile {
					return nil, fmt.Errorf("%w: more than %d segments on one file", ErrTooMany, MaxSegmentsPerFile)
				}

				current.Segments = append(current.Segments, Segment{
					MessageID: id,
					Bytes:     attrUint(t.Attr, "bytes"),
					Number:    attrInt(t.Attr, "number"),
				})
				total++

			case "meta":
				text, err := elementText(dec)
				if err != nil {
					return nil, err
				}
				if typ := attr(t.Attr, "type"); typ != "" {
					if len(doc.Meta) >= MaxMetaEntries {
						return nil, fmt.Errorf("%w: more than %d <meta> entries", ErrTooMany, MaxMetaEntries)
					}
					doc.Meta = append(doc.Meta, Meta{Type: typ, Value: text})
				}
			}

		case xml.EndElement:
			if t.Name.Local != "file" || !inFile {
				continue
			}
			inFile = false

			// A file with no article is dropped, as eMuleQt drops it: it
			// contributes nothing to the identity and can never be fetched, so
			// keeping it would only give the two sides different file indexes.
			if len(current.Segments) == 0 {
				doc.DroppedFiles++

				continue
			}
			if len(doc.Files) >= MaxFiles {
				return nil, fmt.Errorf("%w: more than %d files", ErrTooMany, MaxFiles)
			}

			current.Index = len(doc.Files)
			doc.Files = append(doc.Files, current)
		}
	}

	if len(doc.Files) == 0 {
		return nil, ErrNotNZB
	}

	return doc, nil
}

// -- internals ---------------------------------------------------------------

// fileFromAttrs reads a <file>'s attributes and runs the subject heuristics
// over its subject.
func fileFromAttrs(attrs []xml.Attr, repaired bool) (File, error) {
	subject := attr(attrs, "subject")
	if len(subject) > MaxSubjectBytes {
		return File{}, fmt.Errorf("%w: a subject of %d bytes", ErrTooMany, len(subject))
	}

	// Attributing the repair to a particular subject is a heuristic, because the
	// repaired bytes are all the decoder ever saw. It errs towards flagging: a
	// subject that genuinely contained U+FFFD, in a document that needed repair
	// somewhere else, is flagged too. The only consequence of a false flag is
	// that the row does not claim its path is authoritative, which costs nothing;
	// a false *clear* would have a client looking for a file that does not exist.
	valid := !repaired || !strings.ContainsRune(subject, utf8.RuneError)

	info := ParseSubject(subject)

	return File{
		Subject:          subject,
		SubjectValidUTF8: valid,
		Poster:           attr(attrs, "poster"),
		Date:             parseDate(attr(attrs, "date")),
		FileName:         info.FileName,
		PartsTotal:       info.Total,
	}, nil
}

// elementText reads an element's character data, resolving entities, and stops
// at its end tag. Child elements contribute nothing, which is what keeps a
// <segment> holding stray markup from swallowing the rest of the document.
func elementText(dec *xml.Decoder) (string, error) {
	var sb strings.Builder

	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return sb.String(), nil
		}
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrSyntax, err)
		}

		switch t := tok.(type) {
		case xml.CharData:
			if depth == 0 {
				sb.Write(t)
			}
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if depth == 0 {
				return sb.String(), nil
			}
			depth--
		}
	}
}

func attr(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}

	return ""
}

func attrInt(attrs []xml.Attr, name string) int {
	n, err := strconv.Atoi(strings.TrimSpace(attr(attrs, name)))
	if err != nil {
		return 0
	}

	return n
}

func attrUint(attrs []xml.Attr, name string) uint64 {
	n, err := strconv.ParseUint(strings.TrimSpace(attr(attrs, name)), 10, 64)
	if err != nil {
		return 0
	}

	return n
}

// parseDate reads a @date. It is seconds since the epoch; anything else, and
// anything at or below zero, is "the document did not say".
func parseDate(s string) time.Time {
	secs, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}
	}

	return time.Unix(secs, 0).UTC()
}

// declaredEncoding reads the encoding of an XML declaration, or "" when there
// is none. Only the head of the document is looked at, because a declaration
// that is not first is not a declaration.
func declaredEncoding(raw []byte) string {
	const window = 256

	head := raw
	if len(head) > window {
		head = head[:window]
	}
	if !bytes.HasPrefix(bytes.TrimLeft(head, " \t\r\n\uFEFF"), []byte("<?xml")) {
		return ""
	}

	end := bytes.Index(head, []byte("?>"))
	if end < 0 {
		return ""
	}

	decl := string(head[:end])
	idx := strings.Index(decl, "encoding")
	if idx < 0 {
		return ""
	}

	rest := decl[idx+len("encoding"):]
	quote := strings.IndexAny(rest, `"'`)
	if quote < 0 {
		return ""
	}
	rest = rest[quote+1:]

	if closing := strings.IndexAny(rest, `"'`); closing >= 0 {
		return strings.TrimSpace(rest[:closing])
	}

	return ""
}

func isUTF8Name(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return true
	default:
		return false
	}
}

func isLatin1Name(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "iso-8859-1", "iso8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		return true
	default:
		return false
	}
}

// charsetReader transcodes the one non-UTF-8 family that occurs in real NZBs.
//
// The latin-1 family is a byte-to-rune mapping and costs five lines, so there
// is no reason to refuse it; windows-1252 is read as latin-1, which differs
// only in the C1 range, where a subject holding a control character is already
// mojibake. Everything else is refused rather than guessed: a wrong guess
// produces a release name nobody searches for.
//
// A transcode cannot move the identity. A message-id is ASCII by RFC 5322, so
// the bytes the digest covers are the same in either reading.
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	if !isLatin1Name(charset) {
		return nil, fmt.Errorf("%w: %q", ErrCharset, charset)
	}

	raw, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	sb.Grow(len(raw))
	for _, b := range raw {
		sb.WriteRune(rune(b))
	}

	return strings.NewReader(sb.String()), nil
}
