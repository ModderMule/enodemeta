// Package torrentmeta parses a torrent's info dictionary and derives the
// identity a meta row is built from.
//
// It handles the three shapes a torrent comes in — v1 (BEP 3), v2 (BEP 52) and
// hybrid — and the two conventions for padding files (BEP 47's attr, and
// BitComet's naming). What it deliberately does not do is repair anything: a
// crawler meets every generator that ever existed, so the job is to describe
// what is there, or refuse it, and never to normalise it into something that
// would hash differently than the swarm expects.
//
// # The identity rule
//
// A hybrid torrent is keyed by its v1 infohash, because that is what the swarm,
// the trackers and every magnet link use (eMuleQt's BitTorrent research §7.1,
// rule 2). The v2 hash travels as a tag, not as the identity.
package torrentmeta

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ModderMule/enodemeta/bencode"
	"github.com/ModderMule/enodemeta/metahash"
)

// Limits that keep a hostile or broken metafile from turning into an
// allocation. They are far above anything real: the largest torrents in the
// wild hold tens of thousands of files.
const (
	MaxFiles        = 500_000
	MaxPathSegments = 64
	MaxNameBytes    = 4096
)

// unnamedComponent stands in for a file path component with nothing usable left
// in it: empty, a traversal, or made only of characters that are stripped.
//
// It is libtorrent's choice, and the reason to copy it is that libtorrent is
// what the swarm runs. sanitize_append_path_element (src/torrent_info.cpp) gives
// every element of a file's path exactly one element on disk, writing "_" for one
// that sanitises to nothing, so a client downloads such a torrent without
// complaint. Refusing it here gave up for good on torrents that every client
// could fetch: a four-minute server run permanently failed two of them with
// "has an empty path".
const unnamedComponent = "_"

// Version is which BitTorrent metadata version a torrent uses.
type Version uint8

// The three shapes.
const (
	VersionUnknown Version = iota
	VersionV1
	VersionV2
	VersionHybrid
)

// String names the version.
func (v Version) String() string {
	switch v {
	case VersionV1:
		return "v1"
	case VersionV2:
		return "v2"
	case VersionHybrid:
		return "hybrid"
	default:
		return "unknown"
	}
}

// Errors returned by this package.
var (
	ErrNotDict      = errors.New("torrentmeta: the info dictionary is not a dictionary")
	ErrNoName       = errors.New("torrentmeta: the info dictionary has no name")
	ErrNoFiles      = errors.New("torrentmeta: the info dictionary describes no files")
	ErrBadLength    = errors.New("torrentmeta: a file length is negative or absurd")
	ErrBadPath      = errors.New("torrentmeta: a file path is unusable")
	ErrTooManyFiles = errors.New("torrentmeta: too many files")
	ErrInconsistent = errors.New("torrentmeta: a hybrid torrent's two views disagree")
	ErrNoInfo       = errors.New("torrentmeta: the metafile has no info dictionary")
	ErrHashMismatch = errors.New("torrentmeta: the info dictionary does not hash to the expected infohash")
)

// File is one entry of a torrent, in file-index order.
//
// Padding files are kept rather than dropped: libtorrent counts them when it
// assigns file indexes, and the index is what a meta row carries, so removing
// them here would shift every index after the first pad.
type File struct {
	// Index is the libtorrent file_index_t: the position in the v1 file list
	// for a v1 or hybrid torrent, or the file tree's traversal order for a
	// v2-only one.
	Index uint32

	// Path is the display path, '/'-joined and valid UTF-8.
	Path string

	// RawPath is the path exactly as the torrent spelled it, one element per
	// component.
	RawPath [][]byte

	// PathValidUTF8 is false when the raw path was not valid UTF-8 and Path is
	// therefore a repaired version of it. A row built from such a path must not
	// claim to be authoritative: another client repairs the same bytes
	// differently.
	PathValidUTF8 bool

	Size uint64

	// Padding marks an alignment file: BEP 47's attr containing 'p', or
	// BitComet's _____padding_file_ naming.
	Padding bool
}

// Info is a parsed info dictionary.
type Info struct {
	// Raw is the exact encoded dictionary. Every hash here is computed over
	// these bytes and nothing else.
	Raw []byte

	Name          string
	RawName       []byte
	NameValidUTF8 bool

	Version     Version
	PieceLength uint64
	Private     bool

	// Files are every entry including padding, in file-index order.
	Files []File

	// TotalSize sums the non-padding files.
	TotalSize uint64

	// InfoHashV1 is the SHA-1 of Raw, present for v1 and hybrid torrents.
	InfoHashV1 [20]byte
	HasV1      bool

	// InfoHashV2 is the SHA-256 of Raw, present for v2 and hybrid torrents.
	InfoHashV2 [32]byte
	HasV2      bool
}

// Torrent is a whole .torrent file: the info dictionary plus the fields that
// live outside it.
//
// Only the info dictionary travels over BEP 9, so a crawled torrent has none of
// the outer fields — no announce list, no creation date. That is why a
// release's age is "when this crawler first saw it" rather than anything the
// torrent claims.
type Torrent struct {
	Info *Info

	Announce     string
	AnnounceList [][]string
	Comment      string
	CreatedBy    string
	CreationDate int64
}

// ParseInfo parses an info dictionary from its exact encoded bytes.
//
// The bytes must be the dictionary alone — what BEP 9 transfers, or the `info`
// span of a .torrent — because every hash is computed over them verbatim.
func ParseInfo(raw []byte) (*Info, error) {
	v, err := bencode.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("torrentmeta: %w", err)
	}
	if v.Kind != bencode.KindDict {
		return nil, ErrNotDict
	}

	info := &Info{Raw: raw}

	if err := info.readName(v); err != nil {
		return nil, err
	}

	if length, ok := v.GetInt("piece length"); ok && length > 0 {
		info.PieceLength = uint64(length)
	}
	if private, ok := v.GetInt("private"); ok && private == 1 {
		info.Private = true
	}

	metaVersion, _ := v.GetInt("meta version")
	_, hasFileTree := v.GetDict("file tree")
	_, hasPieces := v.GetString("pieces")
	_, hasFiles := v.GetList("files")
	_, hasLength := v.GetInt("length")
	hasV1Layout := hasPieces || hasFiles || hasLength

	switch {
	case hasFileTree && metaVersion >= 2 && hasV1Layout:
		info.Version = VersionHybrid
	case hasFileTree && metaVersion >= 2:
		info.Version = VersionV2
	default:
		info.Version = VersionV1
	}

	if err := info.readFiles(v); err != nil {
		return nil, err
	}

	info.hash()

	return info, nil
}

// ParseTorrentFile parses a whole .torrent.
//
// Trailing bytes after the top-level dictionary are tolerated, because real
// files carry them.
func ParseTorrentFile(raw []byte) (*Torrent, error) {
	v, _, err := bencode.DecodePrefix(raw)
	if err != nil {
		return nil, fmt.Errorf("torrentmeta: %w", err)
	}
	if v.Kind != bencode.KindDict {
		return nil, ErrNotDict
	}

	infoValue, ok := v.GetDict("info")
	if !ok {
		return nil, ErrNoInfo
	}

	info, err := ParseInfo(infoValue.Raw)
	if err != nil {
		return nil, err
	}

	t := &Torrent{Info: info}

	if announce, ok := v.GetString("announce"); ok {
		t.Announce = string(announce)
	}
	if comment, ok := v.GetString("comment"); ok {
		t.Comment = toValidUTF8(comment)
	}
	if createdBy, ok := v.GetString("created by"); ok {
		t.CreatedBy = toValidUTF8(createdBy)
	}
	if created, ok := v.GetInt("creation date"); ok {
		t.CreationDate = created
	}
	if tiers, ok := v.GetList("announce-list"); ok {
		for _, tier := range tiers {
			if tier.Kind != bencode.KindList {
				continue
			}

			var urls []string
			for _, u := range tier.List {
				if u.Kind == bencode.KindString {
					urls = append(urls, string(u.Str))
				}
			}
			if len(urls) > 0 {
				t.AnnounceList = append(t.AnnounceList, urls)
			}
		}
	}

	return t, nil
}

// Identity returns the meta-hash kind and the identity bytes for this torrent
// (§3.4).
//
// A hybrid torrent reports its v1 hash: that is the swarm's identity, so a
// client that resolves the row ends up in the same swarm the row described.
func (i *Info) Identity() (metahash.Kind, []byte) {
	if i.HasV1 {
		return metahash.KindBTV1, i.InfoHashV1[:]
	}
	if i.HasV2 {
		return metahash.KindBTV2, i.InfoHashV2[:]
	}

	return metahash.KindUnspecified, nil
}

// V2Key is the 20-byte key a v2 torrent is announced under on the DHT: the
// first half of its SHA-256 infohash (BEP 52).
func (i *Info) V2Key() ([20]byte, bool) {
	var key [20]byte
	if !i.HasV2 {
		return key, false
	}
	copy(key[:], i.InfoHashV2[:20])

	return key, true
}

// SelectableFiles are the files worth advertising: everything that is not
// padding. Their Index fields are the original ones, so a caller can still
// address them.
func (i *Info) SelectableFiles() []File {
	out := make([]File, 0, len(i.Files))
	for _, f := range i.Files {
		if !f.Padding {
			out = append(out, f)
		}
	}

	return out
}

// SelectableCount is how many non-padding files the release holds. This is the
// file count a meta hash's flags are derived from.
func (i *Info) SelectableCount() uint32 {
	var n uint32
	for _, f := range i.Files {
		if !f.Padding {
			n++
		}
	}

	return n
}

// MultiFile reports whether the release holds more than one selectable file.
func (i *Info) MultiFile() bool {
	return i.SelectableCount() > 1
}

// VerifyDHTKey checks an info dictionary against the 20-byte key it was fetched
// under.
//
// Both hashes are accepted because the DHT carries both: a v1 torrent is keyed
// by its SHA-1, and a v2 torrent by the truncated SHA-256 (BEP 52). This is the
// check that makes a metadata fetch from a stranger safe — without it, a peer
// could answer with any torrent it liked.
func VerifyDHTKey(infoRaw []byte, key []byte) error {
	if len(key) != 20 {
		return fmt.Errorf("%w: a DHT key is 20 bytes, got %d", ErrHashMismatch, len(key))
	}

	v1 := sha1.Sum(infoRaw)
	if string(v1[:]) == string(key) {
		return nil
	}

	v2 := sha256.Sum256(infoRaw)
	if string(v2[:20]) == string(key) {
		return nil
	}

	return fmt.Errorf("%w: %x is neither the SHA-1 (%x) nor the truncated SHA-256 (%x)",
		ErrHashMismatch, key, v1[:4], v2[:4])
}

// BuildTorrentFile wraps an info dictionary into a .torrent.
//
// The info span is copied through verbatim, so the file's infohash is the one
// the crawler recorded. Trackers are optional: a DHT-crawled torrent has none,
// and a client with DHT enabled needs none.
func BuildTorrentFile(infoRaw []byte, trackers []string) ([]byte, error) {
	if len(infoRaw) == 0 {
		return nil, ErrNoInfo
	}
	// Parsed rather than trusted: writing a file whose info dictionary is not
	// a dictionary would produce something no client can open.
	info, err := bencode.Decode(infoRaw)
	if err != nil {
		return nil, fmt.Errorf("torrentmeta: %w", err)
	}
	if info.Kind != bencode.KindDict {
		return nil, ErrNotDict
	}

	dict := map[string]any{"info": info}

	if len(trackers) > 0 {
		dict["announce"] = trackers[0]

		tiers := make([]any, 0, len(trackers))
		for _, tracker := range trackers {
			tiers = append(tiers, []any{tracker})
		}
		dict["announce-list"] = tiers
	}

	return bencode.Encode(dict)
}

// -- internals ---------------------------------------------------------------

func (i *Info) readName(v bencode.Value) error {
	// name.utf-8 wins when present: it exists precisely because `name` may be
	// in a local encoding this program cannot identify.
	raw, ok := v.GetString("name.utf-8")
	if !ok || len(raw) == 0 {
		raw, ok = v.GetString("name")
	}
	if !ok || len(raw) == 0 {
		return ErrNoName
	}
	if len(raw) > MaxNameBytes {
		raw = raw[:MaxNameBytes]
	}

	i.RawName = append([]byte(nil), raw...)
	i.NameValidUTF8 = utf8.Valid(raw)
	i.Name = sanitiseComponent(toValidUTF8(raw))

	if i.Name == "" {
		return ErrNoName
	}

	return nil
}

func (i *Info) readFiles(v bencode.Value) error {
	switch i.Version {
	case VersionV2:
		if err := i.readFileTree(v); err != nil {
			return err
		}
	default:
		// v1 and hybrid both carry the v1 layout, and for a hybrid it is the
		// authoritative one: libtorrent indexes a hybrid by its v1 list, which
		// includes the padding files the file tree has no entries for.
		if err := i.readV1Files(v); err != nil {
			return err
		}
		if i.Version == VersionHybrid {
			if err := i.checkHybridConsistency(v); err != nil {
				return err
			}
		}
	}

	if len(i.Files) == 0 {
		return ErrNoFiles
	}

	for _, f := range i.Files {
		if !f.Padding {
			i.TotalSize += f.Size
		}
	}

	return nil
}

func (i *Info) readV1Files(v bencode.Value) error {
	files, ok := v.GetList("files")
	if !ok {
		// Single-file torrent: the name is the file name and length is its
		// size.
		length, ok := v.GetInt("length")
		if !ok {
			return ErrNoFiles
		}
		if length < 0 {
			return fmt.Errorf("%w: %d", ErrBadLength, length)
		}

		i.Files = []File{{
			Index:         0,
			Path:          i.Name,
			RawPath:       [][]byte{i.RawName},
			PathValidUTF8: i.NameValidUTF8,
			Size:          uint64(length),
		}}

		return nil
	}

	if len(files) > MaxFiles {
		return fmt.Errorf("%w: %d", ErrTooManyFiles, len(files))
	}

	i.Files = make([]File, 0, len(files))
	for index, entry := range files {
		if entry.Kind != bencode.KindDict {
			return fmt.Errorf("%w: entry %d is not a dictionary", ErrBadPath, index)
		}

		length, ok := entry.GetInt("length")
		if !ok || length < 0 {
			return fmt.Errorf("%w: entry %d", ErrBadLength, index)
		}

		file, err := i.readPath(entry, uint32(index))
		if err != nil {
			return err
		}
		file.Size = uint64(length)
		file.Padding = isPadding(entry, file)

		i.Files = append(i.Files, file)
	}

	return nil
}

// readPath reads one v1 file entry's path, preferring the utf-8 spelling.
func (i *Info) readPath(entry bencode.Value, index uint32) (File, error) {
	components, ok := entry.GetList("path.utf-8")
	if !ok || len(components) == 0 {
		components, ok = entry.GetList("path")
	}
	if !ok || len(components) == 0 {
		return File{}, fmt.Errorf("%w: entry %d has no path", ErrBadPath, index)
	}
	if len(components) > MaxPathSegments {
		return File{}, fmt.Errorf("%w: entry %d nests %d deep", ErrBadPath, index, len(components))
	}

	file := File{Index: index, PathValidUTF8: true}

	parts := make([]string, 0, len(components))
	for _, c := range components {
		if c.Kind != bencode.KindString {
			return File{}, fmt.Errorf("%w: entry %d has a non-string path component", ErrBadPath, index)
		}

		file.RawPath = append(file.RawPath, append([]byte(nil), c.Str...))
		if !utf8.Valid(c.Str) {
			file.PathValidUTF8 = false
		}

		part := sanitiseComponent(toValidUTF8(c.Str))
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}

	// Components that sanitise away in the middle of a path are still dropped,
	// so "../../etc/passwd" stays "etc/passwd" and no stored path changes. Only a
	// path left with nothing takes libtorrent's placeholder.
	if len(parts) == 0 {
		parts = append(parts, unnamedComponent)
	}

	file.Path = strings.Join(parts, "/")

	return file, nil
}

// readFileTree walks a v2 file tree (BEP 52).
//
// Bencode dictionaries are key-sorted, and the decoder keeps that order, so
// walking depth-first assigns the same indexes a v2-aware client does.
func (i *Info) readFileTree(v bencode.Value) error {
	tree, ok := v.GetDict("file tree")
	if !ok {
		return ErrNoFiles
	}

	var walk func(node bencode.Value, prefix []string, rawPrefix [][]byte, validSoFar bool) error
	walk = func(node bencode.Value, prefix []string, rawPrefix [][]byte, validSoFar bool) error {
		if len(i.Files) > MaxFiles {
			return fmt.Errorf("%w: more than %d", ErrTooManyFiles, MaxFiles)
		}

		for _, entry := range node.Dict {
			// The empty key marks a leaf: its value holds the file's own
			// attributes rather than another directory level.
			if len(entry.Key) == 0 {
				length, ok := entry.Value.GetInt("length")
				if !ok || length < 0 {
					return fmt.Errorf("%w: %s", ErrBadLength, strings.Join(prefix, "/"))
				}
				if len(prefix) == 0 {
					return fmt.Errorf("%w: a file tree leaf with no path", ErrBadPath)
				}

				i.Files = append(i.Files, File{
					Index:         uint32(len(i.Files)),
					Path:          strings.Join(prefix, "/"),
					RawPath:       rawPrefix,
					PathValidUTF8: validSoFar,
					Size:          uint64(length),
				})

				continue
			}

			if entry.Value.Kind != bencode.KindDict {
				return fmt.Errorf("%w: %q is neither a directory nor a file", ErrBadPath, entry.Key)
			}

			// Replaced rather than dropped: in a file tree, every level is a
			// directory or a file of its own, and libtorrent keeps it as one.
			part := sanitiseComponent(toValidUTF8(entry.Key))
			if part == "" {
				part = unnamedComponent
			}
			if len(prefix) >= MaxPathSegments {
				return fmt.Errorf("%w: the file tree nests too deep", ErrBadPath)
			}

			if err := walk(
				entry.Value,
				append(append([]string(nil), prefix...), part),
				append(append([][]byte(nil), rawPrefix...), append([]byte(nil), entry.Key...)),
				validSoFar && utf8.Valid(entry.Key),
			); err != nil {
				return err
			}
		}

		return nil
	}

	return walk(tree, nil, nil, true)
}

// checkHybridConsistency rejects a hybrid whose two views describe different
// content.
//
// libtorrent refuses such a torrent, and so must this: the row would advertise
// one file list while the swarm serves another, and which one a client saw
// would depend on whether it spoke v2.
func (i *Info) checkHybridConsistency(v bencode.Value) error {
	shadow := &Info{Raw: i.Raw, Name: i.Name, RawName: i.RawName, Version: VersionV2}
	if err := shadow.readFileTree(v); err != nil {
		return fmt.Errorf("%w: %v", ErrInconsistent, err)
	}

	var v1Total uint64
	var v1Count int
	for _, f := range i.Files {
		if f.Padding {
			continue
		}
		v1Total += f.Size
		v1Count++
	}

	var v2Total uint64
	for _, f := range shadow.Files {
		v2Total += f.Size
	}

	if v1Count != len(shadow.Files) {
		return fmt.Errorf("%w: the v1 list has %d file(s), the file tree has %d",
			ErrInconsistent, v1Count, len(shadow.Files))
	}
	if v1Total != v2Total {
		return fmt.Errorf("%w: the v1 list totals %d bytes, the file tree totals %d",
			ErrInconsistent, v1Total, v2Total)
	}

	return nil
}

func (i *Info) hash() {
	switch i.Version {
	case VersionV2:
		i.InfoHashV2 = sha256.Sum256(i.Raw)
		i.HasV2 = true
	case VersionHybrid:
		i.InfoHashV1 = sha1.Sum(i.Raw)
		i.HasV1 = true
		i.InfoHashV2 = sha256.Sum256(i.Raw)
		i.HasV2 = true
	default:
		i.InfoHashV1 = sha1.Sum(i.Raw)
		i.HasV1 = true
	}
}

// isPadding recognises both padding conventions.
func isPadding(entry bencode.Value, file File) bool {
	// BEP 47: attr is a string of flags, 'p' meaning padding.
	if attr, ok := entry.GetString("attr"); ok && bytes.ContainsRune(attr, 'p') {
		return true
	}

	// BitComet predates BEP 47 and marks alignment files by name. libtorrent
	// recognises this too, and a torrent packed by BitComet is common enough in
	// a DHT crawl to matter.
	for _, raw := range file.RawPath {
		if strings.HasPrefix(string(raw), "_____padding_file_") {
			return true
		}
	}

	return strings.Contains(file.Path, "_____padding_file_")
}

// toValidUTF8 replaces invalid bytes so the result can be stored in a utf8mb4
// column and marshalled into a protobuf string.
//
// Both of those reject invalid UTF-8 outright — MariaDB with error 1366,
// protobuf-go by refusing to marshal — and a crawl turns up GBK and Shift-JIS
// names constantly, so a raw name reaching either would take down the write
// that carried it.
func toValidUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}

	return strings.ToValidUTF8(string(b), "�")
}

// sanitiseComponent strips what must never reach a file path or a display name:
// separators, traversal, control characters and surrounding whitespace.
func sanitiseComponent(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\':
			return '_'
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, s)

	s = strings.TrimSpace(s)

	// "." and ".." would escape the directory they are joined into. They are
	// dropped rather than renamed, since neither names real content.
	if s == "." || s == ".." {
		return ""
	}

	return s
}
