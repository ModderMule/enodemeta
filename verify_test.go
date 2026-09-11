package enodemeta

import (
	"errors"
	"strings"
	"testing"

	"github.com/ModderMule/torrent-crawler/pkg/enodemeta/bencode"
	"github.com/ModderMule/torrent-crawler/pkg/enodemeta/metahash"
	"github.com/ModderMule/torrent-crawler/pkg/enodemeta/torrentmeta"
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

// TestIdentityOfNZB pins the one gap, so it fails loudly rather than silently
// accepting a row nothing can check.
func TestIdentityOfNZB(t *testing.T) {
	_, err := IdentityOf(metahash.KindNZB, []byte("<nzb/>"))
	if err == nil {
		t.Fatal("NZB identity is not implemented yet and must say so")
	}
	t.Logf("output: %v", err)

	if !errors.Is(err, ErrKindUnsupported) {
		t.Errorf("got %v, want it to wrap %v", err, ErrKindUnsupported)
	}
}
