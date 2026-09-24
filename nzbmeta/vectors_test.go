package nzbmeta

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ModderMule/enodemeta/metahash"
)

// vectorsPath is deliberately outside this package's own testdata directory: the
// file is a cross-repository artifact, not a fixture for these tests. eMuleQt's
// C++ port is written from docs/nzb-identity.md and checked against this file,
// and it — not any one implementation — is what the three sides agree on.
const vectorsPath = "../testdata/nzb-identity-vectors.json"

var updateVectors = flag.Bool("update", false, "rewrite the NZB identity vectors file")

// vectorFile is the on-disk shape. Field names are camelCase because a C++ port
// reads this with whatever JSON library it already has.
type vectorFile struct {
	Scheme string `json:"scheme"`
	Note   string `json:"note"`

	// IdentityPrefixText is the domain separator, spelled out so a port does not
	// have to infer it from a pre-image.
	IdentityPrefixText string `json:"identityPrefixText"`

	Identity     []identityVector `json:"identity"`
	SameIdentity []sameVector     `json:"sameIdentity"`
	Reject       []rejectVector   `json:"reject"`
}

// identityVector is one document and everything derivable from it.
//
// The pre-image is carried as text *and* as hex on purpose: a port that gets the
// digest wrong learns nothing from a digest mismatch and everything from the byte
// the pre-image differs at, and the text is what a human reads while the hex is
// what a test compares.
type identityVector struct {
	Label string `json:"label"`
	Note  string `json:"note,omitempty"`

	// Exactly one of NZB and NZBHex is set. NZBHex carries the documents that
	// are not valid UTF-8 and therefore cannot be a JSON string.
	NZB    string `json:"nzb,omitempty"`
	NZBHex string `json:"nzbHex,omitempty"`

	Files        int  `json:"files"`
	DroppedFiles int  `json:"droppedFiles"`
	Segments     int  `json:"segments"`
	Protected    bool `json:"protected"`
	Repaired     bool `json:"repaired"`

	PreimageText string `json:"preimageText"`
	PreimageHex  string `json:"preimageHex"`
	DigestHex    string `json:"digestHex"`
	Fold10Hex    string `json:"fold10Hex"`
	CatalogID    string `json:"catalogId"`

	// WholeSetMetaHashHex is empty for a single-file release, which has no
	// whole-set row: minting one would produce a second hash for the same thing.
	WholeSetMetaHashHex string   `json:"wholeSetMetaHashHex"`
	FileMetaHashHex     []string `json:"fileMetaHashHex"`
}

// sameVector is a pair of documents that must mint the same id.
type sameVector struct {
	Label     string `json:"label"`
	Note      string `json:"note"`
	A         string `json:"a"`
	B         string `json:"b"`
	DigestHex string `json:"digestHex"`
}

type rejectVector struct {
	Label  string `json:"label"`
	NZB    string `json:"nzb,omitempty"`
	NZBHex string `json:"nzbHex,omitempty"`
	Reason string `json:"reason"`
}

// TestVectors checks this implementation against the checked-in file, and
// rewrites it with -update.
//
// A diff here is a contract change: it means every implementation has to move
// together, which is the whole point of keeping the file rather than asserting
// against ourselves.
func TestVectors(t *testing.T) {
	want := buildVectorFile(t)

	if *updateVectors {
		writeVectors(t, want)
		t.Logf("output: rewrote %s with %d identity, %d same-identity and %d reject vectors",
			vectorsPath, len(want.Identity), len(want.SameIdentity), len(want.Reject))

		return
	}

	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatalf("reading the vectors (regenerate with: go test ./nzbmeta -update): %v", err)
	}

	var got vectorFile
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parsing the vectors: %v", err)
	}
	t.Logf("input:  %s with %d identity, %d same-identity and %d reject vectors",
		vectorsPath, len(got.Identity), len(got.SameIdentity), len(got.Reject))

	if got.IdentityPrefixText != IdentityPrefix {
		t.Errorf("identity prefix: got %q, want %q", got.IdentityPrefixText, IdentityPrefix)
	}

	checkIdentityVectors(t, got.Identity)
	checkSameVectors(t, got.SameIdentity)
	checkRejectVectors(t, got.Reject)

	// The serialised form is compared too, so a vector that was dropped or
	// reordered is a failure rather than a silent shrink.
	if diff := compareSerialised(t, got, want); diff != "" {
		t.Errorf("the vectors file no longer matches this implementation: %s\nregenerate deliberately with: go test ./nzbmeta -update", diff)
	}
}

// TestVectorsAgreeWithMetaHash ties this file to meta-hash-vectors.json's scheme,
// so the two identity files cannot drift: every hash here has to parse as a meta
// hash, carry the NZB kind, and verify against the digest the same row states.
func TestVectorsAgreeWithMetaHash(t *testing.T) {
	for _, v := range buildVectorFile(t).Identity {
		t.Run(v.Label, func(t *testing.T) {
			digest := mustHex(t, v.DigestHex)
			t.Logf("input:  %d files, digest %s", v.Files, v.DigestHex)

			hashes := append([]string{}, v.FileMetaHashHex...)
			if v.WholeSetMetaHashHex != "" {
				hashes = append(hashes, v.WholeSetMetaHashHex)
			}

			for _, spelling := range hashes {
				hash, err := metahash.ParseHex(spelling)
				if err != nil {
					t.Fatalf("%s: %v", spelling, err)
				}

				parsed, err := metahash.Parse(hash[:])
				if err != nil {
					t.Fatalf("%s: %v", spelling, err)
				}

				if parsed.Kind != metahash.KindNZB {
					t.Errorf("%s: kind %s, want nzb", spelling, parsed.Kind)
				}
				if parsed.Flags&metahash.FlagPathAuthoritative == 0 {
					t.Errorf("%s: every nzb row is path-authoritative", spelling)
				}
				if (v.Files > 1) != (parsed.Flags&metahash.FlagMultiFile != 0) {
					t.Errorf("%s: multi-file flag disagrees with a %d-file release", spelling, v.Files)
				}
				if v.Protected != (parsed.Flags&metahash.FlagProtected != 0) {
					t.Errorf("%s: protected flag disagrees with the document", spelling)
				}
				if !hash.VerifyIdentity(digest) {
					t.Errorf("%s: must fold to %s", spelling, v.DigestHex)
				}
			}

			// Every row of one release shares the ten digest bytes, which is what
			// lets a client group them before fetching anything.
			if v.WholeSetMetaHashHex != "" {
				whole, err := metahash.ParseHex(v.WholeSetMetaHashHex)
				if err != nil {
					t.Fatal(err)
				}
				for _, spelling := range v.FileMetaHashHex {
					one, err := metahash.ParseHex(spelling)
					if err != nil {
						t.Fatal(err)
					}
					if !whole.SameRelease(one) {
						t.Errorf("%s and %s must read as the same release", spelling, v.WholeSetMetaHashHex)
					}
				}
			}

			t.Logf("output: %d row hash(es) verified against the digest", len(hashes))
		})
	}
}

// -- internals ---------------------------------------------------------------

func checkIdentityVectors(t *testing.T, vectors []identityVector) {
	t.Helper()

	for _, v := range vectors {
		doc, err := Parse(vectorBytes(t, v.NZB, v.NZBHex))
		if err != nil {
			t.Errorf("identity %q: %v", v.Label, err)

			continue
		}

		preimage, err := IdentityPreimage(doc)
		if err != nil {
			t.Errorf("identity %q: %v", v.Label, err)

			continue
		}

		if got := string(preimage); got != v.PreimageText {
			t.Errorf("identity %q: pre-image\n got %q\nwant %q", v.Label, got, v.PreimageText)

			continue
		}

		digest, err := Identity(doc)
		if err != nil {
			t.Errorf("identity %q: %v", v.Label, err)

			continue
		}

		if got := upperHex(digest[:]); got != v.DigestHex {
			t.Errorf("identity %q: got %s, want %s", v.Label, got, v.DigestHex)

			continue
		}
		if got := CatalogID(digest); got != v.CatalogID {
			t.Errorf("identity %q: catalog id got %s, want %s", v.Label, got, v.CatalogID)
		}
		if len(doc.Files) != v.Files || doc.DroppedFiles != v.DroppedFiles || doc.SegmentCount() != v.Segments {
			t.Errorf("identity %q: files=%d dropped=%d segments=%d, want %d/%d/%d",
				v.Label, len(doc.Files), doc.DroppedFiles, doc.SegmentCount(), v.Files, v.DroppedFiles, v.Segments)
		}
		if doc.Protected() != v.Protected || doc.Repaired != v.Repaired {
			t.Errorf("identity %q: protected=%v repaired=%v, want %v/%v",
				v.Label, doc.Protected(), doc.Repaired, v.Protected, v.Repaired)
		}

		t.Logf("output: identity %-58q %s", v.Label, v.DigestHex)
	}
}

func checkSameVectors(t *testing.T, vectors []sameVector) {
	t.Helper()

	for _, v := range vectors {
		a, err := IdentityOf([]byte(v.A))
		if err != nil {
			t.Errorf("same %q: a: %v", v.Label, err)

			continue
		}
		b, err := IdentityOf([]byte(v.B))
		if err != nil {
			t.Errorf("same %q: b: %v", v.Label, err)

			continue
		}

		if a != b || upperHex(a[:]) != v.DigestHex {
			t.Errorf("same %q: got %x and %x, want both %s", v.Label, a, b, v.DigestHex)

			continue
		}

		t.Logf("output: same     %-58q %s", v.Label, v.DigestHex)
	}
}

func checkRejectVectors(t *testing.T, vectors []rejectVector) {
	t.Helper()

	for _, v := range vectors {
		if _, err := Parse(vectorBytes(t, v.NZB, v.NZBHex)); err == nil {
			t.Errorf("reject %q: must not parse", v.Label)

			continue
		}

		t.Logf("output: reject   %-58q %s", v.Label, v.Reason)
	}
}

// buildVectorFile generates the vectors this implementation produces.
//
// The documents are written out in full rather than assembled from a template:
// the file is read by a C++ port, and a reader has to be able to see which byte
// each case is about.
func buildVectorFile(t *testing.T) vectorFile {
	t.Helper()

	const (
		baseline = `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <file poster="p@example.invalid" date="1700000000" subject="[1/1] - &#34;rel.part01.rar&#34; yEnc (1/2)">
    <groups><group>alt.binaries.test</group></groups>
    <segments>
      <segment bytes="700000" number="1">a1@example.invalid</segment>
      <segment bytes="700001" number="2">a2@example.invalid</segment>
    </segments>
  </file>
</nzb>
`

		repost = `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <file poster="someone.else@example.invalid" date="1800000000" subject="a completely different subject">
    <groups><group>alt.binaries.elsewhere</group><group>alt.binaries.second</group></groups>
    <segments>
      <segment bytes="7" number="2">a2@example.invalid</segment>
      <segment bytes="7" number="1">a1@example.invalid</segment>
    </segments>
  </file>
</nzb>
`

		withPassword = `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head>
    <meta type="name">rel</meta>
    <meta type="password">hunter2</meta>
  </head>
  <file poster="p@example.invalid" date="1700000000" subject="[1/1] - &#34;rel.part01.rar&#34; yEnc (1/2)">
    <groups><group>alt.binaries.test</group></groups>
    <segments>
      <segment bytes="700000" number="1">a1@example.invalid</segment>
      <segment bytes="700001" number="2">a2@example.invalid</segment>
    </segments>
  </file>
</nzb>
`

		multiFile = `<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <file subject="[1/3] - &#34;rel.part01.rar&#34; yEnc (1/2)">
    <segments>
      <segment bytes="700000" number="1">a1@example.invalid</segment>
      <segment bytes="700000" number="2">a2@example.invalid</segment>
    </segments>
  </file>
  <file subject="[2/3] - &#34;rel.nfo&#34; yEnc (1/1)">
    <segments><segment bytes="2000" number="1">n1@example.invalid</segment></segments>
  </file>
  <file subject="[3/3] - &#34;rel.vol000+01.par2&#34; yEnc (1/1)">
    <segments><segment bytes="700000" number="1">v1@example.invalid</segment></segments>
  </file>
</nzb>
`
	)

	cases := []struct {
		label  string
		note   string
		nzb    string
		binary []byte
	}{
		{
			label: "the baseline: one file, two articles",
			note:  "Read this one first. Everything below is a variation on it, and most of them must produce this exact digest.",
			nzb:   baseline,
		},
		{
			label: "two files, taken in document order",
			note:  "The second file's articles follow the first's, whatever their numbers are.",
			nzb: `<nzb>` + "\n" +
				`  <file subject="second"><segments><segment number="1">z@example.invalid</segment></segments></file>` + "\n" +
				`  <file subject="first"><segments><segment number="1">a@example.invalid</segment></segments></file>` + "\n" +
				`</nzb>` + "\n",
		},
		{
			label: "segments shuffled in the XML",
			note:  "Rule 2: sorted ascending by @number, so the XML order does not matter. Same digest as the baseline.",
			nzb: strings.Replace(baseline,
				`      <segment bytes="700000" number="1">a1@example.invalid</segment>`+"\n"+
					`      <segment bytes="700001" number="2">a2@example.invalid</segment>`,
				`      <segment bytes="700001" number="2">a2@example.invalid</segment>`+"\n"+
					`      <segment bytes="700000" number="1">a1@example.invalid</segment>`, 1),
		},
		{
			label: "a padded @number, an absent one and a duplicate",
			note:  "Rules 2, 3 and 4: \"007\" is 7, an absent number is 0, and two segments claiming one number keep their document order.",
			nzb: `<nzb><file subject="x"><segments>` +
				`<segment number="007">g@example.invalid</segment>` +
				`<segment>none@example.invalid</segment>` +
				`<segment number="2">second@example.invalid</segment>` +
				`<segment number="2">first@example.invalid</segment>` +
				`</segments></file></nzb>` + "\n",
		},
		{
			label: "a bracketed id beside a bare one, and one carrying an entity",
			note:  "Rule 5: entities resolved, whitespace trimmed, one angle bracket off each end. All three ids are written differently and read the same way.",
			nzb: `<nzb><file subject="x"><segments>` +
				`<segment number="1">&lt;a@example.invalid&gt;</segment>` +
				`<segment number="2">   b@example.invalid   </segment>` +
				`<segment number="3">c&amp;d@example.invalid</segment>` +
				`</segments></file></nzb>` + "\n",
		},
		{
			label: "a zero-segment file beside a good one",
			note:  "Rule 6, the load-bearing one: the empty file contributes nothing, so a reader that drops it and one that keeps it agree. This implementation drops it, and reports it as droppedFiles.",
			nzb: `<nzb>` +
				`<file subject="nothing usable"><segments><segment number="1"></segment><segment number="2">   </segment></segments></file>` +
				`<file subject="x"><segments><segment number="1">a@example.invalid</segment></segments></file>` +
				`</nzb>` + "\n",
		},
		{
			label: "a head with a password, which does not move the digest",
			note:  "Rule 8, and the property it buys: a daemon may serve a copy with <head> stripped and the client's fold check still passes. Same digest as the baseline; the meta hashes differ only by the protected flag.",
			nzb:   withPassword,
		},
		{
			label: "a wrong namespace",
			note:  "Elements are matched on their local name. Same digest as the baseline.",
			nzb:   strings.Replace(baseline, Namespace, "http://example.invalid/not-nzb", 1),
		},
		{
			label: "no namespace at all",
			note:  "Same digest as the baseline.",
			nzb:   strings.Replace(baseline, ` xmlns="`+Namespace+`"`, "", 1),
		},
		{
			label: "a multi-file release, which has a whole-set row",
			note:  "Three files, so wholeSetMetaHashHex is present and every row shares the ten digest bytes. A single-file release has no whole-set row at all.",
			nzb:   multiFile,
		},
		{
			label: "an invalid-UTF-8 subject, repaired",
			note:  "The document claims UTF-8 and carries a latin-1 byte. It is repaired rather than refused, because Qt's codec substitutes where Go's decoder errors, and the bytes the digest covers are ASCII either way — so the digest is the baseline's.",
			binary: []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
				"<nzb xmlns=\"" + Namespace + "\">\n" +
				"  <file poster=\"p@example.invalid\" date=\"1700000000\" subject=\"Caf\xe9 [1/1] - &#34;rel.part01.rar&#34; yEnc (1/2)\">\n" +
				"    <groups><group>alt.binaries.test</group></groups>\n" +
				"    <segments>\n" +
				"      <segment bytes=\"700000\" number=\"1\">a1@example.invalid</segment>\n" +
				"      <segment bytes=\"700001\" number=\"2\">a2@example.invalid</segment>\n" +
				"    </segments>\n" +
				"  </file>\n" +
				"</nzb>\n"),
		},
		{
			label: "a latin-1 declaration, transcoded",
			note:  "The one non-UTF-8 charset this package reads. Nothing is flagged, because nothing was guessed.",
			binary: []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\n" +
				"<nzb><file subject=\"Caf\xe9.Release (1/1)\"><segments>" +
				"<segment bytes=\"1\" number=\"1\">a1@example.invalid</segment>" +
				"</segments></file></nzb>\n"),
		},
	}

	out := vectorFile{
		Scheme: "enode.meta.v1",
		Note: "Cross-repository test vectors for the canonical NZB identity digest (see " +
			"eNode-go docs/meta-search-torrent-usenet-plan.local.md section 3.4, and " +
			"usenet-crawler docs/nzb-identity.md, which is the document a port is written " +
			"from). Every implementation — eNode-go, eMuleQt and the crawler daemons — must " +
			"reproduce this file exactly. Each vector carries the pre-image as text and as " +
			"hex, because a digest mismatch says nothing and a pre-image mismatch says " +
			"everything. Exactly one of nzb and nzbHex is set per vector; nzbHex carries the " +
			"documents that are not valid UTF-8. Regenerate with: go test ./nzbmeta -update",
		IdentityPrefixText: IdentityPrefix,
	}

	for _, c := range cases {
		raw := c.binary
		if raw == nil {
			raw = []byte(c.nzb)
		}

		doc, err := Parse(raw)
		if err != nil {
			t.Fatalf("generating vector %q: %v", c.label, err)
		}

		preimage, err := IdentityPreimage(doc)
		if err != nil {
			t.Fatalf("generating vector %q: %v", c.label, err)
		}

		digest, err := Identity(doc)
		if err != nil {
			t.Fatalf("generating vector %q: %v", c.label, err)
		}

		fold := metahash.Fold10(digest[:])

		v := identityVector{
			Label:        c.label,
			Note:         c.note,
			Files:        len(doc.Files),
			DroppedFiles: doc.DroppedFiles,
			Segments:     doc.SegmentCount(),
			Protected:    doc.Protected(),
			Repaired:     doc.Repaired,
			PreimageText: string(preimage),
			PreimageHex:  upperHex(preimage),
			DigestHex:    upperHex(digest[:]),
			Fold10Hex:    upperHex(fold[:]),
			CatalogID:    CatalogID(digest),
		}

		if c.binary == nil {
			v.NZB = c.nzb
		} else {
			v.NZBHex = upperHex(c.binary)
		}

		for i := range doc.Files {
			v.FileMetaHashHex = append(v.FileMetaHashHex, mintHex(t, doc, uint32(doc.Files[i].Index)))
		}
		if len(doc.Files) > 1 {
			v.WholeSetMetaHashHex = mintHex(t, doc, metahash.FileIndexWholeSet32)
		}

		out.Identity = append(out.Identity, v)
	}

	baselineDigest, err := IdentityOf([]byte(baseline))
	if err != nil {
		t.Fatalf("generating the same-identity vectors: %v", err)
	}
	baselineHex := upperHex(baselineDigest[:])

	out.SameIdentity = []sameVector{
		{
			Label:     "a repost under a different group, poster and subject",
			Note:      "The whole point of rule 8. Two postings of the same articles are one release, which is what stops a re-post doubling a catalogue.",
			A:         baseline,
			B:         repost,
			DigestHex: baselineHex,
		},
		{
			Label:     "the same document with its head stripped",
			Note:      "What catalog.serve_password_meta relies on: redacting a password leaves the release verifiable.",
			A:         withPassword,
			B:         baseline,
			DigestHex: baselineHex,
		},
		{
			Label:     "the same articles grouped into one file or into two",
			Note:      "A pre-image line carries no file boundary. Surprising, and worth knowing before treating the digest as a file-list comparison.",
			A:         `<nzb><file subject="x"><segments><segment number="1">a@h</segment><segment number="1">b@h</segment></segments></file></nzb>`,
			B:         `<nzb><file subject="x"><segments><segment number="1">a@h</segment></segments></file><file subject="y"><segments><segment number="1">b@h</segment></segments></file></nzb>`,
			DigestHex: upperHex(digestOf(t, `<nzb><file subject="x"><segments><segment number="1">a@h</segment><segment number="1">b@h</segment></segments></file></nzb>`)),
		},
	}

	out.Reject = []rejectVector{
		{
			Label:  "empty",
			Reason: "a fetch that returned nothing is not a document",
		},
		{
			Label:  "an indexer's HTML error page served under a .nzb name",
			NZB:    "<html><head><title>404</title></head><body>Not found</body></html>\n",
			Reason: "it parses and lists no file; calling that an empty success puts a permanently stalled item in a queue",
		},
		{
			Label:  "truncated mid-element",
			NZB:    `<nzb><file subject="x"><segments><segment number="1">a@example.invalid`,
			Reason: "not well-formed; a partial fetch must not become a partial release",
		},
		{
			Label:  "a document whose every segment has an empty message-id",
			NZB:    `<nzb><file subject="x"><segments><segment number="1"></segment></segments></file></nzb>` + "\n",
			Reason: "every file is dropped by rule 6, so nothing is left to identify",
		},
		{
			Label:  "a well-formed document with a head and no files",
			NZB:    `<nzb><head><meta type="name">nothing</meta></head></nzb>` + "\n",
			Reason: "a release with no articles has no identity",
		},
	}

	return out
}

func mintHex(t *testing.T, doc *Document, fileIndex uint32) string {
	t.Helper()

	in, err := doc.MintInput(fileIndex)
	if err != nil {
		t.Fatalf("mint input: %v", err)
	}

	hash, err := metahash.Mint(in)
	if err != nil {
		t.Fatalf("minting index %d: %v", fileIndex, err)
	}

	return hash.String()
}

func digestOf(t *testing.T, raw string) []byte {
	t.Helper()

	digest, err := IdentityOf([]byte(raw))
	if err != nil {
		t.Fatalf("digest of %q: %v", raw, err)
	}

	return digest[:]
}

func vectorBytes(t *testing.T, text, spelling string) []byte {
	t.Helper()

	if spelling == "" {
		return []byte(text)
	}
	if text != "" {
		t.Fatalf("a vector sets nzb or nzbHex, never both")
	}

	return mustHex(t, spelling)
}

func writeVectors(t *testing.T, v vectorFile) {
	t.Helper()

	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("encoding the vectors: %v", err)
	}
	raw = append(raw, '\n')

	if err := os.MkdirAll(filepath.Dir(vectorsPath), 0o755); err != nil {
		t.Fatalf("creating the vectors directory: %v", err)
	}
	if err := os.WriteFile(vectorsPath, raw, 0o644); err != nil {
		t.Fatalf("writing the vectors: %v", err)
	}
}

// compareSerialised reports the first difference between the stored file and a
// freshly generated one.
func compareSerialised(t *testing.T, got, want vectorFile) string {
	t.Helper()

	gotRaw, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("encoding the stored vectors: %v", err)
	}
	wantRaw, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatalf("encoding the generated vectors: %v", err)
	}

	if bytes.Equal(gotRaw, wantRaw) {
		return ""
	}

	gotLines := strings.Split(string(gotRaw), "\n")
	wantLines := strings.Split(string(wantRaw), "\n")

	for i := 0; i < len(gotLines) && i < len(wantLines); i++ {
		if gotLines[i] != wantLines[i] {
			return fmt.Sprintf("line %d:\n stored: %s\n  fresh: %s", i+1, gotLines[i], wantLines[i])
		}
	}

	return fmt.Sprintf("the stored file has %d lines, a fresh one has %d", len(gotLines), len(wantLines))
}

func upperHex(b []byte) string {
	return strings.ToUpper(hex.EncodeToString(b))
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()

	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("vector hex %q: %v", s, err)
	}

	return raw
}
