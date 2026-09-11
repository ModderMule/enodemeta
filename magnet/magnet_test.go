package magnet

import (
	"errors"
	"net/url"
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
