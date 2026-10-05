package ed2klink

import (
	"bytes"
	"errors"
	"testing"
)

func testHash() []byte {
	hash := make([]byte, HashSize)
	for i := range hash {
		hash[i] = byte(0xA0 + i)
	}

	return hash
}

func testAICH() []byte {
	aich := make([]byte, AICHSize)
	for i := range aich {
		aich[i] = byte(i * 7)
	}

	return aich
}

func TestBuild(t *testing.T) {
	cases := []struct {
		label string
		in    Link
		want  string
	}{
		{
			label: "a plain file",
			in:    Link{Name: "ubuntu-26.04.iso", Size: 4700000000, Hash: testHash()},
			want:  "ed2k://|file|ubuntu-26.04.iso|4700000000|A0A1A2A3A4A5A6A7A8A9AAABACADAEAF|/",
		},
		{
			label: "a name with a pipe, a slash and a space",
			in:    Link{Name: "a|b/c d.mkv", Size: 1, Hash: testHash()},
			want:  "ed2k://|file|a%7Cb%2Fc%20d.mkv|1|A0A1A2A3A4A5A6A7A8A9AAABACADAEAF|/",
		},
		{
			label: "with an AICH hash",
			in:    Link{Name: "x.bin", Size: 9728000, Hash: testHash(), AICH: testAICH()},
			want:  "ed2k://|file|x.bin|9728000|A0A1A2A3A4A5A6A7A8A9AAABACADAEAF|h=AADQ4FI4EMVDCOB7IZGVIW3CNFYHO7UF|/",
		},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %+v", c.in)

			got, err := Build(c.in)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			t.Logf("output: %s", got)

			if got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}

			back, err := Parse(got)
			if err != nil {
				t.Fatalf("Parse of a built link: %v", err)
			}
			t.Logf("parsed: %+v", back)

			if back.Name != c.in.Name || back.Size != c.in.Size ||
				!bytes.Equal(back.Hash, c.in.Hash) || !bytes.Equal(back.AICH, c.in.AICH) {
				t.Errorf("round trip lost something:\n in: %+v\nout: %+v", c.in, back)
			}
		})
	}
}

func TestBuildRejects(t *testing.T) {
	cases := []struct {
		label string
		in    Link
		want  error
	}{
		{"no name", Link{Name: " ", Size: 1, Hash: testHash()}, ErrNoName},
		{"a short hash", Link{Name: "x", Size: 1, Hash: testHash()[:15]}, ErrHash},
		{"a short AICH hash", Link{Name: "x", Size: 1, Hash: testHash(), AICH: testAICH()[:19]}, ErrHash},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %+v", c.in)

			_, err := Build(c.in)
			t.Logf("output: %v", err)

			if !errors.Is(err, c.want) {
				t.Errorf("got %v, want %v", err, c.want)
			}
		})
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		label string
		in    string
		want  Link
		err   error
	}{
		{
			label: "lowercase hex and an uppercase scheme",
			in:    "ED2K://|file|x.bin|42|a0a1a2a3a4a5a6a7a8a9aaabacadaeaf|/",
			want:  Link{Name: "x.bin", Size: 42, Hash: testHash()},
		},
		{
			label: "unknown trailing fields are skipped",
			in:    "ed2k://|file|x.bin|42|A0A1A2A3A4A5A6A7A8A9AAABACADAEAF|p=00:11|h=AADQ4FI4EMVDCOB7IZGVIW3CNFYHO7UF|/|sources,1.2.3.4:4662|/",
			want:  Link{Name: "x.bin", Size: 42, Hash: testHash(), AICH: testAICH()},
		},
		{
			label: "an unescaped percent sign stays in the name",
			in:    "ed2k://|file|100%.txt|42|A0A1A2A3A4A5A6A7A8A9AAABACADAEAF|/",
			want:  Link{Name: "100%.txt", Size: 42, Hash: testHash()},
		},
		{label: "a magnet", in: "magnet:?xt=urn:btih:00", err: ErrNotLink},
		{label: "a server link", in: "ed2k://|server|1.2.3.4|4661|/", err: ErrNotLink},
		{label: "a truncated link", in: "ed2k://|file|x.bin|42|", err: ErrNotLink},
		{label: "a size that is not a number", in: "ed2k://|file|x.bin|big|A0A1A2A3A4A5A6A7A8A9AAABACADAEAF|/", err: ErrSize},
		{label: "a hash of the wrong length", in: "ed2k://|file|x.bin|42|A0A1|/", err: ErrHash},
		{label: "a broken AICH hash", in: "ed2k://|file|x.bin|42|A0A1A2A3A4A5A6A7A8A9AAABACADAEAF|h=AAAA|/", err: ErrHash},
		{label: "an empty name", in: "ed2k://|file||42|A0A1A2A3A4A5A6A7A8A9AAABACADAEAF|/", err: ErrNoName},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %s", c.in)

			got, err := Parse(c.in)
			t.Logf("output: %+v err=%v", got, err)

			if c.err != nil {
				if !errors.Is(err, c.err) {
					t.Errorf("got %v, want %v", err, c.err)
				}

				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			if got.Name != c.want.Name || got.Size != c.want.Size ||
				!bytes.Equal(got.Hash, c.want.Hash) || !bytes.Equal(got.AICH, c.want.AICH) {
				t.Errorf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}
