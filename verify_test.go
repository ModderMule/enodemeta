package enodemeta

import (
	"errors"
	"strings"
	"testing"

	"github.com/ModderMule/enodemeta/bencode"
	"github.com/ModderMule/enodemeta/metahash"
	"github.com/ModderMule/enodemeta/torrentmeta"
)

func infoDict(t *testing.T, name string) []byte {
	t.Helper()

	return bencode.MustEncode(map[string]any{
		"name":         name,
		"piece length": 262144,
		"pieces":       strings.Repeat("01234567890123456789", 4),
		"length":       1 << 30,
	})
}

// TestVerifyMetaFileRoundTrip walks the whole loop the feature rests on: a
// crawler derives an identity, a server mints a hash from it, and a client
// checks the bytes it fetched against that hash.
func TestVerifyMetaFileRoundTrip(t *testing.T) {
	raw := infoDict(t, "Some.Release.2026")

	info, err := torrentmeta.ParseInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	kind, identity := info.Identity()
	hash, err := metahash.Mint(metahash.MintInput{
		Kind:      kind,
		Identity:  identity,
		FileIndex: 0,
		FileCount: info.SelectableCount(),
	})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	t.Logf("input:  identity=%x -> hash=%s", identity[:6], hash)

	// The bare info dictionary, as BEP 9 delivers it.
	if err := VerifyMetaFile(hash, raw); err != nil {
		t.Errorf("the bytes the hash was minted from must verify: %v", err)
	}

	// The same content wrapped as a .torrent, as the API serves it.
	file, err := torrentmeta.BuildTorrentFile(raw, []string{"udp://tracker.example:6969"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := VerifyMetaFile(hash, file); err != nil {
		t.Errorf("a wrapped .torrent must verify the same way: %v", err)
	}
	t.Logf("output: both the bare info dictionary and the wrapped .torrent verify")
}

// TestVerifyMetaFileRejectsSubstitution is the attack the fold exists to catch:
// a different origin serving some other torrent in place of the advertised one.
func TestVerifyMetaFileRejectsSubstitution(t *testing.T) {
	advertised := infoDict(t, "Advertised.Release")
	substituted := infoDict(t, "Something.Else.Entirely")

	info, err := torrentmeta.ParseInfo(advertised)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	kind, identity := info.Identity()

	hash, err := metahash.Build(kind, 0, 0, identity)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Logf("input:  hash of %q, bytes of %q", "Advertised.Release", "Something.Else.Entirely")

	err = VerifyMetaFile(hash, substituted)
	if err == nil {
		t.Fatal("a substituted metafile must be rejected")
	}
	t.Logf("output: %v", err)

	if !errors.Is(err, ErrVerification) {
		t.Errorf("got %v, want it to wrap %v", err, ErrVerification)
	}
}

// TestVerifyMetaFileRejectsTampering covers the subtler case: the same torrent
// with one byte changed. The infohash moves, so the fold no longer matches.
func TestVerifyMetaFileRejectsTampering(t *testing.T) {
	raw := infoDict(t, "Release")

	info, err := torrentmeta.ParseInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	kind, identity := info.Identity()

	hash, err := metahash.Build(kind, 0, 0, identity)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	tampered := append([]byte(nil), raw...)
	tampered = []byte(strings.Replace(string(tampered), "7:Release", "7:Relaese", 1))
	t.Logf("input:  the same torrent with two letters swapped in its name")

	if err := VerifyMetaFile(hash, tampered); err == nil {
		t.Fatal("tampered bytes must be rejected")
	} else {
		t.Logf("output: %v", err)
	}
}

func TestVerifyMetaFileKindMismatch(t *testing.T) {
	raw := infoDict(t, "A.V1.Torrent")

	// A hash claiming v2 over a v1 torrent's bytes. Its 32-byte identity slot
	// cannot hold a v1 hash, so the claim is checkable on its own.
	identity := make([]byte, 32)
	hash, err := metahash.Build(metahash.KindBTV2, 0, 0, identity)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Logf("input:  a bt-v2 hash over a v1 torrent")

	err = VerifyMetaFile(hash, raw)
	if err == nil {
		t.Fatal("a kind mismatch must be rejected")
	}
	t.Logf("output: %v", err)

	if !errors.Is(err, ErrVerification) {
		t.Errorf("got %v, want it to wrap %v", err, ErrVerification)
	}
}

func TestVerifyMetaFileRejectsGarbage(t *testing.T) {
	raw := infoDict(t, "Release")
	info, err := torrentmeta.ParseInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	kind, identity := info.Identity()

	hash, err := metahash.Build(kind, 0, 0, identity)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	for _, c := range []struct {
		label string
		in    []byte
	}{
		{"empty bytes", nil},
		{"not bencode at all", []byte("<html>404</html>")},
		{"bencode that is not a torrent", bencode.MustEncode(map[string]any{"a": 1})},
	} {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", c.in)

			if err := VerifyMetaFile(hash, c.in); err == nil {
				t.Errorf("%s must be rejected", c.label)
			} else {
				t.Logf("output: %v", err)
			}
		})
	}
}

// TestVerifyNZBMetaFile walks the whole NZB half of the contract: a daemon mints
// a row from a document, a consumer fetches the bytes and folds them, and the
// two agree.
func TestVerifyNZBMetaFile(t *testing.T) {
	nzb := []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">` + "\n" +
		`  <head><meta type="name">Some.Release</meta></head>` + "\n" +
		`  <file poster="p@example.invalid" date="1700000000" subject="Some.Release.part01.rar (1/2)">` + "\n" +
		`    <groups><group>alt.binaries.test</group></groups>` + "\n" +
		`    <segments>` + "\n" +
		`      <segment bytes="700000" number="1">a1@example.invalid</segment>` + "\n" +
		`      <segment bytes="700000" number="2">a2@example.invalid</segment>` + "\n" +
		`    </segments>` + "\n" +
		`  </file>` + "\n" +
		`</nzb>` + "\n")
	t.Logf("input:  a %d-byte NZB listing 2 segments", len(nzb))

	identity, err := IdentityOf(metahash.KindNZB, nzb)
	if err != nil {
		t.Fatalf("deriving the NZB identity: %v", err)
	}
	t.Logf("output: identity %x", identity)

	if len(identity) != metahash.KindNZB.IdentityLen() {
		t.Fatalf("an nzb identity is %d bytes, got %d", metahash.KindNZB.IdentityLen(), len(identity))
	}

	hash, err := metahash.Mint(metahash.MintInput{
		Kind:      metahash.KindNZB,
		Identity:  identity,
		FileIndex: 0,
		FileCount: 1,
	})
	if err != nil {
		t.Fatalf("minting the row: %v", err)
	}
	t.Logf("output: meta hash %s", hash)

	if err := VerifyMetaFile(hash, nzb); err != nil {
		t.Errorf("the bytes the identity came from must verify: %v", err)
	}

	// Redacting the head must not move the digest: that is what lets a daemon
	// serve a password-stripped copy a client can still check.
	redacted := []byte(strings.Replace(string(nzb),
		`  <head><meta type="name">Some.Release</meta></head>`+"\n", "", 1))
	if err := VerifyMetaFile(hash, redacted); err != nil {
		t.Errorf("a copy with <head> stripped must still verify: %v", err)
	}
	t.Logf("output: the %d-byte redacted copy verifies against the same hash", len(redacted))

	// A different article list is a different release.
	other := []byte(strings.Replace(string(nzb), "a2@example.invalid", "a3@example.invalid", 1))
	if err := VerifyMetaFile(hash, other); err == nil {
		t.Error("a document with a different article must not verify")
	} else {
		t.Logf("output: a changed article is rejected: %v", err)
	}
}

// TestIdentityOfUnknownKind pins that a kind from a later version of the scheme
// is refused rather than guessed at.
func TestIdentityOfUnknownKind(t *testing.T) {
	_, err := IdentityOf(metahash.Kind(7), []byte("anything"))
	if err == nil {
		t.Fatal("an unknown kind must be refused")
	}
	t.Logf("output: %v", err)

	if !errors.Is(err, ErrKindUnsupported) {
		t.Errorf("got %v, want it to wrap %v", err, ErrKindUnsupported)
	}
}
