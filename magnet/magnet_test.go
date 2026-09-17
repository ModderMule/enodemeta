package magnet

import (
	"bytes"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func v1Hash() []byte {
	h := make([]byte, 20)
	for i := range h {
		h[i] = byte(i)
	}

	return h
}

func v2Hash() []byte {
	h := make([]byte, 32)
	for i := range h {
		h[i] = byte(0xA0 + i)
	}

	return h
}

func TestBuildV1(t *testing.T) {
	link := Link{V1: v1Hash(), Name: "Some Release 2026", Size: 1 << 30}
	t.Logf("input:  %+v", link)

	got, err := link.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Logf("output: %s", got)

	const wantXT = "xt=urn:btih:000102030405060708090A0B0C0D0E0F10111213"
	if !strings.Contains(got, wantXT) {
		t.Errorf("the v1 hash must appear uppercase as btih: got %s", got)
	}
	if !strings.Contains(got, "dn=Some+Release+2026") {
		t.Errorf("the name must be escaped: got %s", got)
	}
	if !strings.Contains(got, "xl=1073741824") {
		t.Errorf("the size must be exact: got %s", got)
	}

	// Whatever else changes, the result has to parse as a URI.
	if _, err := url.Parse(got); err != nil {
		t.Errorf("a magnet must be a valid URI: %v", err)
	}
}

// TestBuildHybrid pins the rule from eMuleQt §7.1: a hybrid carries both hashes,
// with the v1 one first so a client reading only the first xt joins the swarm
// the row was keyed by.
func TestBuildHybrid(t *testing.T) {
	link := Link{V1: v1Hash(), V2: v2Hash(), Name: "Hybrid"}
	t.Logf("input:  a hybrid torrent with both infohashes")

	got, err := link.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Logf("output: %s", got)

	btih := strings.Index(got, "urn:btih:")
	btmh := strings.Index(got, "urn:btmh:")
	if btih < 0 || btmh < 0 {
		t.Fatalf("a hybrid magnet carries both xt values: got %s", got)
	}
	if btih > btmh {
		t.Error("the v1 hash must come first")
	}

	// The v2 hash is a multihash: 0x12 for sha2-256, 0x20 for its length.
	if !strings.Contains(got, "urn:btmh:1220a0a1a2") {
		t.Errorf("the v2 hash must be a sha2-256 multihash: got %s", got)
	}
}

func TestBuildV2Only(t *testing.T) {
	got, err := Link{V2: v2Hash()}.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Logf("output: %s", got)

	if strings.Contains(got, "btih") {
		t.Errorf("a v2-only torrent has no v1 hash to advertise: got %s", got)
	}
}

// TestSelectOnly covers what makes a per-file row actionable: BEP 53's index
// list, which tells the engine to fetch just the file the row stood for.
func TestSelectOnly(t *testing.T) {
	cases := []struct {
		label string
		in    []uint32
		want  string
	}{
		{"a single file", []uint32{3}, "so=3"},
		{"a run collapses into a range", []uint32{0, 1, 2}, "so=0-2"},
		{"runs and singles together", []uint32{0, 1, 2, 5, 7, 8}, "so=0-2,5,7-8"},
		{"out of order input is sorted", []uint32{5, 1, 0}, "so=0-1,5"},
		{"duplicates say nothing new", []uint32{2, 2, 2}, "so=2"},
		{"no selection omits the key", nil, ""},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %v", c.in)

			got, err := Link{V1: v1Hash(), SelectOnly: c.in}.Build()
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			t.Logf("output: %s", got)

			if c.want == "" {
				if strings.Contains(got, "so=") {
					t.Errorf("an empty selection must omit so: got %s", got)
				}

				return
			}
			if !strings.Contains(got, c.want) {
				t.Errorf("got %s, want it to contain %s", got, c.want)
			}
		})
	}
}

func TestTrackers(t *testing.T) {
	link := Link{V1: v1Hash(), Trackers: []string{"udp://tracker.example:6969/announce", "", "  "}}
	t.Logf("input:  one real tracker and two blanks")

	got, err := link.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Logf("output: %s", got)

	if strings.Count(got, "tr=") != 1 {
		t.Errorf("blank trackers must be dropped: got %s", got)
	}
	if !strings.Contains(got, "tr=udp%3A%2F%2Ftracker.example%3A6969%2Fannounce") {
		t.Errorf("a tracker URL must be escaped: got %s", got)
	}
}

func TestBuildRejectsNoHash(t *testing.T) {
	_, err := Link{Name: "nothing to identify"}.Build()
	if err == nil {
		t.Fatal("a magnet with no hash must be rejected")
	}
	t.Logf("output: %v", err)

	if !errors.Is(err, ErrNoHash) {
		t.Errorf("got %v, want it to wrap %v", err, ErrNoHash)
	}

	// A hash of the wrong length is the same mistake with a different cause.
	if _, err := (Link{V1: make([]byte, 19)}).Build(); err == nil {
		t.Error("a 19-byte v1 hash is not a v1 hash")
	}
}

// TestParseRoundTrip is the contract between the two halves of the package:
// whatever Build writes, Parse reads back unchanged.
func TestParseRoundTrip(t *testing.T) {
	cases := []struct {
		label string
		link  Link
	}{
		{"v1 with a name and size", Link{V1: v1Hash(), Name: "Some Release 2026", Size: 1 << 30}},
		{"hybrid", Link{V1: v1Hash(), V2: v2Hash(), Name: "Hybrid & Co"}},
		{"v2 only", Link{V2: v2Hash()}},
		{"trackers and a selection", Link{
			V1:         v1Hash(),
			Trackers:   []string{"udp://tracker.example:6969/announce", "https://t.example/a?x=1"},
			SelectOnly: []uint32{0, 1, 2, 5},
		}},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			uri, err := c.link.Build()
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			t.Logf("input:  %s", uri)

			got, err := Parse(uri)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			t.Logf("output: %+v", got)

			if !reflect.DeepEqual(got, c.link) {
				t.Errorf("round trip changed the link:\n got %+v\nwant %+v", got, c.link)
			}
		})
	}
}

// TestParseEncodings covers the links Build never writes but other clients do.
func TestParseEncodings(t *testing.T) {
	v1 := v1Hash()
	v2 := v2Hash()

	cases := []struct {
		label  string
		in     string
		wantV1 []byte
		wantV2 []byte
		name   string
	}{
		{"lowercase hex", "magnet:?xt=urn:btih:000102030405060708090a0b0c0d0e0f10111213", v1, nil, ""},
		{"base32", "magnet:?xt=urn:btih:AAAQEAYEAUDAOCAJBIFQYDIOB4IBCEQT&dn=old+client", v1, nil, "old client"},
		{"lowercase base32", "magnet:?xt=urn:btih:aaaqeayeaudaocajbifqydiob4ibceqt", v1, nil, ""},
		{"uppercase scheme and key", "MAGNET:?XT=urn:BTIH:000102030405060708090A0B0C0D0E0F10111213", v1, nil, ""},
		{"indexed keys", "magnet:?xt.1=urn:btih:000102030405060708090A0B0C0D0E0F10111213&xt.2=urn:btmh:1220a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf", v1, v2, ""},
		{"surrounding space", "  magnet:?xt=urn:btih:000102030405060708090A0B0C0D0E0F10111213&dn=a%20b  ", v1, nil, "a b"},
		{"bad escape kept as written", "magnet:?xt=urn:btih:000102030405060708090A0B0C0D0E0F10111213&dn=100%", v1, nil, "100%"},
		{"a non-sha256 btmh is ignored", "magnet:?xt=urn:btih:000102030405060708090A0B0C0D0E0F10111213&xt=urn:btmh:1114a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3", v1, nil, ""},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", c.in)

			got, err := Parse(c.in)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			t.Logf("output: v1=%x v2=%x name=%q", got.V1, got.V2, got.Name)

			if !bytes.Equal(got.V1, c.wantV1) || !bytes.Equal(got.V2, c.wantV2) {
				t.Errorf("got v1=%x v2=%x, want v1=%x v2=%x", got.V1, got.V2, c.wantV1, c.wantV2)
			}
			if got.Name != c.name {
				t.Errorf("got name %q, want %q", got.Name, c.name)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	cases := []struct {
		label string
		in    string
		want  error
	}{
		{"not a magnet", "http://example.com/?xt=urn:btih:000102030405060708090A0B0C0D0E0F10111213", ErrNotMagnet},
		{"empty", "", ErrNotMagnet},
		{"no xt", "magnet:?dn=nothing", ErrNoHash},
		{"short btih", "magnet:?xt=urn:btih:0001020304", ErrNoHash},
		{"btih that is not hex", "magnet:?xt=urn:btih:zz0102030405060708090A0B0C0D0E0F10111213", ErrNoHash},
		{"ed2k only", "magnet:?xt=urn:ed2k:31D6CFE0D16AE931B73C59D7E0C089C0", ErrNoHash},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", c.in)

			_, err := Parse(c.in)
			t.Logf("output: %v", err)

			if !errors.Is(err, c.want) {
				t.Errorf("got %v, want it to wrap %v", err, c.want)
			}
		})
	}
}

// TestParseSelectOnlyIsBounded keeps a few characters of text from becoming a
// multi-gigabyte allocation.
func TestParseSelectOnlyIsBounded(t *testing.T) {
	cases := []struct {
		in   string
		want []uint32
	}{
		{"0-2,5", []uint32{0, 1, 2, 5}},
		{"0-4294967295", nil},
		{"5-2", nil},
		{"1,x", nil},
	}

	for _, c := range cases {
		uri := "magnet:?xt=urn:btih:000102030405060708090A0B0C0D0E0F10111213&so=" + c.in
		t.Logf("input:  so=%s", c.in)

		got, err := Parse(uri)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		t.Logf("output: %v", got.SelectOnly)

		if !reflect.DeepEqual(got.SelectOnly, c.want) {
			t.Errorf("so=%s: got %v, want %v", c.in, got.SelectOnly, c.want)
		}
	}
}
