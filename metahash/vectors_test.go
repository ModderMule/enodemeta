package metahash

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vectorsPath is deliberately outside this package's own testdata directory:
// the file is a cross-repository artifact, not a fixture for these tests.
// eNode-go and eMuleQt reproduce the same file, and it — not any one
// implementation — is what the three sides are checked against (§11).
const vectorsPath = "../testdata/meta-hash-vectors.json"

var updateVectors = flag.Bool("update", false, "rewrite the meta-hash vectors file")

// vectorFile is the on-disk shape. Field names are camelCase because a C++ port
// reads this with whatever JSON library it already has, and camelCase is the
// spelling the specification uses.
type vectorFile struct {
	Scheme string `json:"scheme"`
	Note   string `json:"note"`

	Fold   []foldVector   `json:"fold"`
	Build  []buildVector  `json:"build"`
	Reject []rejectVector `json:"reject"`
}

type foldVector struct {
	Label     string `json:"label"`
	InputHex  string `json:"inputHex"`
	Width     int    `json:"width"`
	OutputHex string `json:"outputHex"`
}

type buildVector struct {
	Label       string `json:"label"`
	Kind        uint8  `json:"kind"`
	KindName    string `json:"kindName"`
	Version     uint8  `json:"version"`
	Flags       uint8  `json:"flags"`
	FileIndex   uint32 `json:"fileIndex"`
	IdentityHex string `json:"identityHex"`
	HashHex     string `json:"hashHex"`
}

type rejectVector struct {
	Label   string `json:"label"`
	HashHex string `json:"hashHex"`
	Reason  string `json:"reason"`
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
		t.Logf("output: rewrote %s with %d fold, %d build and %d reject vectors",
			vectorsPath, len(want.Fold), len(want.Build), len(want.Reject))

		return
	}

	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatalf("reading the vectors (regenerate with: go test ./metahash -update): %v", err)
	}

	var got vectorFile
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parsing the vectors: %v", err)
	}
	t.Logf("input:  %s with %d fold, %d build and %d reject vectors",
		vectorsPath, len(got.Fold), len(got.Build), len(got.Reject))

	checkFoldVectors(t, got.Fold)
	checkBuildVectors(t, got.Build)
	checkRejectVectors(t, got.Reject)

	// The serialised form is compared too, so a vector that was dropped or
	// reordered is a failure rather than a silent shrink.
	if diff := compareSerialised(t, got, want); diff != "" {
		t.Errorf("the vectors file no longer matches this implementation: %s\nregenerate deliberately with: go test ./metahash -update", diff)
	}
}

// -- internals ---------------------------------------------------------------

func checkFoldVectors(t *testing.T, vectors []foldVector) {
	t.Helper()

	for _, v := range vectors {
		in := mustHex(t, v.InputHex)
		want := mustHex(t, v.OutputHex)

		got := Fold(in, v.Width)
		if !bytes.Equal(got, want) {
			t.Errorf("fold %q: got %x, want %x", v.Label, got, want)

			continue
		}
		t.Logf("output: fold %-44q %x", v.Label, got)
	}
}

func checkBuildVectors(t *testing.T, vectors []buildVector) {
	t.Helper()

	for _, v := range vectors {
		identity := mustHex(t, v.IdentityHex)

		h, err := Build(Kind(v.Kind), Flags(v.Flags), v.FileIndex, identity)
		if err != nil {
			t.Errorf("build %q: %v", v.Label, err)

			continue
		}

		if got := strings.ToUpper(h.String()); got != strings.ToUpper(v.HashHex) {
			t.Errorf("build %q: got %s, want %s", v.Label, got, v.HashHex)

			continue
		}

		// Every vector also has to survive the return trip, since a client
		// reading a row does exactly this.
		p, err := Parse(h[:])
		if err != nil {
			t.Errorf("parse %q: %v", v.Label, err)

			continue
		}
		if uint8(p.Kind) != v.Kind || uint8(p.Flags) != v.Flags || p.Version != v.Version {
			t.Errorf("parse %q: got kind=%d flags=%d version=%d, want %d/%d/%d",
				v.Label, p.Kind, p.Flags, p.Version, v.Kind, v.Flags, v.Version)
		}
		if !h.VerifyIdentity(identity) {
			t.Errorf("verify %q: the hash must verify against its own identity", v.Label)
		}

		t.Logf("output: build %-44q %s", v.Label, h)
	}
}

func checkRejectVectors(t *testing.T, vectors []rejectVector) {
	t.Helper()

	for _, v := range vectors {
		raw := mustHex(t, v.HashHex)

		if _, err := Parse(raw); err == nil {
			t.Errorf("reject %q: %s must not parse", v.Label, v.HashHex)

			continue
		}
		if IsMetaHash(raw) {
			t.Errorf("reject %q: IsMetaHash must agree with Parse", v.Label)
		}

		t.Logf("output: reject %-42q %s", v.Label, v.Reason)
	}
}

// buildVectorFile generates the vectors this implementation produces.
func buildVectorFile(t *testing.T) vectorFile {
	t.Helper()

	// Fixed inputs, so the file is reproducible: a vector set that changed
	// whenever it was regenerated would be worthless as a contract.
	v1 := sha1.Sum([]byte("enode.meta.v1 vector torrent"))
	v1b := sha1.Sum([]byte("enode.meta.v1 vector torrent 2"))
	v2 := sha256.Sum256([]byte("enode.meta.v1 vector torrent v2"))
	nzb := sha256.Sum256([]byte("enode.meta.v1 vector nzb"))

	out := vectorFile{
		Scheme: "enode.meta.v1",
		Note: "Cross-repository test vectors for the eD2K meta hash (see eNode-go " +
			"docs/meta-search-torrent-usenet-plan.local.md section 3). Every implementation — " +
			"eNode-go, eMuleQt and the crawler daemons — must reproduce this file exactly. " +
			"Regenerate with: go test ./metahash -update",
	}

	out.Fold = []foldVector{
		foldOf("an empty digest", nil),
		foldOf("one byte", []byte{0xAB}),
		foldOf("nine bytes, one short of the width", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}),
		foldOf("ten bytes, exactly the width", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}),
		foldOf("a 20-byte SHA-1: the two halves XORed", v1[:]),
		foldOf("a 32-byte SHA-256: three passes, the last partial", v2[:]),
		foldOf("64 bytes: a longer input still folds", bytes.Repeat([]byte{0x5A, 0xA5}, 32)),
	}

	type spec struct {
		label string
		kind  Kind
		flags Flags
		index uint32
		ident []byte
	}

	specs := []spec{
		{"bt-v1, single file, no flags", KindBTV1, 0, 0, v1[:]},
		{"bt-v1, multi-file, first file", KindBTV1, FlagMultiFile, 0, v1[:]},
		{"bt-v1, multi-file, file 5", KindBTV1, FlagMultiFile, 5, v1[:]},
		{"bt-v1, multi-file, the whole-set row", KindBTV1, FlagMultiFile, FileIndexWholeSet32, v1[:]},
		{"bt-v1, the last index below the marker", KindBTV1, FlagMultiFile, 0xFFFE, v1[:]},
		{"bt-v1, a wide index keeps its low 16 bits", KindBTV1, FlagMultiFile | FlagPathAuthoritative, 70000, v1[:]},
		{"bt-v1, a second release, same index", KindBTV1, FlagMultiFile, 5, v1b[:]},
		{"bt-v2, single file", KindBTV2, 0, 0, v2[:]},
		{"bt-v2, multi-file, path authoritative", KindBTV2, FlagMultiFile | FlagPathAuthoritative, 3, v2[:]},
		{"nzb, single file", KindNZB, FlagPathAuthoritative, 0, nzb[:]},
		{"nzb, multi-file, protected", KindNZB, FlagMultiFile | FlagPathAuthoritative | FlagProtected, 2, nzb[:]},
	}

	// Every flag combination that leaves the reserved bit clear, so a port
	// cannot pass by handling only the combinations it happens to emit.
	for flags := Flags(0); flags <= FlagMultiFile|FlagPathAuthoritative|FlagProtected; flags++ {
		if flags&FlagReserved != 0 {
			continue
		}
		specs = append(specs, spec{
			label: fmt.Sprintf("bt-v1, flags 0x%X (%s)", uint8(flags), flags),
			kind:  KindBTV1,
			flags: flags,
			index: 1,
			ident: v1[:],
		})
	}

	for _, s := range specs {
		h, err := Build(s.kind, s.flags, s.index, s.ident)
		if err != nil {
			t.Fatalf("generating vector %q: %v", s.label, err)
		}

		out.Build = append(out.Build, buildVector{
			Label:       s.label,
			Kind:        uint8(s.kind),
			KindName:    s.kind.String(),
			Version:     Version1,
			Flags:       uint8(s.flags),
			FileIndex:   s.index,
			IdentityHex: strings.ToUpper(hex.EncodeToString(s.ident)),
			HashHex:     h.String(),
		})
	}

	valid, err := Build(KindBTV1, FlagMultiFile, 1, v1[:])
	if err != nil {
		t.Fatalf("generating the base hash for the reject vectors: %v", err)
	}

	mutate := func(f func(h *Hash)) string {
		h := valid
		f(&h)

		return h.String()
	}

	out.Reject = []rejectVector{
		{"a wrong first magic byte", mutate(func(h *Hash) { h[0] = 0xEE }), "magic must be ED 2B"},
		{"a wrong second magic byte", mutate(func(h *Hash) { h[1] = 0x2C }), "magic must be ED 2B"},
		{"an unknown scheme version", mutate(func(h *Hash) { h[2] = 0x02 }), "a client must silently drop a row whose version it does not know"},
		{"kind 0", mutate(func(h *Hash) { h[3] &= 0x0F }), "kind 0 is reserved"},
		{"an unknown kind", mutate(func(h *Hash) { h[3] = 0xF0 | (h[3] & 0x0F) }), "kind must be 1, 2 or 3 in this version"},
		{"the reserved flag set", mutate(func(h *Hash) { h[3] |= 0x08 }), "flag 0x8 is reserved and must be zero"},
		{"an ordinary MD4 file hash", strings.ToUpper(hex.EncodeToString(bytes.Repeat([]byte{0x41}, Size))), "not a meta hash at all"},
		{"a truncated hash", strings.ToUpper(hex.EncodeToString(valid[:15])), "a meta hash is exactly 16 bytes"},
	}

	return out
}

func foldOf(label string, in []byte) foldVector {
	out := Fold(in, DigestSize)

	return foldVector{
		Label:     label,
		InputHex:  strings.ToUpper(hex.EncodeToString(in)),
		Width:     DigestSize,
		OutputHex: strings.ToUpper(hex.EncodeToString(out)),
	}
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

func mustHex(t *testing.T, s string) []byte {
	t.Helper()

	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("vector hex %q: %v", s, err)
	}

	return raw
}
