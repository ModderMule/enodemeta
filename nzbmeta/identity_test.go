package nzbmeta

import (
	"errors"
	"strings"
	"testing"

	"github.com/ModderMule/enodemeta/metahash"
)

// TestIdentityPreimageRules takes the eleven rules one at a time, and asserts on
// the pre-image rather than on the digest: a port that gets the digest wrong
// learns nothing from a digest mismatch and everything from the byte the
// pre-image differs at.
func TestIdentityPreimageRules(t *testing.T) {
	for _, c := range []struct {
		label string
		raw   string
		want  string
	}{
		{
			label: "rule 1: files in document order",
			raw: `<nzb>` +
				`<file subject="zzz"><segments><segment number="1">z@h</segment></segments></file>` +
				`<file subject="aaa"><segments><segment number="1">a@h</segment></segments></file>` +
				`</nzb>`,
			want: "nzb1\n1:z@h\n1:a@h\n",
		},
		{
			label: "rule 2: segments sorted ascending by number, whatever the XML order",
			raw: `<nzb><file subject="x"><segments>` +
				`<segment number="3">c@h</segment>` +
				`<segment number="1">a@h</segment>` +
				`<segment number="2">b@h</segment>` +
				`</segments></file></nzb>`,
			want: "nzb1\n1:a@h\n2:b@h\n3:c@h\n",
		},
		{
			label: "rule 2: a duplicate number keeps document order, stably",
			raw: `<nzb><file subject="x"><segments>` +
				`<segment number="2">second@h</segment>` +
				`<segment number="2">first@h</segment>` +
				`<segment number="1">zero@h</segment>` +
				`</segments></file></nzb>`,
			want: "nzb1\n1:zero@h\n2:second@h\n2:first@h\n",
		},
		{
			label: "rule 3: a padded number is canonical base-10",
			raw:   `<nzb><file subject="x"><segments><segment number="007">a@h</segment></segments></file></nzb>`,
			want:  "nzb1\n7:a@h\n",
		},
		{
			label: "rule 4: an absent, negative or unparseable number is 0 and still emits a line",
			raw: `<nzb><file subject="x"><segments>` +
				`<segment>none@h</segment>` +
				`<segment number="-3">neg@h</segment>` +
				`<segment number="xx">bad@h</segment>` +
				`</segments></file></nzb>`,
			want: "nzb1\n0:none@h\n0:neg@h\n0:bad@h\n",
		},
		{
			label: "rule 5: entities resolved, whitespace trimmed, one bracket off each end",
			raw: `<nzb><file subject="x"><segments>` +
				`<segment number="1">  &lt;a&amp;b@h&gt;  </segment>` +
				`<segment number="2">bare@h</segment>` +
				`</segments></file></nzb>`,
			want: "nzb1\n1:a&b@h\n2:bare@h\n",
		},
		{
			label: "rule 6: an empty message-id emits no line, so a file of them contributes nothing",
			raw: `<nzb>` +
				`<file subject="all empty"><segments><segment number="1"></segment><segment number="2">  </segment></segments></file>` +
				`<file subject="good"><segments><segment number="1">a@h</segment></segments></file>` +
				`</nzb>`,
			want: "nzb1\n1:a@h\n",
		},
		{
			label: "rule 8: head, groups, poster, date, subject and bytes are all excluded",
			raw: `<nzb><head><meta type="password">s3cret</meta><meta type="name">N</meta></head>` +
				`<file poster="someone" date="1700000000" subject="a long descriptive subject (1/1)">` +
				`<groups><group>alt.binaries.one</group><group>alt.binaries.two</group></groups>` +
				`<segments><segment bytes="999999" number="1">a@h</segment></segments></file></nzb>`,
			want: "nzb1\n1:a@h\n",
		},
	} {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %s", c.raw)

			doc, err := Parse([]byte(c.raw))
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}

			preimage, err := IdentityPreimage(doc)
			if err != nil {
				t.Fatalf("pre-image: %v", err)
			}
			t.Logf("output: %q", string(preimage))

			if string(preimage) != c.want {
				t.Errorf("pre-image:\n got %q\nwant %q", string(preimage), c.want)
			}
		})
	}
}

// TestIdentityIsIndifferentToEverythingButTheArticles is rules 8 and 11 stated as
// the property that actually matters: a repost of the same articles under another
// group, poster and subject is deliberately the same release.
func TestIdentityIsIndifferentToEverythingButTheArticles(t *testing.T) {
	original := `<nzb><head><meta type="name">Original</meta></head>` +
		`<file poster="first@invalid" date="1700000000" subject="Some.Release.part01.rar (1/2)">` +
		`<groups><group>alt.binaries.one</group></groups>` +
		`<segments><segment bytes="100" number="1">a@h</segment><segment bytes="100" number="2">b@h</segment></segments>` +
		`</file></nzb>`

	repost := `<nzb><head><meta type="name">Repost</meta><meta type="password">p</meta></head>` +
		`<file poster="second@invalid" date="1800000000" subject="different subject entirely">` +
		`<groups><group>alt.binaries.two</group><group>alt.binaries.three</group></groups>` +
		`<segments><segment bytes="7" number="2">b@h</segment><segment bytes="7" number="1">a@h</segment></segments>` +
		`</file></nzb>`

	t.Logf("input:  two documents listing the same two articles")

	a, err := IdentityOf([]byte(original))
	if err != nil {
		t.Fatalf("original: %v", err)
	}
	b, err := IdentityOf([]byte(repost))
	if err != nil {
		t.Fatalf("repost: %v", err)
	}
	t.Logf("output: %x and %x", a, b)

	if a != b {
		t.Errorf("a repost of the same articles must be the same identity:\n %x\n %x", a, b)
	}
}

func TestIdentityChangesWithTheArticles(t *testing.T) {
	base := `<nzb><file subject="x"><segments><segment number="1">a@h</segment><segment number="2">b@h</segment></segments></file></nzb>`

	for _, c := range []struct {
		label string
		raw   string
	}{
		{"a different message-id", strings.Replace(base, "b@h", "c@h", 1)},
		{"a different part number", strings.Replace(base, `number="2"`, `number="3"`, 1)},
		{"one article fewer", `<nzb><file subject="x"><segments><segment number="1">a@h</segment></segments></file></nzb>`},
	} {
		t.Run(c.label, func(t *testing.T) {
			want, err := IdentityOf([]byte(base))
			if err != nil {
				t.Fatalf("base: %v", err)
			}

			got, err := IdentityOf([]byte(c.raw))
			if err != nil {
				t.Fatalf("variant: %v", err)
			}
			t.Logf("input:  %s\noutput: %x vs the base's %x", c.raw, got, want)

			if got == want {
				t.Errorf("%s must move the identity", c.label)
			}
		})
	}
}

// TestIdentitySameNumbersDifferentGrouping states the flip side, because it is
// the surprising half: a pre-image line carries no file boundary, so two
// documents that split the same articles into files differently mint the same
// id. That is what makes an identity survive a re-assembly that grouped the
// files another way, and it is why the digest is not a substitute for comparing
// file lists.
func TestIdentitySameNumbersDifferentGrouping(t *testing.T) {
	oneFile := `<nzb><file subject="x"><segments>` +
		`<segment number="1">a@h</segment><segment number="1">b@h</segment>` +
		`</segments></file></nzb>`
	twoFiles := `<nzb>` +
		`<file subject="x"><segments><segment number="1">a@h</segment></segments></file>` +
		`<file subject="y"><segments><segment number="1">b@h</segment></segments></file></nzb>`

	a, err := IdentityOf([]byte(oneFile))
	if err != nil {
		t.Fatalf("one file: %v", err)
	}
	b, err := IdentityOf([]byte(twoFiles))
	if err != nil {
		t.Fatalf("two files: %v", err)
	}
	t.Logf("input:  the same two articles as one file and as two\noutput: %x and %x", a, b)

	if a != b {
		t.Error("the pre-image carries no file boundary, so these must agree")
	}
}

func TestIdentityRefusesADocumentWithNoArticle(t *testing.T) {
	doc := &Document{Files: []File{{Subject: "x", Segments: []Segment{{Number: 1}}}}}
	t.Logf("input:  a hand-built document whose only segment has no message-id")

	_, err := Identity(doc)
	t.Logf("output: %v", err)

	if !errors.Is(err, ErrNoSegments) {
		t.Errorf("got %v, want it to wrap %v", err, ErrNoSegments)
	}
}

func TestNormalizeMessageID(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"a@h", "a@h"},
		{"<a@h>", "a@h"},
		{"  <a@h>  ", "a@h"},
		{"<a@h", "a@h"},
		{"a@h>", "a@h"},
		{"", ""},
		{"   ", ""},
		{"<>", ""},
		// Not idempotent, and deliberately so: this is eMuleQt's function, and
		// both sides have to remove the brackets the same number of times or the
		// digest moves.
		{"<<a@h>>", "<a@h>"},
	} {
		got := NormalizeMessageID(c.in)
		t.Logf("input:  %q  output: %q", c.in, got)

		if got != c.want {
			t.Errorf("NormalizeMessageID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCatalogIDIsUppercaseHex(t *testing.T) {
	digest, err := IdentityOf([]byte(sampleNZB))
	if err != nil {
		t.Fatalf("identity: %v", err)
	}

	id := CatalogID(digest)
	t.Logf("input:  %x\noutput: %s", digest, id)

	if len(id) != 64 {
		t.Fatalf("a catalog id is 64 hex characters, got %d", len(id))
	}
	if id != strings.ToUpper(id) {
		t.Errorf("uppercase everywhere: got %s", id)
	}
}

func TestMintInput(t *testing.T) {
	doc, err := Parse([]byte(sampleNZB))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	t.Logf("input:  %d files, password %q", len(doc.Files), doc.Password())

	in, err := doc.MintInput(metahash.FileIndexWholeSet32)
	if err != nil {
		t.Fatalf("mint input: %v", err)
	}
	t.Logf("output: kind=%s files=%d protected=%v", in.Kind, in.FileCount, in.Protected)

	if in.Kind != metahash.KindNZB || in.FileCount != 2 || !in.Protected {
		t.Errorf("got kind=%s count=%d protected=%v", in.Kind, in.FileCount, in.Protected)
	}

	hash, err := metahash.Mint(in)
	if err != nil {
		t.Fatalf("minting: %v", err)
	}

	parsed, err := metahash.Parse(hash[:])
	if err != nil {
		t.Fatalf("parsing the hash: %v", err)
	}
	t.Logf("output: %s flags=%s wholeSet=%v", hash, parsed.Flags, parsed.WholeSet())

	// An NZB's file order is not fixed across generators, so the path is always
	// authoritative — Mint decides that, not the caller.
	if parsed.Flags&metahash.FlagPathAuthoritative == 0 {
		t.Error("every nzb row must be path-authoritative")
	}
	if parsed.Flags&metahash.FlagProtected == 0 {
		t.Error("a document with a password must mint as protected")
	}
	if !parsed.WholeSet() {
		t.Error("FileIndexWholeSet32 must mint the whole-set row")
	}
}
