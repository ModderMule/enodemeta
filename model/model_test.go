package model

import (
	"errors"
	"testing"

	"github.com/ModderMule/enodemeta/metahash"
)

func validEntry() Entry {
	identity := make([]byte, 20)
	for i := range identity {
		identity[i] = byte(i + 1)
	}

	return Entry{
		Kind:      metahash.KindBTV1,
		Identity:  identity,
		Name:      "Some.Release.2026.1080p.mkv",
		CatalogID: "bt:v1:0102030405060708090A0B0C0D0E0F1011121314",
		FileIndex: 0,
		FileCount: 1,
		Size:      700 << 20,
		TotalSize: 700 << 20,
	}
}

// validNZBEntry is the Usenet shape: a 32-byte canonical digest, an nzb:
// catalogue id, and no magnet.
func validNZBEntry() Entry {
	identity := make([]byte, 32)
	for i := range identity {
		identity[i] = byte(0x20 + i)
	}

	return Entry{
		Kind:              metahash.KindNZB,
		Identity:          identity,
		Name:              "Some.Release.2026.1080p",
		CatalogID:         "nzb:202122232425262728292A2B2C2D2E2F303132333435363738393A3B3C3D3E3F",
		FileIndex:         0,
		FileCount:         3,
		Size:              700 << 20,
		TotalSize:         2100 << 20,
		PathAuthoritative: true,
	}
}

func TestValidate(t *testing.T) {
	entry := validEntry()
	t.Logf("input:  %s %q", entry.Kind, entry.Name)

	if err := entry.Validate(); err != nil {
		t.Fatalf("a well-formed entry must validate: %v", err)
	}
	t.Logf("output: accepted")
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		label  string
		mutate func(e *Entry)
	}{
		{"an unknown kind", func(e *Entry) { e.Kind = metahash.KindUnspecified }},
		{"an identity of the wrong length", func(e *Entry) { e.Identity = e.Identity[:19] }},
		{"no identity at all", func(e *Entry) { e.Identity = nil }},
		{"no catalog id", func(e *Entry) { e.CatalogID = "  " }},
		{"no name", func(e *Entry) { e.Name = "" }},
		{"a file larger than its release", func(e *Entry) { e.Size = e.TotalSize + 1 }},
		{"a whole-set row for a single-file release", func(e *Entry) { e.FileIndex = metahash.FileIndexWholeSet32 }},
		{"an index that collides with the whole-set marker", func(e *Entry) {
			e.FileIndex = 0xFFFF
			e.FileCount = 70000
		}},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			entry := validEntry()
			c.mutate(&entry)
			t.Logf("input:  %+v", entry)

			err := entry.Validate()
			if err == nil {
				t.Fatalf("%s must be rejected", c.label)
			}
			t.Logf("output: %v", err)

			if !errors.Is(err, ErrInvalidEntry) {
				t.Errorf("got %v, want it to wrap %v", err, ErrInvalidEntry)
			}
		})
	}
}

// TestValidateNZB pins the two rules an NZB row adds, which only bite once a
// second kind exists: a Usenet release has no magnet, and a client that saw
// FlagMagnetOnly on one would offer a download that cannot start.
func TestValidateNZB(t *testing.T) {
	entry := validNZBEntry()
	t.Logf("input:  %s %q", entry.Kind, entry.Name)

	if err := entry.Validate(); err != nil {
		t.Fatalf("a well-formed nzb entry must validate: %v", err)
	}
	t.Logf("output: accepted")

	for _, c := range []struct {
		label  string
		mutate func(e *Entry)
	}{
		{"a magnet", func(e *Entry) { e.Magnet = "magnet:?xt=urn:btih:0102030405060708090A0B0C0D0E0F1011121314" }},
		{"the magnet-only flag", func(e *Entry) { e.Flags |= FlagMagnetOnly }},
		{"a 20-byte identity", func(e *Entry) { e.Identity = e.Identity[:20] }},
	} {
		t.Run(c.label, func(t *testing.T) {
			entry := validNZBEntry()
			c.mutate(&entry)
			t.Logf("input:  an nzb entry with %s", c.label)

			err := entry.Validate()
			if err == nil {
				t.Fatalf("%s must be rejected on an nzb row", c.label)
			}
			t.Logf("output: %v", err)

			if !errors.Is(err, ErrInvalidEntry) {
				t.Errorf("got %v, want it to wrap %v", err, ErrInvalidEntry)
			}
		})
	}

	// The same two things are fine on a torrent row, which is what makes these
	// rules about the kind rather than about the fields.
	torrent := validEntry()
	torrent.Magnet = "magnet:?xt=urn:btih:0102030405060708090A0B0C0D0E0F1011121314"
	torrent.Flags |= FlagMagnetOnly
	if err := torrent.Validate(); err != nil {
		t.Errorf("a torrent row may carry a magnet: %v", err)
	}
	t.Logf("output: the same fields are accepted on a bt-v1 row")
}

// TestMint is the server's half: a daemon sends an entry with no hash, and the
// server fills it in from the identity the daemon supplied.
func TestMint(t *testing.T) {
	entry := validEntry()
	if len(entry.MetaHash) != 0 {
		t.Fatal("an entry from a daemon carries no hash")
	}

	if err := entry.Mint(); err != nil {
		t.Fatalf("mint: %v", err)
	}
	t.Logf("input:  identity=%x", entry.Identity[:6])

	hash, err := metahash.FromBytes(entry.MetaHash)
	if err != nil {
		t.Fatalf("the minted hash must be 16 bytes: %v", err)
	}
	t.Logf("output: %s", hash)

	if !hash.VerifyIdentity(entry.Identity) {
		t.Error("the minted hash must verify against the identity it came from")
	}
}

// TestMintCarriesProtectedFlag pins the one place the entry's flag bitfield and
// the hash's flag nibble have to agree.
func TestMintCarriesProtectedFlag(t *testing.T) {
	entry := validEntry()
	entry.Flags = FlagPasswordProtected
	t.Logf("input:  flags=0x%X", entry.Flags)

	if err := entry.Mint(); err != nil {
		t.Fatalf("mint: %v", err)
	}

	parsed, err := metahash.Parse(entry.MetaHash)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("output: hash flags=%s", parsed.Flags)

	if parsed.Flags&metahash.FlagProtected == 0 {
		t.Error("a password-protected release must set the hash's protected flag")
	}
}

func TestWholeSet(t *testing.T) {
	entry := validEntry()
	entry.FileCount = 5
	entry.FileIndex = metahash.FileIndexWholeSet32

	if !entry.WholeSet() {
		t.Error("0xFFFFFFFF is the whole-set marker")
	}

	entry.FileIndex = 0
	if entry.WholeSet() {
		t.Error("index 0 is a file, not the whole set")
	}
	t.Logf("output: the whole-set marker is recognised and index 0 is not")
}
