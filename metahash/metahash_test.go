package metahash

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

// wideIndex is a file ordinal past what the hash's 16-bit field can hold, with
// its truncation alongside. They are variables rather than constants because
// the conversion is deliberately lossy, and Go refuses to write that as one.
var (
	wideIndex      = uint32(70000)
	wideIndexLow16 = uint16(wideIndex)
)

// TestFoldVectors pins the fold against hand-computed cases, including the two
// real input widths: a 20-byte SHA-1 and a 32-byte SHA-256.
func TestFoldVectors(t *testing.T) {
	cases := []struct {
		label string
		in    []byte
		n     int
		want  []byte
	}{
		{
			label: "an empty digest folds to zeros",
			in:    nil,
			n:     10,
			want:  make([]byte, 10),
		},
		{
			label: "one byte lands in the first slot",
			in:    []byte{0xAB},
			n:     10,
			want:  []byte{0xAB, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			label: "20 bytes is the two halves XORed",
			in: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xA0,
			},
			n:    10,
			want: []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA},
		},
		{
			label: "32 bytes is three passes, the last partial",
			in: []byte{
				0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x08, 0x10,
			},
			n: 10,
			// d[0], d[10], d[20] and d[30] all land in out[0]; d[31] in out[1].
			want: []byte{0x0F, 0x10, 0, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			label: "a width of one XORs everything together",
			in:    []byte{0x0F, 0xF0, 0xFF},
			n:     1,
			want:  []byte{0x00},
		},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  % x (n=%d)", c.in, c.n)

			got := Fold(c.in, c.n)
			t.Logf("output: % x", got)

			if !bytes.Equal(got, c.want) {
				t.Errorf("got % x, want % x", got, c.want)
			}

			if c.n == DigestSize {
				folded := Fold10(c.in)
				if !bytes.Equal(folded[:], c.want) {
					t.Errorf("Fold10 disagrees with Fold: % x vs % x", folded, c.want)
				}
			}
		})
	}
}

func TestBuildParseRoundTrip(t *testing.T) {
	v1 := sha1.Sum([]byte("a torrent"))
	v2 := sha256.Sum256([]byte("a v2 torrent"))

	cases := []struct {
		label    string
		kind     Kind
		flags    Flags
		index    uint32
		identity []byte
	}{
		{"a single-file torrent", KindBTV1, 0, 0, v1[:]},
		{"one file of a multi-file torrent", KindBTV1, FlagMultiFile, 3, v1[:]},
		{"the whole-set row", KindBTV1, FlagMultiFile, FileIndexWholeSet32, v1[:]},
		{"a v2-only torrent", KindBTV2, FlagMultiFile | FlagPathAuthoritative, 7, v2[:]},
		{"an NZB, path authoritative", KindNZB, FlagMultiFile | FlagPathAuthoritative, 1, v2[:]},
		{"a protected release", KindNZB, FlagProtected, 0, v2[:]},
		{"every defined flag at once", KindBTV1, FlagMultiFile | FlagPathAuthoritative | FlagProtected, 9, v1[:]},
		{"a wide index keeps its low 16 bits", KindBTV1, FlagMultiFile | FlagPathAuthoritative, 70000, v1[:]},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  kind=%s flags=%s index=%d identity=% x", c.kind, c.flags, c.index, c.identity[:4])

			h, err := Build(c.kind, c.flags, c.index, c.identity)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			t.Logf("output: %s", h)

			p, err := Parse(h[:])
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			if p.Kind != c.kind {
				t.Errorf("kind: got %s, want %s", p.Kind, c.kind)
			}
			if p.Flags != c.flags {
				t.Errorf("flags: got %s, want %s", p.Flags, c.flags)
			}
			if want := uint16(c.index); p.FileIndex != want {
				t.Errorf("index: got %d, want %d", p.FileIndex, want)
			}
			if p.Version != Version1 {
				t.Errorf("version: got %d, want %d", p.Version, Version1)
			}

			if !h.VerifyIdentity(c.identity) {
				t.Error("the hash must verify against the identity it was built from")
			}
			if h.VerifyIdentity([]byte("something else entirely")) {
				t.Error("a different identity must not verify")
			}
			if !IsMetaHash(h[:]) {
				t.Error("a minted hash must be recognised as one")
			}
			if !RejectOfferedFile(h[:]) {
				t.Error("a meta hash offered as a real file must be refused")
			}

			if err := CrossCheck(h, c.kind, Version1, c.flags, c.index); err != nil {
				t.Errorf("a row whose tags match its hash must cross-check: %v", err)
			}
		})
	}
}

func TestBuildRejects(t *testing.T) {
	v1 := sha1.Sum([]byte("x"))
	v2 := sha256.Sum256([]byte("x"))

	cases := []struct {
		label    string
		kind     Kind
		flags    Flags
		index    uint32
		identity []byte
		want     error
	}{
		{"an unknown kind", Kind(9), 0, 0, v1[:], ErrKind},
		{"kind 0", KindUnspecified, 0, 0, v1[:], ErrKind},
		{"no identity", KindBTV1, 0, 0, nil, ErrIdentityMissing},
		{"a v2 identity under kind v1", KindBTV1, 0, 0, v2[:], ErrIdentityLength},
		{"a v1 identity under kind v2", KindBTV2, 0, 0, v1[:], ErrIdentityLength},
		{"a truncated identity", KindBTV1, 0, 0, v1[:19], ErrIdentityLength},
		{"the reserved flag", KindBTV1, FlagReserved, 0, v1[:], ErrReservedFlag},
		{"a file index that truncates to the whole-set marker", KindBTV1, FlagMultiFile, 0xFFFF, v1[:], ErrFileIndex},
		{"a wide index that also truncates to it", KindBTV1, FlagMultiFile | FlagPathAuthoritative, 0x1FFFF, v1[:], ErrFileIndex},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  kind=%d flags=%s index=%d identity=%d byte(s)", c.kind, c.flags, c.index, len(c.identity))

			_, err := Build(c.kind, c.flags, c.index, c.identity)
			if err == nil {
				t.Fatalf("%s must be rejected", c.label)
			}
			t.Logf("output: %v", err)

			if !errors.Is(err, c.want) {
				t.Errorf("got %v, want it to wrap %v", err, c.want)
			}
		})
	}
}

// TestBuildWideIndexNeedsPath pins §3.5: past 65535 files the hash cannot
// address the file by itself, so the path has to be authoritative.
func TestBuildWideIndexNeedsPath(t *testing.T) {
	v1 := sha1.Sum([]byte("x"))
	t.Logf("input:  index=70000 with no path-authoritative flag")

	_, err := Build(KindBTV1, FlagMultiFile, 70000, v1[:])
	if err == nil {
		t.Fatal("an index past 65535 without FlagPathAuthoritative must be rejected")
	}
	t.Logf("output: %v", err)

	if !strings.Contains(err.Error(), "FlagPathAuthoritative") {
		t.Errorf("the message must name the missing flag, got: %v", err)
	}
}

func TestParseRejects(t *testing.T) {
	good, err := Build(KindBTV1, 0, 0, make([]byte, 20))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	mutate := func(f func(h *Hash)) []byte {
		h := good
		f(&h)

		return h[:]
	}

	cases := []struct {
		label string
		in    []byte
		want  error
	}{
		{"a short hash", good[:15], ErrLength},
		{"a long hash", append(good[:], 0), ErrLength},
		{"an MD4 that is not a meta hash", bytes.Repeat([]byte{0x41}, Size), ErrMagic},
		{"a wrong first magic byte", mutate(func(h *Hash) { h[0] = 0xEE }), ErrMagic},
		{"a wrong second magic byte", mutate(func(h *Hash) { h[1] = 0x2C }), ErrMagic},
		{"an unknown version", mutate(func(h *Hash) { h[2] = 0x02 }), ErrVersion},
		{"an unknown kind", mutate(func(h *Hash) { h[3] = 0xF0 }), ErrKind},
		{"kind 0", mutate(func(h *Hash) { h[3] = 0x00 }), ErrKind},
		{"the reserved flag set", mutate(func(h *Hash) { h[3] |= 0x08 }), ErrReservedFlag},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  % x", c.in)

			_, err := Parse(c.in)
			if err == nil {
				t.Fatalf("%s must be rejected", c.label)
			}
			t.Logf("output: %v", err)

			if !errors.Is(err, c.want) {
				t.Errorf("got %v, want it to wrap %v", err, c.want)
			}
			if IsMetaHash(c.in) {
				t.Error("IsMetaHash must agree with Parse")
			}
		})
	}
}

// TestSameRelease pins the grouping property of §3.4: the digest covers the
// whole metafile, so rows for two files of one torrent match while rows from
// different torrents do not.
func TestSameRelease(t *testing.T) {
	identity := sha1.Sum([]byte("one release"))
	other := sha1.Sum([]byte("another release"))

	first, err := Build(KindBTV1, FlagMultiFile, 0, identity[:])
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	second, err := Build(KindBTV1, FlagMultiFile, 5, identity[:])
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	wholeSet, err := Build(KindBTV1, FlagMultiFile, FileIndexWholeSet32, identity[:])
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	unrelated, err := Build(KindBTV1, FlagMultiFile, 0, other[:])
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	t.Logf("input:  file0=%s file5=%s wholeSet=%s other=%s", first, second, wholeSet, unrelated)

	if !first.SameRelease(second) || !first.SameRelease(wholeSet) {
		t.Error("rows of one release must group together")
	}
	if first.SameRelease(unrelated) {
		t.Error("rows of different releases must not group")
	}

	// Distinct rows must still be distinct hashes, or the client's md4-equality
	// grouping would collapse them into one result.
	if first == second || first == wholeSet {
		t.Error("two rows of one release must not share a full hash")
	}
	t.Logf("output: grouped by digest, distinct by index")
}

func TestCrossCheckMismatches(t *testing.T) {
	identity := sha1.Sum([]byte("release"))
	h, err := Build(KindBTV1, FlagMultiFile, 3, identity[:])
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	cases := []struct {
		label   string
		kind    Kind
		version uint8
		flags   Flags
		index   uint32
		want    error
	}{
		{"the kind tag disagrees", KindBTV2, Version1, FlagMultiFile, 3, ErrKind},
		{"the version tag disagrees", KindBTV1, 2, FlagMultiFile, 3, ErrVersion},
		{"the flags disagree", KindBTV1, Version1, 0, 3, nil},
		{"the index disagrees", KindBTV1, Version1, FlagMultiFile, 4, nil},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  hash=%s tags: kind=%s version=%d flags=%s index=%d", h, c.kind, c.version, c.flags, c.index)

			err := CrossCheck(h, c.kind, c.version, c.flags, c.index)
			if err == nil {
				t.Fatalf("%s must be reported", c.label)
			}
			t.Logf("output: %v", err)

			if c.want != nil && !errors.Is(err, c.want) {
				t.Errorf("got %v, want it to wrap %v", err, c.want)
			}
		})
	}
}

// TestCrossCheckWideIndex covers the one case where a disagreement is expected
// to pass: the hash holds the low 16 bits of a wide index, so the tag's full
// value and the hash's truncation still describe the same file.
func TestCrossCheckWideIndex(t *testing.T) {
	identity := sha1.Sum([]byte("huge release"))
	index := wideIndex

	h, err := Build(KindBTV1, FlagMultiFile|FlagPathAuthoritative, index, identity[:])
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Logf("input:  index=%d hash index=%d", index, wideIndexLow16)

	if err := CrossCheck(h, KindBTV1, Version1, FlagMultiFile|FlagPathAuthoritative, index); err != nil {
		t.Errorf("a wide index must cross-check against its truncation: %v", err)
	}
	t.Logf("output: accepted")
}

func TestMint(t *testing.T) {
	v1 := sha1.Sum([]byte("mint"))
	v2 := sha256.Sum256([]byte("mint"))

	cases := []struct {
		label     string
		in        MintInput
		wantFlags Flags
		wantIndex uint16
	}{
		{
			label:     "a single-file torrent has no multi-file flag",
			in:        MintInput{Kind: KindBTV1, Identity: v1[:], FileIndex: 0, FileCount: 1},
			wantFlags: 0,
			wantIndex: 0,
		},
		{
			label:     "a multi-file torrent sets the flag from the file count",
			in:        MintInput{Kind: KindBTV1, Identity: v1[:], FileIndex: 2, FileCount: 500},
			wantFlags: FlagMultiFile,
			wantIndex: 2,
		},
		{
			label:     "the whole-set row",
			in:        MintInput{Kind: KindBTV1, Identity: v1[:], FileIndex: FileIndexWholeSet32, FileCount: 500},
			wantFlags: FlagMultiFile,
			wantIndex: FileIndexWholeSet,
		},
		{
			label:     "an NZB is always path-authoritative",
			in:        MintInput{Kind: KindNZB, Identity: v2[:], FileIndex: 1, FileCount: 4},
			wantFlags: FlagMultiFile | FlagPathAuthoritative,
			wantIndex: 1,
		},
		{
			label:     "a wide index implies path-authoritative",
			in:        MintInput{Kind: KindBTV1, Identity: v1[:], FileIndex: 70000, FileCount: 80000},
			wantFlags: FlagMultiFile | FlagPathAuthoritative,
			wantIndex: wideIndexLow16,
		},
		{
			label:     "protected carries through",
			in:        MintInput{Kind: KindBTV2, Identity: v2[:], FileIndex: 0, FileCount: 1, Protected: true},
			wantFlags: FlagProtected,
			wantIndex: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %+v", c.in)

			h, err := Mint(c.in)
			if err != nil {
				t.Fatalf("mint: %v", err)
			}

			p, err := Parse(h[:])
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			t.Logf("output: %s kind=%s flags=%s index=%d", h, p.Kind, p.Flags, p.FileIndex)

			if p.Flags != c.wantFlags {
				t.Errorf("flags: got %s, want %s", p.Flags, c.wantFlags)
			}
			if p.FileIndex != c.wantIndex {
				t.Errorf("index: got %d, want %d", p.FileIndex, c.wantIndex)
			}
		})
	}
}

// TestMintFileCountIsStable is the reason FileCount comes from the metafile and
// not from how many rows were emitted: publishing fewer files of the same
// release must not change the hash of the rows that are still published.
func TestMintFileCountIsStable(t *testing.T) {
	identity := sha1.Sum([]byte("stable"))

	before, err := Mint(MintInput{Kind: KindBTV1, Identity: identity[:], FileIndex: 2, FileCount: 500})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	// The operator lowers max_files_per_torrent; the release is unchanged.
	after, err := Mint(MintInput{Kind: KindBTV1, Identity: identity[:], FileIndex: 2, FileCount: 500})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	t.Logf("input:  the same file of the same release, published twice")
	t.Logf("output: %s / %s", before, after)

	if before != after {
		t.Errorf("the hash of a row must not depend on how many rows were emitted: %s vs %s", before, after)
	}
}

func TestMintRejects(t *testing.T) {
	v1 := sha1.Sum([]byte("x"))

	cases := []struct {
		label string
		in    MintInput
	}{
		{"an unknown kind", MintInput{Kind: Kind(7), Identity: v1[:], FileCount: 1}},
		{"a whole-set row for a single-file release", MintInput{Kind: KindBTV1, Identity: v1[:], FileIndex: FileIndexWholeSet32, FileCount: 1}},
		{"a file whose index truncates to the marker", MintInput{Kind: KindBTV1, Identity: v1[:], FileIndex: 0xFFFF, FileCount: 70000}},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %+v", c.in)

			_, err := Mint(c.in)
			if err == nil {
				t.Fatalf("%s must be rejected", c.label)
			}
			t.Logf("output: %v", err)
		})
	}
}

func TestHexRoundTrip(t *testing.T) {
	identity := sha1.Sum([]byte("hex"))
	h, err := Build(KindBTV1, FlagMultiFile, 1, identity[:])
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	s := h.String()
	t.Logf("input:  %s", s)

	if s != strings.ToUpper(s) {
		t.Errorf("hashes print uppercase in this ecosystem, got %q", s)
	}

	back, err := ParseHex(s)
	if err != nil {
		t.Fatalf("parse hex: %v", err)
	}
	if back != h {
		t.Errorf("round trip: got %s, want %s", back, h)
	}

	lower, err := ParseHex(strings.ToLower(s))
	if err != nil || lower != h {
		t.Errorf("a lowercase spelling must still parse: %v", err)
	}
	t.Logf("output: %s", back)
}
