package torrentmeta

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"github.com/ModderMule/enodemeta/bencode"
	"github.com/ModderMule/enodemeta/metahash"
)

// The fixtures are built here rather than checked in as binaries: a reader can
// see exactly which field is under test, and a case can be varied by one key.

func v1SingleFile(t *testing.T) []byte {
	t.Helper()

	return bencode.MustEncode(map[string]any{
		"name":         "ubuntu-24.04.iso",
		"piece length": 262144,
		"pieces":       strings.Repeat("01234567890123456789", 4),
		"length":       1 << 30,
	})
}

func v1MultiFile(t *testing.T) []byte {
	t.Helper()

	return bencode.MustEncode(map[string]any{
		"name":         "Some.Release.2026",
		"piece length": 262144,
		"pieces":       strings.Repeat("01234567890123456789", 8),
		"files": []any{
			map[string]any{"length": 700 << 20, "path": []any{"Some.Release.2026.mkv"}},
			// BEP 47 padding, between the two real files.
			map[string]any{"length": 16384, "path": []any{".pad", "16384"}, "attr": "p"},
			map[string]any{"length": 4096, "path": []any{"Some.Release.2026.nfo"}},
			// BitComet's older convention, by name rather than by attribute.
			map[string]any{"length": 8192, "path": []any{"_____padding_file_3", "8192"}},
			map[string]any{"length": 120 << 20, "path": []any{"Extras", "Sample.mkv"}},
		},
	})
}

func v2Only(t *testing.T) []byte {
	t.Helper()

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

func hybrid(t *testing.T, consistent bool) []byte {
	t.Helper()

	piecesRoot := strings.Repeat("A", 32)
	notesLength := 2048
	if !consistent {
		notesLength = 4096 // the file tree disagrees with the v1 list
	}

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
					"": map[string]any{"length": notesLength, "pieces root": piecesRoot},
				},
			},
			"movie.mkv": map[string]any{
				"": map[string]any{"length": 900 << 20, "pieces root": piecesRoot},
			},
		},
	})
}

func TestParseV1SingleFile(t *testing.T) {
	raw := v1SingleFile(t)
	t.Logf("input:  %d byte info dictionary", len(raw))

	info, err := ParseInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("output: version=%s name=%q files=%d total=%d hash=%x",
		info.Version, info.Name, len(info.Files), info.TotalSize, info.InfoHashV1[:6])

	if info.Version != VersionV1 {
		t.Errorf("version: got %s, want v1", info.Version)
	}
	if info.Name != "ubuntu-24.04.iso" {
		t.Errorf("name: got %q", info.Name)
	}
	if len(info.Files) != 1 || info.Files[0].Size != 1<<30 {
		t.Errorf("a single-file torrent must yield one file, got %+v", info.Files)
	}
	if info.MultiFile() {
		t.Error("one file is not multi-file")
	}
	if info.TotalSize != 1<<30 {
		t.Errorf("total: got %d, want %d", info.TotalSize, 1<<30)
	}

	// The identity has to be the SHA-1 of the exact bytes, or the row points at
	// a swarm that does not exist.
	want := sha1.Sum(raw)
	if info.InfoHashV1 != want {
		t.Errorf("infohash: got %x, want %x", info.InfoHashV1, want)
	}

	kind, identity := info.Identity()
	if kind != metahash.KindBTV1 || !bytes.Equal(identity, want[:]) {
		t.Errorf("identity: got %s %x, want bt-v1 %x", kind, identity, want)
	}
}

// TestParseV1MultiFileIndexes is the case the meta-row file index depends on:
// padding files occupy indexes, so dropping them would shift every file after
// the first pad and a row would select the wrong file.
func TestParseV1MultiFileIndexes(t *testing.T) {
	raw := v1MultiFile(t)
	t.Logf("input:  a 5-entry torrent, 2 of them padding")

	info, err := ParseInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	for _, f := range info.Files {
		t.Logf("output: index=%d padding=%-5v size=%-10d path=%q", f.Index, f.Padding, f.Size, f.Path)
	}

	if len(info.Files) != 5 {
		t.Fatalf("every entry keeps its slot, got %d", len(info.Files))
	}

	wantPadding := []bool{false, true, false, true, false}
	for i, want := range wantPadding {
		if info.Files[i].Padding != want {
			t.Errorf("file %d: padding=%v, want %v (%q)", i, info.Files[i].Padding, want, info.Files[i].Path)
		}
		if info.Files[i].Index != uint32(i) {
			t.Errorf("file %d: index=%d", i, info.Files[i].Index)
		}
	}

	selectable := info.SelectableFiles()
	if len(selectable) != 3 {
		t.Fatalf("selectable files: got %d, want 3", len(selectable))
	}
	if selectable[2].Index != 4 {
		t.Errorf("a selectable file must keep its original index, got %d, want 4", selectable[2].Index)
	}
	if selectable[2].Path != "Extras/Sample.mkv" {
		t.Errorf("nested path: got %q", selectable[2].Path)
	}

	// Padding is alignment, not content, so it must not inflate the size the
	// row advertises.
	wantTotal := uint64(700<<20 + 4096 + 120<<20)
	if info.TotalSize != wantTotal {
		t.Errorf("total size must exclude padding: got %d, want %d", info.TotalSize, wantTotal)
	}
	if info.SelectableCount() != 3 {
		t.Errorf("selectable count: got %d, want 3", info.SelectableCount())
	}
}

func TestParseV2(t *testing.T) {
	raw := v2Only(t)
	t.Logf("input:  a v2-only info dictionary")

	info, err := ParseInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, f := range info.Files {
		t.Logf("output: index=%d size=%-10d path=%q", f.Index, f.Size, f.Path)
	}

	if info.Version != VersionV2 {
		t.Fatalf("version: got %s, want v2", info.Version)
	}
	if info.HasV1 {
		t.Error("a v2-only torrent has no v1 infohash")
	}

	want := sha256.Sum256(raw)
	if info.InfoHashV2 != want {
		t.Errorf("infohash: got %x, want %x", info.InfoHashV2[:6], want[:6])
	}

	kind, identity := info.Identity()
	if kind != metahash.KindBTV2 || len(identity) != 32 {
		t.Errorf("identity: got %s with %d bytes, want bt-v2 with 32", kind, len(identity))
	}

	key, ok := info.V2Key()
	if !ok || !bytes.Equal(key[:], want[:20]) {
		t.Errorf("the DHT key is the truncated SHA-256: got %x", key)
	}

	// The file tree is walked in the dictionary's sorted key order, which is
	// what a v2-aware client indexes by.
	if len(info.Files) != 2 {
		t.Fatalf("files: got %d, want 2", len(info.Files))
	}
	if info.Files[0].Path != "Extras/notes.txt" || info.Files[1].Path != "movie.mkv" {
		t.Errorf("file tree order: got %q then %q", info.Files[0].Path, info.Files[1].Path)
	}
}

func TestParseHybrid(t *testing.T) {
	raw := hybrid(t, true)
	t.Logf("input:  a hybrid info dictionary")

	info, err := ParseInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("output: version=%s v1=%x v2=%x files=%d",
		info.Version, info.InfoHashV1[:6], info.InfoHashV2[:6], len(info.Files))

	if info.Version != VersionHybrid {
		t.Fatalf("version: got %s, want hybrid", info.Version)
	}
	if !info.HasV1 || !info.HasV2 {
		t.Error("a hybrid torrent has both infohashes")
	}

	// The rule from eMuleQt's §7.1: the v1 hash stays the swarm identity, and
	// the v2 hash travels as a tag.
	kind, identity := info.Identity()
	if kind != metahash.KindBTV1 || !bytes.Equal(identity, info.InfoHashV1[:]) {
		t.Errorf("a hybrid must be keyed by v1: got %s %x", kind, identity[:6])
	}

	// Indexes come from the v1 list, padding included, because that is what
	// libtorrent assigns.
	if len(info.Files) != 3 || !info.Files[1].Padding {
		t.Errorf("a hybrid is indexed by its v1 list: got %d files", len(info.Files))
	}

	if _, ok := info.V2Key(); !ok {
		t.Error("a hybrid is also announced under its truncated v2 hash")
	}
}

// TestParseHybridInconsistent covers a torrent whose two views describe
// different content. libtorrent refuses it, and so must this: which file list a
// client saw would otherwise depend on whether it spoke v2.
func TestParseHybridInconsistent(t *testing.T) {
	raw := hybrid(t, false)
	t.Logf("input:  a hybrid whose file tree disagrees with its v1 list")

	_, err := ParseInfo(raw)
	if err == nil {
		t.Fatal("an inconsistent hybrid must be rejected")
	}
	t.Logf("output: %v", err)

	if !errors.Is(err, ErrInconsistent) {
		t.Errorf("got %v, want it to wrap %v", err, ErrInconsistent)
	}
}

// TestInvalidUTF8 is the case a crawl meets constantly: a name in a local
// encoding, with no name.utf-8 alongside. It has to survive into a utf8mb4
// column and a protobuf string, and the row must not claim its path is
// authoritative, because another client repairs the same bytes differently.
func TestInvalidUTF8(t *testing.T) {
	raw := bencode.MustEncode(map[string]any{
		"name":         []byte{0xC8, 0xAB, 0xCA, 0xE9}, // GBK, not UTF-8
		"piece length": 262144,
		"files": []any{
			map[string]any{"length": 100, "path": []any{[]byte{0xB5, 0xE7, 0xD3, 0xB0}, "a.mkv"}},
		},
	})
	t.Logf("input:  a GBK name and path with no utf-8 spelling")

	info, err := ParseInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("output: name=%q valid=%v path=%q valid=%v",
		info.Name, info.NameValidUTF8, info.Files[0].Path, info.Files[0].PathValidUTF8)

	if info.NameValidUTF8 {
		t.Error("the name was not valid UTF-8 and must be reported as such")
	}
	if !utf8Valid(info.Name) {
		t.Errorf("the repaired name must be valid UTF-8, got %q", info.Name)
	}
	if info.Files[0].PathValidUTF8 {
		t.Error("the path was not valid UTF-8 and must be reported as such")
	}
	if !utf8Valid(info.Files[0].Path) {
		t.Errorf("the repaired path must be valid UTF-8, got %q", info.Files[0].Path)
	}
	if !bytes.Equal(info.RawName, []byte{0xC8, 0xAB, 0xCA, 0xE9}) {
		t.Errorf("the raw bytes must be kept as they were: % x", info.RawName)
	}
}

func TestPrefersUTF8Spelling(t *testing.T) {
	raw := bencode.MustEncode(map[string]any{
		"name":         []byte{0xC8, 0xAB},
		"name.utf-8":   "全集",
		"piece length": 262144,
		"files": []any{
			map[string]any{
				"length":      100,
				"path":        []any{[]byte{0xB5, 0xE7}},
				"path.utf-8":  []any{"电影.mkv"},
				"nonsense":    "ignored",
				"another key": 1,
			},
		},
	})
	t.Logf("input:  a torrent carrying both spellings")

	info, err := ParseInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("output: name=%q path=%q", info.Name, info.Files[0].Path)

	if info.Name != "全集" {
		t.Errorf("name.utf-8 must win: got %q", info.Name)
	}
	if info.Files[0].Path != "电影.mkv" {
		t.Errorf("path.utf-8 must win: got %q", info.Files[0].Path)
	}
	if !info.NameValidUTF8 || !info.Files[0].PathValidUTF8 {
		t.Error("the utf-8 spellings are valid, so nothing was repaired")
	}
}

// TestPathTraversal covers a hostile torrent: nothing it spells may escape the
// directory a client would write it into, and no separator may survive inside
// a single component.
func TestPathTraversal(t *testing.T) {
	raw := bencode.MustEncode(map[string]any{
		"name":         "evil",
		"piece length": 262144,
		"files": []any{
			map[string]any{"length": 1, "path": []any{"..", "..", "etc", "passwd"}},
			map[string]any{"length": 2, "path": []any{"a/b", "c\\d"}},
			map[string]any{"length": 3, "path": []any{"ok", "file\x00name.mkv"}},
		},
	})
	t.Logf("input:  paths with traversal, separators and a NUL")

	info, err := ParseInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, f := range info.Files {
		t.Logf("output: %q", f.Path)
	}

	if info.Files[0].Path != "etc/passwd" {
		t.Errorf("traversal components must be dropped: got %q", info.Files[0].Path)
	}
	if strings.Contains(info.Files[1].Path, "a/b") || strings.Contains(info.Files[1].Path, "\\") {
		t.Errorf("separators inside a component must not survive: got %q", info.Files[1].Path)
	}
	if strings.ContainsRune(info.Files[2].Path, 0) {
		t.Errorf("control characters must be stripped: got %q", info.Files[2].Path)
	}
}

func TestParseRejects(t *testing.T) {
	cases := []struct {
		label string
		raw   []byte
		want  error
	}{
		{"not a dictionary", bencode.MustEncode([]any{1, 2}), ErrNotDict},
		{"no name", bencode.MustEncode(map[string]any{"length": 1}), ErrNoName},
		{"an empty name", bencode.MustEncode(map[string]any{"name": "", "length": 1}), ErrNoName},
		{"a name that sanitises away", bencode.MustEncode(map[string]any{"name": "..", "length": 1}), ErrNoName},
		{"no files at all", bencode.MustEncode(map[string]any{"name": "x"}), ErrNoFiles},
		{"a negative length", bencode.MustEncode(map[string]any{"name": "x", "length": -5}), ErrBadLength},
		{
			"a file entry with no path",
			bencode.MustEncode(map[string]any{"name": "x", "files": []any{map[string]any{"length": 1}}}),
			ErrBadPath,
		},
		{
			"a file entry with no length",
			bencode.MustEncode(map[string]any{"name": "x", "files": []any{map[string]any{"path": []any{"a"}}}}),
			ErrBadLength,
		},
		{"malformed bencode", []byte("d3:abc"), nil},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", truncate(c.raw))

			_, err := ParseInfo(c.raw)
			if err == nil {
				t.Fatalf("%s must be rejected", c.label)
			}
			t.Logf("output: %v", err)

			if c.want != nil && !errors.Is(err, c.want) {
				t.Errorf("got %v, want it to wrap %v", err, c.want)
			}
		})
	}
}

func TestVerifyDHTKey(t *testing.T) {
	v1Raw := v1SingleFile(t)
	v2Raw := v2Only(t)

	v1Hash := sha1.Sum(v1Raw)
	v2Hash := sha256.Sum256(v2Raw)

	cases := []struct {
		label   string
		raw     []byte
		key     []byte
		wantErr bool
	}{
		{"a v1 torrent under its SHA-1", v1Raw, v1Hash[:], false},
		{"a v2 torrent under its truncated SHA-256", v2Raw, v2Hash[:20], false},
		{"a torrent under someone else's key", v1Raw, v2Hash[:20], true},
		{"a key of the wrong length", v1Raw, v1Hash[:19], true},
		{"tampered bytes", append(append([]byte(nil), v1Raw...), ' '), v1Hash[:], true},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %d byte(s) against key %x", len(c.raw), c.key)

			err := VerifyDHTKey(c.raw, c.key)
			t.Logf("output: %v", err)

			if c.wantErr && err == nil {
				t.Errorf("%s must be rejected", c.label)
			}
			if !c.wantErr && err != nil {
				t.Errorf("%s must be accepted: %v", c.label, err)
			}
		})
	}
}

// TestBuildTorrentFile checks the round trip a FetchMetaFile response makes:
// the info dictionary the crawler stored must come back out of the .torrent
// with its infohash intact, or a client's verification fails.
func TestBuildTorrentFile(t *testing.T) {
	raw := v1MultiFile(t)
	want := sha1.Sum(raw)

	file, err := BuildTorrentFile(raw, []string{"udp://tracker.example:6969", "udp://other.example:80"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Logf("input:  %d byte info dictionary -> %d byte torrent", len(raw), len(file))

	torrent, err := ParseTorrentFile(file)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("output: infohash=%x announce=%q tiers=%d",
		torrent.Info.InfoHashV1[:6], torrent.Announce, len(torrent.AnnounceList))

	if torrent.Info.InfoHashV1 != want {
		t.Errorf("the infohash must survive the wrapping: got %x, want %x",
			torrent.Info.InfoHashV1[:6], want[:6])
	}
	if !bytes.Equal(torrent.Info.Raw, raw) {
		t.Error("the info span must come back byte for byte")
	}
	if torrent.Announce != "udp://tracker.example:6969" || len(torrent.AnnounceList) != 2 {
		t.Errorf("trackers: got %q and %d tier(s)", torrent.Announce, len(torrent.AnnounceList))
	}

	// A DHT-crawled torrent has no trackers, which is the common case.
	bare, err := BuildTorrentFile(raw, nil)
	if err != nil {
		t.Fatalf("build without trackers: %v", err)
	}
	bareTorrent, err := ParseTorrentFile(bare)
	if err != nil {
		t.Fatalf("parse without trackers: %v", err)
	}
	if bareTorrent.Info.InfoHashV1 != want {
		t.Error("a trackerless torrent must keep the same infohash")
	}
}

func TestParseTorrentFileOuterFields(t *testing.T) {
	raw := bencode.MustEncode(map[string]any{
		"info":          bencodeValue(t, v1SingleFile(t)),
		"announce":      "udp://tracker.example:6969",
		"comment":       "hello",
		"created by":    "mktorrent 1.1",
		"creation date": 1700000000,
	})
	t.Logf("input:  a torrent with outer fields")

	torrent, err := ParseTorrentFile(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("output: comment=%q createdBy=%q date=%d",
		torrent.Comment, torrent.CreatedBy, torrent.CreationDate)

	if torrent.Comment != "hello" || torrent.CreatedBy != "mktorrent 1.1" || torrent.CreationDate != 1700000000 {
		t.Error("the outer fields must be read")
	}
}

func TestParseTorrentFileTrailingBytes(t *testing.T) {
	raw := append(bencode.MustEncode(map[string]any{"info": bencodeValue(t, v1SingleFile(t))}), "junk"...)
	t.Logf("input:  a torrent with 4 trailing bytes, as real files carry")

	torrent, err := ParseTorrentFile(raw)
	if err != nil {
		t.Fatalf("trailing bytes must be tolerated: %v", err)
	}
	t.Logf("output: infohash=%x", torrent.Info.InfoHashV1[:6])
}

// -- internals ---------------------------------------------------------------

func bencodeValue(t *testing.T, raw []byte) bencode.Value {
	t.Helper()

	v, err := bencode.Decode(raw)
	if err != nil {
		t.Fatalf("decoding a fixture: %v", err)
	}

	return v
}

func utf8Valid(s string) bool {
	return strings.ToValidUTF8(s, "?") == s
}

func truncate(b []byte) string {
	const max = 60
	if len(b) <= max {
		return string(b)
	}

	return string(b[:max]) + "..."
}
