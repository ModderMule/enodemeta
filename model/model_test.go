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
