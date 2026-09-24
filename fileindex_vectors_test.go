package enodemeta

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

	"github.com/ModderMule/enodemeta/bencode"
	"github.com/ModderMule/enodemeta/metahash"
	"github.com/ModderMule/enodemeta/nzbmeta"
	"github.com/ModderMule/enodemeta/torrentmeta"
)

// fileIndexVectorsPath is the second cross-repository artifact, alongside
// meta-hash-vectors.json and nzb-identity-vectors.json.
//
// docs/ingest-contract.md amendment 4 says file_index means libtorrent's
// file_index_t and that it is "pinned by test vectors rather than asserted in
// prose". This is that file, and writing it is what makes the amendment true.
const fileIndexVectorsPath = "testdata/fileindex-vectors.json"

var updateFileIndexVectors = flag.Bool("update", false, "rewrite the file-index vectors file")

type fileIndexVectorFile struct {
	Scheme string `json:"scheme"`
	Note   string `json:"note"`

	Releases []fileIndexVector `json:"releases"`
}

// fileIndexVector is one release and the row every one of its files produces.
type fileIndexVector struct {
	Label string `json:"label"`
	Note  string `json:"note"`

	Kind     uint8  `json:"kind"`
	KindName string `json:"kindName"`

	// Exactly one of MetafileHex and NZB is set. A torrent's info dictionary is
	// binary, so it is hex; an NZB is text, so it is readable.
	MetafileHex string `json:"metafileHex,omitempty"`
	NZB         string `json:"nzb,omitempty"`

	IdentityHex string `json:"identityHex"`

	// FileCount is what MintInput.FileCount carries: the selectable,
	// non-padding files. It comes from the metafile rather than from how many
	// rows were emitted, so a hash's flags do not change when the operator
	// changes how many files are advertised.
	FileCount int `json:"fileCount"`

	Files []fileIndexEntry `json:"files"`

	// WholeSetMetaHashHex is empty for a release of one selectable file.
	WholeSetMetaHashHex string `json:"wholeSetMetaHashHex"`
}

type fileIndexEntry struct {
	// Index is the libtorrent file_index_t: the position in the v1 file list for
	// a v1 or hybrid torrent, the file tree's traversal order for a v2-only one,
	// and the surviving-document order for an NZB.
	Index uint32 `json:"index"`

	Path string `json:"path"`
	Size uint64 `json:"size"`

	// Padding marks an alignment file. It keeps its index — that is the whole
	// point of this file — and gets no row, so MetaHashHex is empty for it.
	Padding bool `json:"padding"`

	MetaHashHex string `json:"metaHashHex,omitempty"`
}

// TestFileIndexVectors checks this implementation against the checked-in file,
// and rewrites it with -update.
func TestFileIndexVectors(t *testing.T) {
	want := buildFileIndexVectors(t)

	if *updateFileIndexVectors {
		writeFileIndexVectors(t, want)
		t.Logf("output: rewrote %s with %d releases", fileIndexVectorsPath, len(want.Releases))

		return
	}

	raw, err := os.ReadFile(fileIndexVectorsPath)
	if err != nil {
		t.Fatalf("reading the vectors (regenerate with: go test . -update): %v", err)
	}

	var got fileIndexVectorFile
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parsing the vectors: %v", err)
	}
	t.Logf("input:  %s with %d releases", fileIndexVectorsPath, len(got.Releases))

	for _, v := range got.Releases {
		t.Run(v.Label, func(t *testing.T) {
			checkFileIndexVector(t, v)
		})
	}

	if diff := compareFileIndexSerialised(t, got, want); diff != "" {
		t.Errorf("the vectors file no longer matches this implementation: %s\nregenerate deliberately with: go test . -update", diff)
	}
}

// -- internals ---------------------------------------------------------------

// checkFileIndexVector re-derives everything the vector states from the metafile
// alone, which is what a port has to be able to do.
func checkFileIndexVector(t *testing.T, v fileIndexVector) {
	t.Helper()

	kind := metahash.Kind(v.Kind)

	var metafile []byte
	if v.NZB != "" {
		metafile = []byte(v.NZB)
	} else {
		metafile = mustFileIndexHex(t, v.MetafileHex)
	}
	t.Logf("input:  %s, %d bytes, %d files", v.KindName, len(metafile), len(v.Files))

	identity, err := IdentityOf(kind, metafile)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if got := strings.ToUpper(hex.EncodeToString(identity)); got != v.IdentityHex {
		t.Fatalf("identity: got %s, want %s", got, v.IdentityHex)
	}

	for _, f := range v.Files {
		if f.Padding {
			if f.MetaHashHex != "" {
				t.Errorf("index %d: a padding file must produce no row", f.Index)
			}
			t.Logf("output: index %-5d %-40q %10d bytes  padding, no row", f.Index, f.Path, f.Size)

			continue
		}

		hash, err := metahash.Mint(metahash.MintInput{
			Kind:      kind,
			Identity:  identity,
			FileIndex: f.Index,
			FileCount: uint32(v.FileCount),
		})
		if err != nil {
			t.Fatalf("index %d: %v", f.Index, err)
		}

		if hash.String() != f.MetaHashHex {
			t.Errorf("index %d: got %s, want %s", f.Index, hash, f.MetaHashHex)
		}
		if !hash.VerifyIdentity(identity) {
			t.Errorf("index %d: must fold to the release identity", f.Index)
		}

		t.Logf("output: index %-5d %-40q %10d bytes  %s", f.Index, f.Path, f.Size, hash)
	}

	if v.WholeSetMetaHashHex == "" {
		return
	}

	whole, err := metahash.ParseHex(v.WholeSetMetaHashHex)
	if err != nil {
		t.Fatalf("whole-set hash: %v", err)
	}
	for _, f := range v.Files {
		if f.MetaHashHex == "" {
			continue
		}

		one, err := metahash.ParseHex(f.MetaHashHex)
		if err != nil {
			t.Fatal(err)
		}
		if !whole.SameRelease(one) {
			t.Errorf("index %d must read as the same release as the whole-set row", f.Index)
		}
	}
	t.Logf("output: whole-set %s", v.WholeSetMetaHashHex)
}

func buildFileIndexVectors(t *testing.T) fileIndexVectorFile {
	t.Helper()

	out := fileIndexVectorFile{
		Scheme: "enode.meta.v1",
		Note: "Cross-repository test vectors for file_index (see docs/ingest-contract.md " +
			"amendment 4). file_index is libtorrent's file_index_t: padding files are counted " +
			"in the numbering and get no row of their own, so a selected file's index has " +
			"gaps — it has to, because that index is what a BEP 53 so= and a meta row both " +
			"refer to. For a v2-only torrent it is the file tree's traversal order; for an " +
			"NZB it is the position among the files that survived parsing. fileCount is the " +
			"non-padding count and comes from the metafile, never from how many rows were " +
			"emitted. Not covered here because it needs 65536 files: an index whose low 16 " +
			"bits are 0xFFFF is refused rather than renumbered, and metahash's own vectors " +
			"pin that. Regenerate with: go test . -update",
	}

	for _, c := range []struct {
		label string
		note  string
		raw   []byte
	}{
		{
			label: "v1, a single file",
			note:  "One row at index 0, the multi-file flag clear, and no whole-set row: it would say nothing a plain row does not.",
			raw:   v1SingleFileFixture(),
		},
		{
			label: "v1 multi-file, with a BEP 47 pad and a BitComet pad",
			note:  "The two padding conventions, interleaved with real files. Indexes 1 and 3 are pads and produce no row, so the real files are 0, 2 and 4 — the gaps are the point.",
			raw:   v1MultiFileFixture(),
		},
		{
			label: "v2-only, numbered by file tree traversal",
			note:  "A v2 torrent has no file list, so the index is the order the file tree is walked in. BEP 52 has no padding files at all.",
			raw:   v2OnlyFixture(),
		},
		{
			label: "hybrid, numbered by its v1 file list",
			note:  "A hybrid is keyed by its v1 infohash and numbered by its v1 list, which is what the swarm and every magnet use. The v2 view must agree about the files, and a torrent whose views disagree is refused rather than catalogued.",
			raw:   hybridFixture(),
		},
	} {
		info, err := torrentmeta.ParseInfo(c.raw)
		if err != nil {
			t.Fatalf("generating vector %q: %v", c.label, err)
		}

		kind, identity := info.Identity()

		v := fileIndexVector{
			Label:       c.label,
			Note:        c.note,
			Kind:        uint8(kind),
			KindName:    kind.String(),
			MetafileHex: strings.ToUpper(hex.EncodeToString(c.raw)),
			IdentityHex: strings.ToUpper(hex.EncodeToString(identity)),
		}

		for _, f := range info.Files {
			if !f.Padding {
				v.FileCount++
			}
		}

		for _, f := range info.Files {
			entry := fileIndexEntry{Index: f.Index, Path: f.Path, Size: f.Size, Padding: f.Padding}
			if !f.Padding {
				entry.MetaHashHex = mintFileIndexHex(t, kind, identity, f.Index, uint32(v.FileCount))
			}
			v.Files = append(v.Files, entry)
		}

		if v.FileCount > 1 {
			v.WholeSetMetaHashHex = mintFileIndexHex(t, kind, identity, metahash.FileIndexWholeSet32, uint32(v.FileCount))
		}

		out.Releases = append(out.Releases, v)
	}

	// The NZB case. Two properties are specific to it: padding files do not
	// exist, and FlagPathAuthoritative is always set because an NZB's file order
	// is not fixed across generators.
	const nzb = `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <file subject="[1/3] - &#34;rel.part01.rar&#34; yEnc (1/2)">
    <segments>
      <segment bytes="700000" number="1">a1@example.invalid</segment>
      <segment bytes="700000" number="2">a2@example.invalid</segment>
    </segments>
  </file>
  <file subject="empty, and therefore dropped before it is numbered">
    <segments><segment number="1"></segment></segments>
  </file>
  <file subject="[3/3] - &#34;rel.nfo&#34; yEnc (1/1)">
    <segments><segment bytes="2000" number="1">n1@example.invalid</segment></segments>
  </file>
</nzb>
`

	doc, err := nzbmeta.Parse([]byte(nzb))
	if err != nil {
		t.Fatalf("generating the NZB vector: %v", err)
	}

	digest, err := nzbmeta.Identity(doc)
	if err != nil {
		t.Fatalf("generating the NZB vector: %v", err)
	}

	nzbVector := fileIndexVector{
		Label: "nzb, numbered by surviving document order",
		Note: "The middle <file> has no usable segment, so it is dropped before anything is " +
			"numbered and the third file is index 1. Numbering it would give two " +
			"implementations different indexes for the same release, and the index is what a " +
			"row addresses. Every nzb row is path-authoritative.",
		Kind:        uint8(metahash.KindNZB),
		KindName:    metahash.KindNZB.String(),
		NZB:         nzb,
		IdentityHex: strings.ToUpper(hex.EncodeToString(digest[:])),
		FileCount:   len(doc.Files),
	}

	for i := range doc.Files {
		f := doc.Files[i]
		nzbVector.Files = append(nzbVector.Files, fileIndexEntry{
			Index:       uint32(f.Index),
			Path:        f.Subject,
			Size:        f.EncodedBytes(),
			MetaHashHex: mintFileIndexHex(t, metahash.KindNZB, digest[:], uint32(f.Index), uint32(len(doc.Files))),
		})
	}

	if len(doc.Files) > 1 {
		nzbVector.WholeSetMetaHashHex = mintFileIndexHex(t, metahash.KindNZB, digest[:],
			metahash.FileIndexWholeSet32, uint32(len(doc.Files)))
	}

	out.Releases = append(out.Releases, nzbVector)

	return out
}

// The fixtures are built here rather than checked in as binaries: a reader can
// see exactly which field each case is about, and a case can be varied by one
// key. They mirror torrentmeta's own test fixtures deliberately, so a failure
// here and a failure there point at the same thing.

func v1SingleFileFixture() []byte {
	return bencode.MustEncode(map[string]any{
		"name":         "ubuntu-24.04.iso",
		"piece length": 262144,
		"pieces":       strings.Repeat("01234567890123456789", 4),
		"length":       1 << 30,
	})
}

func v1MultiFileFixture() []byte {
	return bencode.MustEncode(map[string]any{
		"name":         "Some.Release.2026",
		"piece length": 262144,
		"pieces":       strings.Repeat("01234567890123456789", 8),
		"files": []any{
			map[string]any{"length": 700 << 20, "path": []any{"Some.Release.2026.mkv"}},
			// BEP 47 padding, by attribute.
			map[string]any{"length": 16384, "path": []any{".pad", "16384"}, "attr": "p"},
			map[string]any{"length": 4096, "path": []any{"Some.Release.2026.nfo"}},
			// BitComet's older convention, by name.
			map[string]any{"length": 8192, "path": []any{"_____padding_file_3", "8192"}},
			map[string]any{"length": 120 << 20, "path": []any{"Extras", "Sample.mkv"}},
		},
	})
}

func v2OnlyFixture() []byte {
	piecesRoot := strings.Repeat("A", 32)

	return bencode.MustEncode(map[string]any{
		"name":         "v2.release",
		"piece length": 262144,
		"meta version": 2,
		"file tree": map[string]any{
			"Extras": map[string]any{
				"notes.txt": map[string]any{
					"": map[string]any{"length": 2048, "pieces root": piecesRoot},
				},
			},
			"movie.mkv": map[string]any{
				"": map[string]any{"length": 900 << 20, "pieces root": piecesRoot},
			},
		},
	})
}

func hybridFixture() []byte {
	piecesRoot := strings.Repeat("A", 32)

	return bencode.MustEncode(map[string]any{
		"name":         "hybrid.release",
		"piece length": 262144,
		"meta version": 2,
		"pieces":       strings.Repeat("01234567890123456789", 4),
		"files": []any{
			map[string]any{"length": 900 << 20, "path": []any{"movie.mkv"}},
			map[string]any{"length": 16384, "path": []any{".pad", "16384"}, "attr": "p"},
			map[string]any{"length": 2048, "path": []any{"Extras", "notes.txt"}},
		},
		"file tree": map[string]any{
			"Extras": map[string]any{
				"notes.txt": map[string]any{
					"": map[string]any{"length": 2048, "pieces root": piecesRoot},
				},
			},
			"movie.mkv": map[string]any{
				"": map[string]any{"length": 900 << 20, "pieces root": piecesRoot},
			},
		},
	})
}

func mintFileIndexHex(t *testing.T, kind metahash.Kind, identity []byte, index, count uint32) string {
	t.Helper()

	hash, err := metahash.Mint(metahash.MintInput{
		Kind:      kind,
		Identity:  identity,
		FileIndex: index,
		FileCount: count,
	})
	if err != nil {
		t.Fatalf("minting index %d of %d: %v", index, count, err)
	}

	return hash.String()
}

func writeFileIndexVectors(t *testing.T, v fileIndexVectorFile) {
	t.Helper()

	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("encoding the vectors: %v", err)
	}
	raw = append(raw, '\n')

	if err := os.MkdirAll(filepath.Dir(fileIndexVectorsPath), 0o755); err != nil {
		t.Fatalf("creating the vectors directory: %v", err)
	}
	if err := os.WriteFile(fileIndexVectorsPath, raw, 0o644); err != nil {
		t.Fatalf("writing the vectors: %v", err)
	}
}

func compareFileIndexSerialised(t *testing.T, got, want fileIndexVectorFile) string {
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

func mustFileIndexHex(t *testing.T, s string) []byte {
	t.Helper()

	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("vector hex %q: %v", s, err)
	}

	return raw
}
