package nzbmeta

import (
	"encoding/xml"
	"sort"
	"strconv"
	"strings"
)

// indentUnit is one level of indentation. It is not configurable: two
// implementations that indent differently produce different bytes for the same
// release, and the only thing that would buy is a preference.
const indentUnit = "  "

// Encoder writes an NZB.
//
// The zero Encoder is the canonical writer, and it is what the two round-trip
// invariants are stated over:
//
//	Identity(Parse(Encode(d))) == Identity(d)
//	Encode(Parse(Encode(d)))   == Encode(d)     byte for byte
//
// They hold because Encode writes exactly what Parse keeps and nothing else, and
// because it makes the same three omissions Parse makes: a segment with no
// message-id, a file with no usable segment, and a <meta> with no type. A
// generator comment is the one thing that can be added, and it is deliberately
// outside Document — a comment carried in the type would have to survive a parse
// for the invariant to hold, and comments are the one part of an NZB nothing
// reads.
//
// One document shape is outside both invariants, and it is worth naming rather
// than hiding. NormalizeMessageID removes one angle bracket from each end
// independently rather than as a pair, matching eMuleQt, so it is not
// idempotent: an id that is still "<x>" after normalisation came from
// "<<x>>", loses another bracket on the next read, and neither invariant can
// hold for it under any writer that emits ids the way real writers do. Such a
// string is not a message-id — RFC 5322 forbids an angle bracket inside one — and
// TestRoundTripPathologicalMessageID pins exactly what happens to it.
type Encoder struct {
	// Comment is written as an XML comment before <nzb>, or nothing when empty.
	//
	// It must not carry a timestamp. The crawler's store is content-addressed by
	// identity, so a timestamped comment would not break verification — but it
	// would make the same release produce different bytes on every write, and
	// then nothing can tell a re-generated NZB from a changed one.
	Comment string
}

// Encode writes a document with the canonical writer.
func Encode(d *Document) []byte { return Encoder{}.Encode(d) }

// Encode writes a document.
func (e Encoder) Encode(d *Document) []byte {
	var sb strings.Builder

	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<!DOCTYPE nzb PUBLIC "` + DoctypeID + `" "` + DoctypeURL + `">` + "\n")

	if comment := strings.TrimSpace(e.Comment); comment != "" {
		// "--" cannot appear inside an XML comment and there is no escape for
		// it, so it is spaced out rather than dropped: the comment is provenance
		// and a mangled one is still readable.
		sb.WriteString("<!-- " + strings.ReplaceAll(comment, "--", "- -") + " -->\n")
	}

	sb.WriteString(`<nzb xmlns="` + Namespace + `">` + "\n")

	writeHead(&sb, d.Meta)

	for i := range d.Files {
		writeFile(&sb, d.Files[i])
	}

	sb.WriteString("</nzb>\n")

	return []byte(sb.String())
}

// Builder assembles a document from crawled rows.
//
// It exists so that the crawler cannot skip the two things that make an id
// reproducible: message-ids are normalised exactly once, here, and the finished
// document is put in CanonicalOrder.
type Builder struct {
	meta  []Meta
	files []File
}

// SetMeta sets a <head><meta type=…>. An empty value removes the entry, and
// setting a type twice replaces it rather than appending, so the head stays the
// map it is read as.
func (b *Builder) SetMeta(typ, value string) *Builder {
	typ = strings.TrimSpace(typ)
	if typ == "" {
		return b
	}

	for i := range b.meta {
		if !strings.EqualFold(b.meta[i].Type, typ) {
			continue
		}

		if value == "" {
			b.meta = append(b.meta[:i], b.meta[i+1:]...)
		} else {
			b.meta[i].Value = value
		}

		return b
	}

	if value != "" {
		b.meta = append(b.meta, Meta{Type: typ, Value: value})
	}

	return b
}

// AddFile adds one posted file, normalising each segment's message-id and
// dropping the ones with nothing left. A file with no usable segment is not
// added at all, which is what Parse does with one.
func (b *Builder) AddFile(f File) *Builder {
	segments := make([]Segment, 0, len(f.Segments))
	for _, s := range f.Segments {
		s.MessageID = NormalizeMessageID(s.MessageID)
		if s.MessageID == "" {
			continue
		}
		segments = append(segments, s)
	}

	if len(segments) == 0 {
		return b
	}

	f.Segments = segments

	// The subject heuristics are re-run rather than trusted, so a document the
	// crawler wrote reads back with the filename and part counter a client will
	// compute from it.
	info := ParseSubject(f.Subject)
	f.FileName = info.FileName
	f.PartsTotal = info.Total

	b.files = append(b.files, f)

	return b
}

// Document returns the assembled document in canonical order.
func (b *Builder) Document() *Document {
	return &Document{Meta: b.meta, Files: CanonicalOrder(b.files)}
}

// CanonicalOrder returns the files in the order our own writer always uses:
// ascending by subject, then by their lowest message-id, with each file's
// segments ascending by number and Index renumbered to match.
//
// This is the most load-bearing decision on the path from a crawl to an id. The
// identity does not depend on it — the digest sorts segments itself and takes
// files in document order, so two orderings of the same articles already agree —
// but the *bytes* do, and content-addressed storage means a release assembled
// twice from the same articles must produce the same file, not merely the same
// digest. Both reference projects already ORDER BY the binary name and the part
// number, which is what makes this reproducible from their data too.
//
// An imported third-party document is never reordered. The same release from two
// indexers may therefore carry two ids, which the name-plus-poster-plus-size
// dedupe is what limits.
func CanonicalOrder(files []File) []File {
	out := make([]File, len(files))
	copy(out, files)

	for i := range out {
		out[i].Segments = sortedSegments(out[i].Segments)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}

		return lowestMessageID(out[i]) < lowestMessageID(out[j])
	})

	for i := range out {
		out[i].Index = i
	}

	return out
}

// -- internals ---------------------------------------------------------------

func writeHead(sb *strings.Builder, meta []Meta) {
	written := false

	for _, m := range meta {
		if strings.TrimSpace(m.Type) == "" {
			continue
		}

		if !written {
			sb.WriteString(indentUnit + "<head>\n")
			written = true
		}

		sb.WriteString(indentUnit + indentUnit + `<meta type="`)
		escape(sb, m.Type)
		sb.WriteString(`">`)
		escape(sb, m.Value)
		sb.WriteString("</meta>\n")
	}

	if written {
		sb.WriteString(indentUnit + "</head>\n")
	}
}

func writeFile(sb *strings.Builder, f File) {
	segments := make([]Segment, 0, len(f.Segments))
	for _, s := range f.Segments {
		if s.MessageID != "" {
			segments = append(segments, s)
		}
	}

	if len(segments) == 0 {
		return
	}

	sb.WriteString(indentUnit + "<file")
	if f.Poster != "" {
		sb.WriteString(` poster="`)
		escape(sb, f.Poster)
		sb.WriteString(`"`)
	}
	if !f.Date.IsZero() {
		sb.WriteString(` date="` + strconv.FormatInt(f.Date.Unix(), 10) + `"`)
	}
	sb.WriteString(` subject="`)
	escape(sb, f.Subject)
	sb.WriteString("\">\n")

	if len(f.Groups) > 0 {
		sb.WriteString(indentUnit + indentUnit + "<groups>\n")
		for _, g := range f.Groups {
			sb.WriteString(indentUnit + indentUnit + indentUnit + "<group>")
			escape(sb, g)
			sb.WriteString("</group>\n")
		}
		sb.WriteString(indentUnit + indentUnit + "</groups>\n")
	}

	sb.WriteString(indentUnit + indentUnit + "<segments>\n")
	for _, s := range segments {
		sb.WriteString(indentUnit + indentUnit + indentUnit +
			`<segment bytes="` + strconv.FormatUint(s.Bytes, 10) +
			`" number="` + strconv.Itoa(s.Number) + `">`)

		// Bare, without the angle brackets, because that is what every real
		// writer emits and what a reader that is not a real XML parser can still
		// find: a literal '<' is illegal in element text, so a bracketed id has
		// to be written as "&lt;id&gt;" and a regex-based reader then hands its
		// fetcher an id with entities in it.
		escape(sb, s.MessageID)
		sb.WriteString("</segment>\n")
	}
	sb.WriteString(indentUnit + indentUnit + "</segments>\n")

	sb.WriteString(indentUnit + "</file>\n")
}

// escape writes text XML-escaped. xml.EscapeText escapes the quote, the
// apostrophe and every line ending as well as the three markup characters, which
// is what makes one function correct for both attribute values and element text.
func escape(sb *strings.Builder, text string) {
	_ = xml.EscapeText(sb, []byte(text))
}

func lowestMessageID(f File) string {
	lowest := ""
	for _, s := range f.Segments {
		if lowest == "" || s.MessageID < lowest {
			lowest = s.MessageID
		}
	}

	return lowest
}
