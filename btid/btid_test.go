package btid

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestFormatAndParse(t *testing.T) {
	var v1 [20]byte
	var v2 [32]byte
	var md4 [16]byte
	for i := range v1 {
		v1[i] = byte(i)
	}
	for i := range v2 {
		v2[i] = byte(0xF0 - i)
	}
	for i := range md4 {
		md4[i] = byte(i * 2)
	}

	cases := []struct {
		label   string
		id      string
		network Network
		hash    []byte
	}{
		{"a v1 torrent", V1(v1), NetworkBTV1, v1[:]},
		{"a v2 torrent", V2(v2), NetworkBTV2, v2[:]},
		{"an ed2k file", PrefixED2K + strings.ToUpper("00020406080a0c0e10121416181a1c1e"), NetworkED2K, md4[:]},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %s", c.id)

			id, err := Parse(c.id)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			t.Logf("output: network=%s hash=%x", id.Network, id.Hash)

			if id.Network != c.network {
				t.Errorf("network: got %s, want %s", id.Network, c.network)
			}
			if !bytes.Equal(id.Hash, c.hash) {
				t.Errorf("hash: got %x, want %x", id.Hash, c.hash)
			}
			if id.String() != c.id {
				t.Errorf("round trip: got %s, want %s", id.String(), c.id)
			}
		})
	}
}

// TestUppercase pins rule 1. A lowercase id parses — a peer may send one — but
// everything this project emits is uppercase, so a comparison against a stored
// id cannot fail on case alone.
func TestUppercase(t *testing.T) {
	var v1 [20]byte
	for i := range v1 {
		v1[i] = byte(0xAB)
	}

	formatted := V1(v1)
	t.Logf("input:  %s", formatted)

	// The scheme prefix is lowercase and the hash is uppercase, so only the
	// hash half is checked here.
	hexPart := strings.TrimPrefix(formatted, PrefixBTV1)
	if hexPart != strings.ToUpper(hexPart) {
		t.Errorf("the hash is emitted uppercase, got %q", hexPart)
	}
	if !strings.HasPrefix(formatted, PrefixBTV1) {
		t.Errorf("the scheme prefix stays lowercase, got %q", formatted)
	}

	lower := strings.ToLower(formatted)
	id, err := Parse(lower)
	if err != nil {
		t.Fatalf("a lowercase id must still parse: %v", err)
	}
	t.Logf("output: %s normalised to %s", lower, id.String())

	if id.String() != formatted {
		t.Errorf("parsing must normalise the case: got %s, want %s", id.String(), formatted)
	}
}

// TestHybridUsesV1 pins rule 2: the v1 hash is the swarm's identity, so a
// hybrid torrent is named by it even though it also has a v2 hash.
func TestHybridUsesV1(t *testing.T) {
	var v1 [20]byte
	var v2 [32]byte
	v1[0] = 0x11
	v2[0] = 0x22

	id, err := Torrent(&v1, &v2)
	if err != nil {
		t.Fatalf("torrent: %v", err)
	}
	t.Logf("input:  a hybrid torrent with both hashes")
	t.Logf("output: %s", id)

	if !strings.HasPrefix(id, PrefixBTV1) {
		t.Errorf("a hybrid must be named by its v1 hash, got %s", id)
	}

	// A v2-only torrent has no choice.
	v2Only, err := Torrent(nil, &v2)
	if err != nil {
		t.Fatalf("torrent: %v", err)
	}
	if !strings.HasPrefix(v2Only, PrefixBTV2) {
		t.Errorf("a v2-only torrent must be named by its v2 hash, got %s", v2Only)
	}

	if _, err := Torrent(nil, nil); err == nil {
		t.Error("a torrent with no infohash at all must be an error")
	}
}

func TestParseBareMD4(t *testing.T) {
	const bare = "0102030405060708090A0B0C0D0E0F10"
	t.Logf("input:  %s (no scheme, as the pre-namespace IPC calls send)", bare)

	id, err := Parse(bare)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("output: network=%s", id.Network)

	if id.Network != NetworkED2K {
		t.Errorf("an id with no colon is an eD2K MD4, got %s", id.Network)
	}
}

// TestNZBDigest walks the catalogue-id form: a daemon formats it, a consumer
// echoes it back, and FetchMetaFile turns it into a store key with no database
// round trip.
func TestNZBDigest(t *testing.T) {
	var digest [32]byte
	for i := range digest {
		digest[i] = byte(0x10 + i)
	}

	id := NZBDigest(digest)
	t.Logf("input:  %x\noutput: %s", digest, id)

	if want := "nzb:101112131415161718191A1B1C1D1E1F202122232425262728292A2B2C2D2E2F"; id != want {
		t.Errorf("got %s, want %s", id, want)
	}

	parsed, err := Parse(id)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Network != NetworkNZB || !bytes.Equal(parsed.Hash, digest[:]) {
		t.Errorf("a digest-form id must decode to its bytes: %+v", parsed)
	}
	if parsed.String() != id {
		t.Errorf("round trip: got %s, want %s", parsed.String(), id)
	}

	got, err := ParseNZB(id)
	if err != nil {
		t.Fatalf("ParseNZB: %v", err)
	}
	if got != digest {
		t.Errorf("ParseNZB: got %x, want %x", got, digest)
	}

	// The reason case is normalised here and not at each call site: a daemon
	// emits uppercase and hand-rolled client hex is lowercase, and before this
	// the two compared unequal with nothing logged.
	lower, err := ParseNZB(strings.ToLower(id))
	if err != nil {
		t.Fatalf("a lowercase id must parse: %v", err)
	}
	if lower != digest {
		t.Errorf("case must not change the digest: got %x", lower)
	}
	t.Logf("output: the lowercase spelling decodes to the same digest")
}

func TestParseNZBRejectsATransferID(t *testing.T) {
	const uuid = "nzb:6f8c4a1e-0f2b-4d3c-9a7e-1b2c3d4e5f60"
	t.Logf("input:  %s", uuid)

	_, err := ParseNZB(uuid)
	if err == nil {
		t.Fatal("a uuid names a download, not a stored release, and must be refused")
	}
	t.Logf("output: %v", err)

	if !errors.Is(err, ErrLength) {
		t.Errorf("got %v, want it to wrap %v", err, ErrLength)
	}

	// Parse still accepts it, because both forms have to keep parsing.
	if _, err := Parse(uuid); err != nil {
		t.Errorf("Parse must still accept a transfer id: %v", err)
	}
}

// TestParseNZBOpaqueStaysOpaque pins the fall-through: the digest form is
// recognised by being 64 hex characters, and anything else of that length stays
// the opaque value Parse has always returned rather than becoming an error.
// Guessing that a malformed value "meant" to be a digest is how a client ends up
// asking for a release that does not exist.
func TestParseNZBOpaqueStaysOpaque(t *testing.T) {
	id := "nzb:" + strings.Repeat("ZZ", 32)
	t.Logf("input:  %s", id)

	parsed, err := Parse(id)
	if err != nil {
		t.Fatalf("an opaque nzb value must parse: %v", err)
	}
	t.Logf("output: network=%s hash=%d bytes value=%s", parsed.Network, len(parsed.Hash), parsed.Value)

	if parsed.Network != NetworkNZB || len(parsed.Hash) != 0 || parsed.String() != id {
		t.Errorf("got %+v", parsed)
	}

	if _, err := ParseNZB(id); err == nil {
		t.Error("ParseNZB must still refuse it: it names no stored release")
	}
}

func TestParseNZBUUID(t *testing.T) {
	const id = "nzb:6f8c4a1e-0f2b-4d3c-9a7e-1b2c3d4e5f60"
	t.Logf("input:  %s", id)

	parsed, err := Parse(id)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("output: network=%s value=%s", parsed.Network, parsed.Value)

	if parsed.Network != NetworkNZB || parsed.String() != id {
		t.Errorf("an nzb id is opaque and must round-trip: got %+v", parsed)
	}
	if len(parsed.Hash) != 0 {
		t.Error("an nzb id carries no hash")
	}
}

func TestParseRejects(t *testing.T) {
	cases := []struct {
		label string
		in    string
		want  error
	}{
		{"empty", "", ErrFormat},
		{"an unknown scheme", "magnet:?xt=urn:btih:abc", ErrFormat},
		{"a v1 id that is too short", "bt:v1:0102", ErrLength},
		{"a v1 id that is too long", "bt:v1:" + strings.Repeat("AB", 21), ErrLength},
		{"a v2 id of v1 length", "bt:v2:" + strings.Repeat("AB", 20), ErrLength},
		{"non-hex characters", "bt:v1:" + strings.Repeat("ZZ", 20), ErrFormat},
		{"an nzb id with no value", "nzb:", ErrFormat},
		{"a bare string that is not an MD4", "hello", ErrFormat},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", c.in)

			_, err := Parse(c.in)
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
