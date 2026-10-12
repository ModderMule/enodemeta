package weblinks

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/ModderMule/enodemeta/metahash"
)

// vectorsPath is outside this package's own testdata directory for the reason
// nzbmeta's is: the file is what eNode-go, eMuleQt and the daemons agree on, not
// a fixture for these tests.
const vectorsPath = "../testdata/web-identity-vectors.json"

var updateVectors = flag.Bool("update", false, "rewrite the web identity vectors file")

// vectorFile is the on-disk shape, camelCase because a C++ port reads it.
type vectorFile struct {
	Scheme             string `json:"scheme"`
	Note               string `json:"note"`
	IdentityPrefixText string `json:"identityPrefixText"`

	Canonical    []canonicalVector `json:"canonical"`
	Identity     []identityVector  `json:"identity"`
	SameIdentity []sameVector      `json:"sameIdentity"`
	Reject       []rejectVector    `json:"reject"`
}

type canonicalVector struct {
	Label     string `json:"label"`
	Link      string `json:"link"`
	Canonical string `json:"canonical"`
}

// identityVector is one container and everything derivable from it. The
// pre-image is carried as text and as hex: the text is what a person reads and
// the hex is what a test compares.
type identityVector struct {
	Label     string `json:"label"`
	Note      string `json:"note,omitempty"`
	Container string `json:"container"`

	Links     int  `json:"links"`
	Files     int  `json:"files"`
	Protected bool `json:"protected"`

	PreimageText string `json:"preimageText"`
	PreimageHex  string `json:"preimageHex"`
	DigestHex    string `json:"digestHex"`
	Fold10Hex    string `json:"fold10Hex"`
	CatalogID    string `json:"catalogId"`

	// WholeSetMetaHashHex is empty for a release of one file, which has no
	// whole-set row. FileMetaHashHex has one hash per file, in file order.
	WholeSetMetaHashHex string   `json:"wholeSetMetaHashHex"`
	FileMetaHashHex     []string `json:"fileMetaHashHex"`
}

// sameVector is a pair of containers that must have one identity.
type sameVector struct {
	Label     string `json:"label"`
	Note      string `json:"note"`
	A         string `json:"a"`
	B         string `json:"b"`
	DigestHex string `json:"digestHex"`
}

type rejectVector struct {
	Label     string `json:"label"`
	Container string `json:"container"`
	Reason    string `json:"reason"`
}

// TestVectors checks this implementation against the checked-in file, and
// rewrites it with -update. A diff here is a contract change.
func TestVectors(t *testing.T) {
	want := buildVectorFile(t)

	if *updateVectors {
		raw, err := json.MarshalIndent(want, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorsPath, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("output: rewrote %s with %d canonical, %d identity, %d same-identity and %d reject vectors",
			vectorsPath, len(want.Canonical), len(want.Identity), len(want.SameIdentity), len(want.Reject))

		return
	}

	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatalf("reading the vectors (regenerate with: go test ./weblinks -update): %v", err)
	}

	var got vectorFile
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parsing the vectors: %v", err)
	}
	t.Logf("input:  %s with %d canonical, %d identity, %d same-identity and %d reject vectors",
		vectorsPath, len(got.Canonical), len(got.Identity), len(got.SameIdentity), len(got.Reject))

	for _, v := range got.Canonical {
		if c, err := Canonical(v.Link); err != nil || c != v.Canonical {
			t.Errorf("%s: %q gave %q (%v), want %q", v.Label, v.Link, c, err, v.Canonical)
		}
	}

	for _, v := range got.Identity {
		checkIdentityVector(t, v)
	}

	for _, v := range got.SameIdentity {
		a, errA := IdentityOf([]byte(v.A))
		b, errB := IdentityOf([]byte(v.B))
		if errA != nil || errB != nil {
			t.Errorf("%s: %v, %v", v.Label, errA, errB)

			continue
		}
		if a != b || hex.EncodeToString(a[:]) != v.DigestHex {
			t.Errorf("%s: got %x and %x, want both %s", v.Label, a, b, v.DigestHex)
		}
	}

	for _, v := range got.Reject {
		if _, err := IdentityOf([]byte(v.Container)); err == nil {
			t.Errorf("%s: accepted, want it refused (%s)", v.Label, v.Reason)
		}
	}

	// The serialised form is compared too, so a vector that was dropped or
	// reordered fails rather than shrinking the file quietly.
	wantRaw, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(wantRaw)) {
		t.Errorf("the vectors file no longer matches this implementation\nregenerate deliberately with: go test ./weblinks -update")
	}

	t.Logf("output: every vector reproduced")
}

// -- internals ---------------------------------------------------------------

func checkIdentityVector(t *testing.T, v identityVector) {
	t.Helper()

	doc, err := Parse([]byte(v.Container))
	if err != nil {
		t.Errorf("%s: %v", v.Label, err)

		return
	}

	preimage, err := IdentityPreimage(doc)
	if err != nil {
		t.Errorf("%s: %v", v.Label, err)

		return
	}
	if string(preimage) != v.PreimageText || hex.EncodeToString(preimage) != v.PreimageHex {
		t.Errorf("%s: pre-image\n got %q\nwant %q", v.Label, preimage, v.PreimageText)
	}

	digest, _ := Identity(doc)
	if hex.EncodeToString(digest[:]) != v.DigestHex {
		t.Errorf("%s: digest %x, want %s", v.Label, digest, v.DigestHex)
	}

	// Every hash in the file has to parse as a web meta hash and fold to the
	// digest beside it, which ties this file to meta-hash-vectors.json's scheme.
	for _, spelling := range append(append([]string{}, v.FileMetaHashHex...), v.WholeSetMetaHashHex) {
		if spelling == "" {
			continue
		}

		hash, err := metahash.ParseHex(spelling)
		if err != nil {
			t.Errorf("%s: %s: %v", v.Label, spelling, err)

			continue
		}

		parsed, err := metahash.Parse(hash[:])
		if err != nil || parsed.Kind != metahash.KindWeb || !hash.VerifyIdentity(digest[:]) {
			t.Errorf("%s: %s is not a web hash of this digest (%v)", v.Label, spelling, err)
		}
	}
}

func buildVectorFile(t *testing.T) vectorFile {
	t.Helper()

	const baseline = "#title: Some Release 1080p\r\n" +
		"#file: 0\r\n#name: some.release.mkv\r\n#size: 1073741824\r\n#hoster: examplehost\r\n" +
		"https://examplehost.test/f/abc123\r\n"

	const twoParts = "#title: Some Release 1080p\r\n" +
		"#file: 0\r\n#name: some.release.part1.rar\r\n#hoster: examplehost\r\nhttps://examplehost.test/f/abc123\r\n" +
		"#file: 0\r\n#hoster: otherhost\r\nhttps://otherhost.test/d/xyz789\r\n" +
		"#file: 1\r\n#name: some.release.part2.rar\r\n#hoster: examplehost\r\nhttps://examplehost.test/f/def456\r\n"

	file := vectorFile{
		Scheme: "enode.meta.v1",
		Note: "Cross-repository test vectors for the canonical web link-set digest (enodemeta weblinks/identity.go " +
			"holds the rules, and docs/ingest-contract.md amendment 24 the reasons). Every implementation — eNode-go, " +
			"eMuleQt and the crawler daemons — must reproduce this file exactly. Regenerate with: go test ./weblinks -update",
		IdentityPrefixText: IdentityPrefix,
	}

	for _, c := range []struct{ label, link string }{
		{"scheme and host are lowered, the path is not", "HTTPS://Host.TEST/F/Abc"},
		{"a fragment is dropped", "https://host.test/f/abc#player"},
		{"the default https port is dropped", "https://host.test:443/f"},
		{"the default http port is dropped", "http://host.test:80/f"},
		{"another port stays", "https://host.test:8443/f"},
		{"an empty port is dropped", "https://host.test:/f"},
		{"no path becomes a slash", "https://host.test"},
		{"a query with no path gets one", "https://host.test?id=1"},
		{"percent escapes are kept as written", "https://host.test/a%2fb%2Fc"},
		{"the query is not reordered", "https://host.test/?b=2&a=1"},
		{"an IPv6 literal loses a default port and keeps its colons", "http://[2001:DB8::1]:80/f"},
	} {
		canonical, err := Canonical(c.link)
		if err != nil {
			t.Fatalf("%s: %v", c.label, err)
		}

		file.Canonical = append(file.Canonical, canonicalVector{Label: c.label, Link: c.link, Canonical: canonical})
	}

	for _, c := range []struct{ label, note, container string }{
		{"the baseline: one file, one link", "Read this one first.", baseline},
		{"two files, one of them mirrored", "Three links and two files: a mirror is not a file.", twoParts},
		{"a protected release", "The protected hint is outside the identity and inside the hash's flag nibble.",
			"#protected: 1\r\n" + twoParts},
		{"bare links, no hints", "Every link is file 0, so this is one file with two mirrors.",
			"https://examplehost.test/f/abc123\nhttps://otherhost.test/d/xyz789\n"},
	} {
		file.Identity = append(file.Identity, buildIdentityVector(t, c.label, c.note, c.container))
	}

	for _, c := range []struct{ label, note, a, b string }{
		{"hints are outside the identity", "A size learned later does not make another release.",
			baseline, "https://examplehost.test/f/abc123\n"},
		{"order does not matter", "Links are sorted before they are digested.",
			"https://a.test/1\nhttps://b.test/2\n", "https://b.test/2\nhttps://a.test/1\n"},
		{"a repeated link counts once", "", "https://a.test/1\nhttps://a.test/1\n", "https://a.test/1\n"},
		{"line endings do not matter", "", "https://a.test/1\r\nhttps://b.test/2\r\n", "https://a.test/1\nhttps://b.test/2"},
		{"spellings of one link", "Case of scheme and host, a default port and a fragment.",
			"HTTPS://A.test:443/1#top\n", "https://a.test/1\n"},
	} {
		digest, err := IdentityOf([]byte(c.a))
		if err != nil {
			t.Fatalf("%s: %v", c.label, err)
		}

		file.SameIdentity = append(file.SameIdentity, sameVector{
			Label: c.label, Note: c.note, A: c.a, B: c.b, DigestHex: hex.EncodeToString(digest[:]),
		})
	}

	file.Reject = []rejectVector{
		{"empty", "", "no link"},
		{"only comments", "#title: nothing\r\n", "no link"},
		{"an html error page", "<html><body>404</body></html>\n", "a line that is neither comment nor link"},
		{"an ftp link", "ftp://host.test/f\n", "scheme"},
		{"a magnet among links", "https://host.test/f\nmagnet:?xt=urn:btih:0102030405060708090A0B0C0D0E0F1011121314\n", "scheme"},
		{"credentials", "https://user:pass@host.test/f\n", "a link with credentials"},
		{"a space inside a link", "https://host.test/a b\n", "a byte at or below 0x20"},
	}

	return file
}

func buildIdentityVector(t *testing.T, label, note, container string) identityVector {
	t.Helper()

	doc, err := Parse([]byte(container))
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}

	preimage, err := IdentityPreimage(doc)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}

	digest, _ := Identity(doc)
	fold := metahash.Fold10(digest[:])

	v := identityVector{
		Label: label, Note: note, Container: container,
		Links: len(doc.Links), Files: int(doc.FileCount()), Protected: doc.Protected,
		PreimageText: string(preimage), PreimageHex: hex.EncodeToString(preimage),
		DigestHex: hex.EncodeToString(digest[:]), Fold10Hex: hex.EncodeToString(fold[:]),
		CatalogID: strings.ToUpper(hex.EncodeToString(digest[:])),
	}

	mint := func(index uint32) string {
		in, err := doc.MintInput(index)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}

		hash, err := metahash.Mint(in)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}

		return hash.String()
	}

	for index := uint32(0); index < doc.FileCount(); index++ {
		v.FileMetaHashHex = append(v.FileMetaHashHex, mint(index))
	}
	if doc.FileCount() > 1 {
		v.WholeSetMetaHashHex = mint(metahash.FileIndexWholeSet32)
	}

	return v
}
