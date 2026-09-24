package nzbmeta

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// sampleNZB is a two-file release with the shapes a crawl actually meets: a
// bracketed message-id beside a bare one, a padded @number, and a <head>.
const sampleNZB = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head>
    <meta type="name">Some.Release.1080p</meta>
    <meta type="password">hunter2</meta>
  </head>
  <file poster="Poster &lt;p@example.invalid&gt;" date="1700000000" subject="[1/2] - &#34;Some.Release.part01.rar&#34; yEnc (1/3)">
    <groups>
      <group>alt.binaries.test</group>
      <group>alt.binaries.other</group>
    </groups>
    <segments>
      <segment bytes="700001" number="002">&lt;b2@example.invalid&gt;</segment>
      <segment bytes="700000" number="1">b1@example.invalid</segment>
    </segments>
  </file>
  <file poster="p@example.invalid" date="1700000100" subject="[2/2] - &#34;Some.Release.vol000+01.par2&#34; yEnc (1/1)">
    <segments>
      <segment bytes="120000" number="1">v1@example.invalid</segment>
    </segments>
  </file>
</nzb>
`

func TestParse(t *testing.T) {
	t.Logf("input:  %d bytes of NZB", len(sampleNZB))

	doc, err := Parse([]byte(sampleNZB))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	if len(doc.Files) != 2 {
		t.Fatalf("got %d files, want 2", len(doc.Files))
	}
	for i := range doc.Files {
		f := doc.Files[i]
		t.Logf("output: file %d index=%d name=%q parts=%d segments=%d bytes=%d groups=%v",
			i, f.Index, f.FileName, f.PartsTotal, len(f.Segments), f.EncodedBytes(), f.Groups)
	}

	first := doc.Files[0]
	if first.Index != 0 || doc.Files[1].Index != 1 {
		t.Errorf("indexes must be document order, got %d and %d", first.Index, doc.Files[1].Index)
	}
	if want := "Some.Release.part01.rar"; first.FileName != want {
		t.Errorf("filename: got %q, want %q", first.FileName, want)
	}
	if first.PartsTotal != 3 {
		t.Errorf("parts total: got %d, want 3 (the last parenthesised pair)", first.PartsTotal)
	}
	if want := "Poster <p@example.invalid>"; first.Poster != want {
		t.Errorf("poster: got %q, want %q — entities must be resolved", first.Poster, want)
	}
	if want := time.Unix(1700000000, 0).UTC(); !first.Date.Equal(want) {
		t.Errorf("date: got %v, want %v", first.Date, want)
	}
	if len(first.Groups) != 2 {
		t.Errorf("groups: got %v", first.Groups)
	}

	// Both spellings of a message-id arrive stripped, and the document order of
	// the segments is kept — the identity is what sorts them.
	if got := []string{first.Segments[0].MessageID, first.Segments[1].MessageID}; got[0] != "b2@example.invalid" || got[1] != "b1@example.invalid" {
		t.Errorf("message ids: got %v", got)
	}
	if first.Segments[0].Number != 2 {
		t.Errorf("a padded @number must read as 2, got %d", first.Segments[0].Number)
	}

	if doc.Name() != "Some.Release.1080p" || doc.Password() != "hunter2" || !doc.Protected() {
		t.Errorf("head: name=%q password=%q protected=%v", doc.Name(), doc.Password(), doc.Protected())
	}
	if doc.SegmentCount() != 3 || doc.TotalEncodedBytes() != 1520001 {
		t.Errorf("segments=%d bytes=%d", doc.SegmentCount(), doc.TotalEncodedBytes())
	}
	if !doc.ValidUTF8() || doc.Repaired || doc.DroppedFiles != 0 {
		t.Errorf("valid=%v repaired=%v dropped=%d", doc.ValidUTF8(), doc.Repaired, doc.DroppedFiles)
	}
	if !doc.Files[1].IsPAR2() || doc.Files[1].RecoveryBlocks() != 1 {
		t.Errorf("the second file is a par2 volume of 1 block, got par2=%v blocks=%d",
			doc.Files[1].IsPAR2(), doc.Files[1].RecoveryBlocks())
	}
}

func TestParseAcceptsAnyNamespace(t *testing.T) {
	for _, c := range []struct {
		label string
		open  string
	}{
		{"the newzbin namespace", `<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">`},
		{"a wrong namespace", `<nzb xmlns="http://example.invalid/nzb">`},
		{"no namespace at all", `<nzb>`},
		{"a prefixed namespace", `<n:nzb xmlns:n="http://www.newzbin.com/DTD/2003/nzb">`},
		{"a byte order mark before the declaration", "\xef\xbb\xbf<?xml version=\"1.0\" encoding=\"UTF-8\"?><nzb>"},
	} {
		t.Run(c.label, func(t *testing.T) {
			closing := "</nzb>"
			if strings.Contains(c.open, "<n:nzb") {
				closing = "</n:nzb>"
			}

			raw := c.open + `<file subject="x (1/1)"><segments><segment number="1">a@b</segment></segments></file>` + closing
			t.Logf("input:  %s", raw)

			doc, err := Parse([]byte(raw))
			if err != nil {
				t.Fatalf("elements are matched on their local name, so this must parse: %v", err)
			}
			t.Logf("output: %d file(s), %d segment(s)", len(doc.Files), doc.SegmentCount())
		})
	}
}

func TestParseDropsAFileWithNoUsableSegment(t *testing.T) {
	raw := `<nzb>` +
		`<file subject="empty"><segments><segment number="1"></segment><segment number="2">   </segment></segments></file>` +
		`<file subject="good (1/1)"><segments><segment number="1">a@b</segment></segments></file>` +
		`</nzb>`
	t.Logf("input:  %s", raw)

	doc, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	t.Logf("output: %d file(s) kept, %d dropped, first index %d",
		len(doc.Files), doc.DroppedFiles, doc.Files[0].Index)

	if len(doc.Files) != 1 || doc.DroppedFiles != 1 {
		t.Fatalf("got %d files and %d dropped, want 1 and 1", len(doc.Files), doc.DroppedFiles)
	}

	// The surviving file is index 0. Numbering the dropped one would give the
	// two implementations different indexes for the same release, and the index
	// is what a row addresses.
	if doc.Files[0].Index != 0 || doc.Files[0].Subject != "good (1/1)" {
		t.Errorf("got index %d subject %q", doc.Files[0].Index, doc.Files[0].Subject)
	}
}

func TestParseRejects(t *testing.T) {
	long := strings.Repeat("a", MaxMessageIDBytes+1)

	for _, c := range []struct {
		label string
		raw   string
		want  error
	}{
		{"empty", "", ErrEmpty},
		{"an indexer's HTML error page", "<html><body>Not found</body></html>", ErrNotNZB},
		{"a well-formed document with no file", `<nzb><head><meta type="name">x</meta></head></nzb>`, ErrNotNZB},
		{"truncated mid-element", `<nzb><file subject="x"><segments><segment number="1">a@b`, ErrSyntax},
		{"an undefined entity", `<nzb><file subject="a&nbsp;b"><segments><segment number="1">a@b</segment></segments></file></nzb>`, ErrSyntax},
		{"a charset nothing can read", `<?xml version="1.0" encoding="EBCDIC-CP-BE"?><nzb/>`, ErrCharset},
		{"an over-long message-id", `<nzb><file subject="x"><segments><segment number="1">` + long + `</segment></segments></file></nzb>`, ErrTooMany},
		{"an over-long subject", `<nzb><file subject="` + strings.Repeat("s", MaxSubjectBytes+1) + `"/></nzb>`, ErrTooMany},
		{"too many groups on one file", `<nzb><file subject="x">` + strings.Repeat("<groups><group>g</group></groups>", MaxGroupsPerFile+1) + `</file></nzb>`, ErrTooMany},
	} {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %.90q (%d bytes)", c.raw, len(c.raw))

			_, err := Parse([]byte(c.raw))
			if err == nil {
				t.Fatalf("%s must be refused", c.label)
			}
			t.Logf("output: %v", err)

			if !errors.Is(err, c.want) {
				t.Errorf("got %v, want it to wrap %v", err, c.want)
			}
		})
	}

	t.Run("larger than MaxDocumentBytes", func(t *testing.T) {
		raw := make([]byte, MaxDocumentBytes+1)
		t.Logf("input:  %d bytes", len(raw))

		_, err := Parse(raw)
		t.Logf("output: %v", err)

		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("got %v, want it to wrap %v", err, ErrTooLarge)
		}
	})
}

func TestParseRepairsInvalidUTF8(t *testing.T) {
	// A latin-1 subject in a document that claims UTF-8. Go's XML decoder
	// hard-errors on this and Qt's substitutes, so refusing would make the two
	// implementations disagree about whether the release exists.
	raw := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>" +
		"<nzb><file subject=\"Caf\xe9.Release (1/1)\"><segments>" +
		"<segment number=\"1\">a@b</segment></segments></file>" +
		"<file subject=\"Clean.Release (1/1)\"><segments>" +
		"<segment number=\"1\">c@d</segment></segments></file></nzb>"
	t.Logf("input:  %q", raw)

	doc, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("invalid UTF-8 must be repaired, not refused: %v", err)
	}
	for i := range doc.Files {
		t.Logf("output: file %d subject=%q valid=%v", i, doc.Files[i].Subject, doc.Files[i].SubjectValidUTF8)
	}

	if !doc.Repaired {
		t.Error("Document.Repaired must record that it happened")
	}
	if doc.Files[0].SubjectValidUTF8 {
		t.Error("the repaired subject must be flagged, so no row claims its path is authoritative")
	}
	if !doc.Files[1].SubjectValidUTF8 {
		t.Error("a subject that came through untouched must not be flagged")
	}
	if !utf8.ValidString(doc.Files[0].Subject) {
		t.Error("the repaired subject must itself be valid UTF-8")
	}
	if !strings.ContainsRune(doc.Files[0].Subject, utf8.RuneError) {
		t.Errorf("the repair replaces the bad byte, got %q", doc.Files[0].Subject)
	}
}

func TestParseLatin1Declaration(t *testing.T) {
	raw := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>" +
		"<nzb><file subject=\"Caf\xe9.Release (1/1)\"><segments>" +
		"<segment number=\"1\">a@b</segment></segments></file></nzb>"
	t.Logf("input:  %q", raw)

	doc, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("a latin-1 declaration must be honoured: %v", err)
	}
	t.Logf("output: subject=%q repaired=%v valid=%v",
		doc.Files[0].Subject, doc.Repaired, doc.Files[0].SubjectValidUTF8)

	if want := "Café.Release (1/1)"; doc.Files[0].Subject != want {
		t.Errorf("got %q, want %q", doc.Files[0].Subject, want)
	}
	if doc.Repaired || !doc.Files[0].SubjectValidUTF8 {
		t.Error("a declared charset is a transcode, not a repair: nothing was guessed and nothing is flagged")
	}
}

func TestParseSegmentNumbers(t *testing.T) {
	raw := `<nzb><file subject="x"><segments>` +
		`<segment bytes="10" number="007">a@b</segment>` +
		`<segment bytes="20">b@b</segment>` +
		`<segment bytes="30" number="-4">c@b</segment>` +
		`<segment bytes="40" number="not a number">d@b</segment>` +
		`</segments></file></nzb>`
	t.Logf("input:  %s", raw)

	doc, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	want := []int{7, 0, -4, 0}
	for i, s := range doc.Files[0].Segments {
		t.Logf("output: segment %d number=%d id=%q bytes=%d", i, s.Number, s.MessageID, s.Bytes)

		if s.Number != want[i] {
			t.Errorf("segment %d: got number %d, want %d", i, s.Number, want[i])
		}
	}
}

func TestDocumentWithoutHeadKeepsEverythingElse(t *testing.T) {
	doc, err := Parse([]byte(sampleNZB))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	t.Logf("input:  a document with %d meta entries", len(doc.Meta))

	redacted := doc.WithoutHead()
	t.Logf("output: %d meta entries, %d files, password %q",
		len(redacted.Meta), len(redacted.Files), redacted.Password())

	if len(redacted.Meta) != 0 || redacted.Password() != "" {
		t.Error("WithoutHead must remove every meta entry")
	}
	if len(redacted.Files) != len(doc.Files) {
		t.Error("WithoutHead must not touch the files")
	}
	if len(doc.Meta) == 0 {
		t.Error("WithoutHead must not mutate its receiver")
	}
}

func TestFileSegmentAccounting(t *testing.T) {
	for _, c := range []struct {
		label      string
		file       File
		wantAll    bool
		wantMissed int
		wantMean   uint64
	}{
		{
			label:   "a complete 1..3 run",
			file:    File{PartsTotal: 3, Segments: segs(3, 1, 2, 3)},
			wantAll: true, wantMissed: 0, wantMean: 100,
		},
		{
			label:   "three segments, one of them a duplicate",
			file:    File{PartsTotal: 3, Segments: segs(3, 1, 2, 2)},
			wantAll: false, wantMissed: 0, wantMean: 100,
		},
		{
			label:   "two of a claimed three",
			file:    File{PartsTotal: 3, Segments: segs(2, 1, 2)},
			wantAll: false, wantMissed: 1, wantMean: 100,
		},
		{
			label:   "no counter at all, which is an obfuscated post",
			file:    File{PartsTotal: 0, Segments: segs(2, 1, 2)},
			wantAll: true, wantMissed: 0, wantMean: 100,
		},
		{
			label:   "a counter lower than the list, which real posters write",
			file:    File{PartsTotal: 2, Segments: segs(5, 1, 2, 3, 4, 5)},
			wantAll: false, wantMissed: 0, wantMean: 100,
		},
	} {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  partsTotal=%d segments=%d", c.file.PartsTotal, len(c.file.Segments))

			gotAll, gotMissed, gotMean := c.file.HasAllSegments(), c.file.MissingSegmentCount(), c.file.MeanSegmentBytes()
			t.Logf("output: hasAll=%v missing=%d mean=%d", gotAll, gotMissed, gotMean)

			if gotAll != c.wantAll || gotMissed != c.wantMissed || gotMean != c.wantMean {
				t.Errorf("got %v/%d/%d, want %v/%d/%d", gotAll, gotMissed, gotMean, c.wantAll, c.wantMissed, c.wantMean)
			}
		})
	}
}

// -- helpers -----------------------------------------------------------------

// segs builds n segments of 100 encoded bytes each, numbered as given.
func segs(n int, numbers ...int) []Segment {
	out := make([]Segment, 0, n)
	for i := 0; i < n; i++ {
		number := i + 1
		if i < len(numbers) {
			number = numbers[i]
		}
		out = append(out, Segment{MessageID: "a@b", Bytes: 100, Number: number})
	}

	return out
}
