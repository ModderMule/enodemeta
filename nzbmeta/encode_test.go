package nzbmeta

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestEncodeShape pins the bytes, because this is what another implementation
// reads and what a reviewer diffs.
func TestEncodeShape(t *testing.T) {
	doc := (&Builder{}).
		SetMeta(MetaName, "Some.Release.1080p").
		SetMeta(MetaCategory, "TV > HD").
		AddFile(File{
			Subject: `[1/1] - "Some.Release.part01.rar" yEnc (1/2)`,
			Poster:  "Poster <p@example.invalid>",
			Date:    time.Unix(1700000000, 0).UTC(),
			Groups:  []string{"alt.binaries.test"},
			Segments: []Segment{
				{MessageID: "<b2@example.invalid>", Bytes: 700001, Number: 2},
				{MessageID: "b1@example.invalid", Bytes: 700000, Number: 1},
			},
		}).
		Document()

	got := string(Encode(doc))
	t.Logf("output:\n%s", got)

	want := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head>
    <meta type="name">Some.Release.1080p</meta>
    <meta type="category">TV &gt; HD</meta>
  </head>
  <file poster="Poster &lt;p@example.invalid&gt;" date="1700000000" subject="[1/1] - &#34;Some.Release.part01.rar&#34; yEnc (1/2)">
    <groups>
      <group>alt.binaries.test</group>
    </groups>
    <segments>
      <segment bytes="700000" number="1">b1@example.invalid</segment>
      <segment bytes="700001" number="2">b2@example.invalid</segment>
    </segments>
  </file>
</nzb>
`

	if got != want {
		t.Errorf("encoded bytes:\n got %q\nwant %q", got, want)
	}
}

func TestEncodeComment(t *testing.T) {
	doc := (&Builder{}).AddFile(File{Subject: "x", Segments: []Segment{{MessageID: "a@h", Number: 1}}}).Document()

	got := string(Encoder{Comment: "usenet-crawler v0.1.0 -- do not --> nest"}.Encode(doc))
	t.Logf("output:\n%s", got)

	const want = "<!-- usenet-crawler v0.1.0 - - do not - -> nest -->\n<nzb"
	if !strings.Contains(got, want) {
		t.Errorf("the comment must sit before <nzb> with its double hyphens spaced out:\n got %q\nwant it to contain %q", got, want)
	}

	// A double hyphen cannot appear inside an XML comment and there is no escape
	// for it, so the only "--" left in the document is the pair that closes the
	// comment and the two in the DOCTYPE's public identifier.
	body := got[strings.Index(got, "<!--")+len("<!--") : strings.Index(got, "-->")]
	if strings.Contains(body, "--") {
		t.Errorf("a surviving double hyphen would make the document unparseable: %q", body)
	}

	// And it really is still parseable.
	if _, err := Parse([]byte(got)); err != nil {
		t.Errorf("a commented document must parse: %v", err)
	}
}

// TestRoundTrip is the pair of invariants the whole design rests on.
func TestRoundTrip(t *testing.T) {
	for _, c := range []struct {
		label string
		raw   string
	}{
		{"the sample document", sampleNZB},
		{"no head at all", `<nzb><file subject="x"><segments><segment number="1">a@h</segment></segments></file></nzb>`},
		{"no groups", `<nzb><file subject="x" poster="p"><segments><segment number="1">a@h</segment></segments></file></nzb>`},
		{"no poster and no date", `<nzb><file subject="x"><segments><segment number="1">a@h</segment></segments></file></nzb>`},
		{"zero bytes and no number", `<nzb><file subject="x"><segments><segment>a@h</segment></segments></file></nzb>`},
		{"a subject full of markup", `<nzb><file subject="a &amp; b &lt;c&gt; &#34;d&#34;"><segments><segment number="1">a@h</segment></segments></file></nzb>`},
		{"a file that will be dropped, beside one that will not", `<nzb>` +
			`<file subject="empty"><segments><segment number="1"></segment></segments></file>` +
			`<file subject="good"><segments><segment number="1">a@h</segment></segments></file></nzb>`},
		{"a latin-1 document", "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><nzb><file subject=\"Caf\xe9\"><segments><segment number=\"1\">a@h</segment></segments></file></nzb>"},
	} {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %.140q", c.raw)

			doc, err := Parse([]byte(c.raw))
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}

			wantIdentity, err := Identity(doc)
			if err != nil {
				t.Fatalf("identity: %v", err)
			}

			once := Encode(doc)

			reparsed, err := Parse(once)
			if err != nil {
				t.Fatalf("re-parsing our own output: %v", err)
			}

			gotIdentity, err := Identity(reparsed)
			if err != nil {
				t.Fatalf("identity of the re-parsed document: %v", err)
			}

			twice := Encode(reparsed)
			t.Logf("output: %d bytes, identity %x", len(once), gotIdentity)

			if gotIdentity != wantIdentity {
				t.Errorf("Identity(Parse(Encode(d))) != Identity(d):\n %x\n %x", gotIdentity, wantIdentity)
			}
			if !bytes.Equal(once, twice) {
				t.Errorf("Encode(Parse(Encode(d))) != Encode(d):\n%s\n---\n%s", once, twice)
			}
		})
	}
}

// TestRoundTripPathologicalMessageID names the one document shape outside both
// invariants, so that a future reader meets it here rather than in production.
//
// "<<a@h>>" normalises to "<a@h>", which is not a message-id: RFC 5322 forbids an
// angle bracket inside one. Encode writes it bare, as every real writer does, and
// the next read removes the brackets a second time. The alternative — writing
// "&lt;a@h&gt;" — would make the invariant hold for a string that can never be
// fetched, at the price of every other document carrying entity-escaped ids.
func TestRoundTripPathologicalMessageID(t *testing.T) {
	raw := `<nzb><file subject="x"><segments><segment number="1">&lt;&lt;a@h&gt;&gt;</segment></segments></file></nzb>`
	t.Logf("input:  %s", raw)

	doc, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	first := doc.Files[0].Segments[0].MessageID
	reparsed, err := Parse(Encode(doc))
	if err != nil {
		t.Fatalf("re-parsing: %v", err)
	}
	second := reparsed.Files[0].Segments[0].MessageID
	t.Logf("output: read once %q, read twice %q", first, second)

	if first != "<a@h>" || second != "a@h" {
		t.Errorf("got %q then %q, want %q then %q", first, second, "<a@h>", "a@h")
	}
}

func TestCanonicalOrder(t *testing.T) {
	// Deliberately shuffled, and with two files whose subjects are equal so the
	// message-id tie-break is exercised.
	in := []File{
		{Index: 9, Subject: "b", Segments: []Segment{{MessageID: "m", Number: 2}, {MessageID: "l", Number: 1}}},
		{Index: 3, Subject: "a", Segments: []Segment{{MessageID: "z", Number: 1}}},
		{Index: 7, Subject: "a", Segments: []Segment{{MessageID: "y", Number: 1}}},
	}
	for _, f := range in {
		t.Logf("input:  index=%d subject=%q lowest=%q", f.Index, f.Subject, lowestMessageID(f))
	}

	out := CanonicalOrder(in)
	for _, f := range out {
		t.Logf("output: index=%d subject=%q lowest=%q numbers=%d,%d",
			f.Index, f.Subject, lowestMessageID(f), f.Segments[0].Number, f.Segments[len(f.Segments)-1].Number)
	}

	if out[0].Subject != "a" || lowestMessageID(out[0]) != "y" {
		t.Errorf("equal subjects tie-break on the lowest message-id, got %q/%q", out[0].Subject, lowestMessageID(out[0]))
	}
	if out[1].Subject != "a" || out[2].Subject != "b" {
		t.Errorf("subjects must be ascending, got %q %q %q", out[0].Subject, out[1].Subject, out[2].Subject)
	}
	for i := range out {
		if out[i].Index != i {
			t.Errorf("Index must be renumbered to the position, got %d at %d", out[i].Index, i)
		}
	}
	if out[2].Segments[0].Number != 1 {
		t.Errorf("segments must end up ascending by number, got %d first", out[2].Segments[0].Number)
	}

	// The input is an input.
	if in[0].Index != 9 || in[0].Segments[0].Number != 2 {
		t.Error("CanonicalOrder must not mutate its argument")
	}
}

// TestBuilderIsOrderIndifferent is the property the acquisition path needs: a
// release assembled twice from the same articles, in any arrival order, must
// produce the same bytes and therefore the same id.
func TestBuilderIsOrderIndifferent(t *testing.T) {
	files := []File{
		{Subject: "Some.Release.part02.rar (1/1)", Segments: []Segment{{MessageID: "b@h", Bytes: 2, Number: 1}}},
		{Subject: "Some.Release.part01.rar (1/2)", Segments: []Segment{{MessageID: "a2@h", Bytes: 1, Number: 2}, {MessageID: "a1@h", Bytes: 1, Number: 1}}},
		{Subject: "Some.Release.nfo (1/1)", Segments: []Segment{{MessageID: "n@h", Bytes: 3, Number: 1}}},
	}

	forward := &Builder{}
	for _, f := range files {
		forward.AddFile(f)
	}

	backward := &Builder{}
	for i := len(files) - 1; i >= 0; i-- {
		backward.AddFile(files[i])
	}

	a, b := Encode(forward.Document()), Encode(backward.Document())
	t.Logf("input:  three files added in both orders\noutput: %d and %d bytes", len(a), len(b))

	if !bytes.Equal(a, b) {
		t.Errorf("arrival order must not change the bytes:\n%s\n---\n%s", a, b)
	}

	identity, err := Identity(forward.Document())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	t.Logf("output: identity %x", identity)
}

func TestBuilderSetMeta(t *testing.T) {
	b := (&Builder{}).
		SetMeta(MetaName, "first").
		SetMeta(MetaPassword, "p").
		SetMeta("NAME", "second").
		SetMeta(MetaPassword, "").
		SetMeta("  ", "ignored").
		AddFile(File{Subject: "x", Segments: []Segment{{MessageID: "a@h", Number: 1}}})

	doc := b.Document()
	t.Logf("output: meta=%v name=%q password=%q", doc.Meta, doc.Name(), doc.Password())

	if len(doc.Meta) != 1 || doc.Name() != "second" || doc.Password() != "" {
		t.Errorf("setting a type twice replaces it and an empty value removes it, got %v", doc.Meta)
	}
}

func TestBuilderDropsAFileWithNoUsableSegment(t *testing.T) {
	b := (&Builder{}).
		AddFile(File{Subject: "nothing", Segments: []Segment{{MessageID: "  ", Number: 1}, {MessageID: "<>", Number: 2}}}).
		AddFile(File{Subject: "something", Segments: []Segment{{MessageID: "<a@h>", Number: 1}}})

	doc := b.Document()
	t.Logf("output: %d file(s), first id %q", len(doc.Files), doc.Files[0].Segments[0].MessageID)

	if len(doc.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(doc.Files))
	}
	if doc.Files[0].Segments[0].MessageID != "a@h" {
		t.Errorf("AddFile must normalise the id exactly once, got %q", doc.Files[0].Segments[0].MessageID)
	}
}

func TestBuilderRunsTheSubjectHeuristics(t *testing.T) {
	doc := (&Builder{}).AddFile(File{
		Subject:    `[1/9] - "Some.Release.vol000+02.par2" yEnc (2/181)`,
		FileName:   "a lie the caller passed in",
		PartsTotal: 9999,
		Segments:   []Segment{{MessageID: "a@h", Number: 1}},
	}).Document()

	f := doc.Files[0]
	t.Logf("output: name=%q parts=%d par2=%v blocks=%d", f.FileName, f.PartsTotal, f.IsPAR2(), f.RecoveryBlocks())

	if f.FileName != "Some.Release.vol000+02.par2" || f.PartsTotal != 181 {
		t.Errorf("the heuristics are re-run, not trusted: got name=%q parts=%d", f.FileName, f.PartsTotal)
	}
	if !f.IsPAR2Volume() || f.RecoveryBlocks() != 2 {
		t.Errorf("got par2Volume=%v blocks=%d", f.IsPAR2Volume(), f.RecoveryBlocks())
	}
}
