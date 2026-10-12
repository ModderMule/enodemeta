package weblinks

import (
	"errors"
	"strings"
	"testing"

	"github.com/ModderMule/enodemeta/metahash"
)

func TestCanonical(t *testing.T) {
	cases := []struct {
		label string
		in    string
		want  string
	}{
		{"already canonical", "https://host.test/f/abc", "https://host.test/f/abc"},
		{"scheme and host are lowered", "HTTPS://Host.TEST/F/Abc", "https://host.test/F/Abc"},
		{"the path keeps its case", "http://host.test/AbC?Id=Q", "http://host.test/AbC?Id=Q"},
		{"a fragment is kept", "https://host.test/f/abc#player", "https://host.test/f/abc#player"},
		{"a fragment keeps its case", "HTTPS://Host.TEST/file/AbCd#KeY-_9", "https://host.test/file/AbCd#KeY-_9"},
		{"an empty fragment is dropped", "https://host.test/f/abc#", "https://host.test/f/abc"},
		{"a fragment that is all there is", "https://host.test/#!AbCd!KeY", "https://host.test/#!AbCd!KeY"},
		{"a fragment with no path gets one", "https://Host.test#!AbCd!KeY", "https://host.test/#!AbCd!KeY"},
		{"a second hash belongs to the fragment", "https://host.test/f#a#b", "https://host.test/f#a#b"},
		{"a slash in a fragment is not a path", "https://Host.test#/2/file/AbC/", "https://host.test/#/2/file/AbC/"},
		{"the default https port is dropped", "https://host.test:443/f", "https://host.test/f"},
		{"the default http port is dropped", "http://host.test:80/f", "http://host.test/f"},
		{"another port stays", "https://host.test:8443/f", "https://host.test:8443/f"},
		{"443 is not http's default", "http://host.test:443/f", "http://host.test:443/f"},
		{"an empty port is dropped", "https://host.test:/f", "https://host.test/f"},
		{"no path becomes a slash", "https://host.test", "https://host.test/"},
		{"a query with no path gets one", "https://host.test?id=1", "https://host.test/?id=1"},
		{"percent escapes are kept as written", "https://host.test/a%2fb%2Fc", "https://host.test/a%2fb%2Fc"},
		{"the query is not reordered", "https://host.test/?b=2&a=1", "https://host.test/?b=2&a=1"},
		{"an IPv6 literal keeps its colons", "http://[2001:DB8::1]/f", "http://[2001:db8::1]/f"},
		{"an IPv6 literal loses a default port", "http://[2001:db8::1]:80/f", "http://[2001:db8::1]/f"},
		{"surrounding blanks are trimmed", " \thttps://host.test/f\r", "https://host.test/f"},
		{"an onion host", "http://ExampleOnionAddress.onion/t/1", "http://exampleonionaddress.onion/t/1"},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", c.in)

			got, err := Canonical(c.in)
			if err != nil {
				t.Fatalf("canonical: %v", err)
			}
			t.Logf("output: %q", got)

			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}

			again, err := Canonical(got)
			if err != nil || again != got {
				t.Errorf("not idempotent: %q then %q (%v)", got, again, err)
			}
		})
	}
}

func TestCanonicalRejects(t *testing.T) {
	cases := []struct {
		label string
		in    string
	}{
		{"no scheme", "host.test/f"},
		{"an ftp link", "ftp://host.test/f"},
		{"a magnet", "magnet:?xt=urn:btih:0102030405060708090A0B0C0D0E0F1011121314"},
		{"a javascript url", "javascript://host.test/%0Aalert(1)"},
		{"credentials", "https://user:pass@host.test/f"},
		{"no host", "https:///f"},
		{"only a port", "https://:443/f"},
		{"a space inside", "https://host.test/a b"},
		{"a tab inside", "https://host.test/a\tb"},
		{"empty", ""},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", c.in)

			got, err := Canonical(c.in)
			t.Logf("output: %q, %v", got, err)

			if !errors.Is(err, ErrLink) {
				t.Errorf("got %q and %v, want ErrLink", got, err)
			}
		})
	}
}

func TestParseReadsHints(t *testing.T) {
	raw := "#title: Some Release 1080p\r\n" +
		"# a plain comment\r\n" +
		"#protected: 1\r\n" +
		"#file: 1\r\n" +
		"#name: some.release.part2.rar\r\n" +
		"#size: 524288000\r\n" +
		"#hoster: examplehost\r\n" +
		"https://examplehost.test/f/def456\r\n" +
		"#unknown: ignored\r\n" +
		"HTTPS://OtherHost.test/d/xyz789#frag\r\n"
	t.Logf("input:\n%s", raw)

	doc, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("output: %+v", *doc)

	if doc.Title != "Some Release 1080p" || !doc.Protected {
		t.Errorf("document hints: title %q protected %v", doc.Title, doc.Protected)
	}
	if len(doc.Links) != 2 {
		t.Fatalf("got %d links, want 2", len(doc.Links))
	}

	want := Link{URL: "https://examplehost.test/f/def456", File: 1, Name: "some.release.part2.rar", Size: 524288000, Hoster: "examplehost"}
	if doc.Links[0] != want {
		t.Errorf("first link: got %+v, want %+v", doc.Links[0], want)
	}

	// A hint belongs to the link after it and to no other.
	// Its fragment is the link's own, and the line is no comment for holding a '#'.
	if bare := (Link{URL: "https://otherhost.test/d/xyz789#frag"}); doc.Links[1] != bare {
		t.Errorf("second link: got %+v, want %+v", doc.Links[1], bare)
	}
}

func TestBytesRoundTrips(t *testing.T) {
	doc := &Document{
		Title: "Some\nRelease",
		Links: []Link{
			{URL: "https://b.test/2", File: 1, Name: "part2.rar", Size: 20},
			{URL: "https://a.test/1", File: 0, Name: "part1.rar", Size: 10, Hoster: "a"},
			{URL: "https://c.test/1", File: 0, Hoster: "c"},
		},
	}
	t.Logf("input:  %+v", *doc)

	raw := doc.Bytes()
	t.Logf("output:\n%s", raw)

	back, err := Parse(raw)
	if err != nil {
		t.Fatalf("parsing what Bytes wrote: %v", err)
	}

	// A line break in a value would have started a link, so it is a space.
	if back.Title != "Some Release" {
		t.Errorf("title: got %q", back.Title)
	}

	// Ordered by file, then by URL.
	order := []string{"https://a.test/1", "https://c.test/1", "https://b.test/2"}
	for i, want := range order {
		if back.Links[i].URL != want {
			t.Errorf("link %d: got %s, want %s", i, back.Links[i].URL, want)
		}
	}
	if back.FileCount() != 2 {
		t.Errorf("file count: got %d, want 2", back.FileCount())
	}

	a, _ := Identity(doc)
	b, _ := Identity(back)
	if a != b {
		t.Errorf("the identity changed across a round trip: %x then %x", a, b)
	}
	if string(back.Bytes()) != string(raw) {
		t.Errorf("Bytes is not stable across a round trip")
	}
}

func TestParseRejects(t *testing.T) {
	tooMany := strings.Repeat("https://host.test/f\n", MaxLinks+1)

	cases := []struct {
		label string
		raw   string
		want  error
	}{
		{"empty", "", ErrNoLinks},
		{"only comments", "#title: nothing\n# here\n", ErrNoLinks},
		{"an html error page", "<html><body>404</body></html>\n", ErrLink},
		{"one bad line among good ones", "https://host.test/f\nftp://host.test/f\n", ErrLink},
		{"past the limit", tooMany, ErrTooManyLinks},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %d bytes", len(c.raw))

			_, err := Parse([]byte(c.raw))
			t.Logf("output: %v", err)

			if !errors.Is(err, c.want) {
				t.Errorf("got %v, want %v", err, c.want)
			}
		})
	}
}

func TestMintInput(t *testing.T) {
	doc := &Document{
		Protected: true,
		Links: []Link{
			{URL: "https://a.test/1", File: 0},
			{URL: "https://b.test/1", File: 0},
			{URL: "https://a.test/2", File: 1},
		},
	}
	t.Logf("input:  %+v", *doc)

	in, err := doc.MintInput(1)
	if err != nil {
		t.Fatalf("mint input: %v", err)
	}

	hash, err := metahash.Mint(in)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	parsed, err := metahash.Parse(hash[:])
	if err != nil {
		t.Fatalf("parsing the minted hash: %v", err)
	}
	t.Logf("output: %s kind=%s flags=%#x index=%d", hash, parsed.Kind, uint8(parsed.Flags), parsed.FileIndex)

	if parsed.Kind != metahash.KindWeb {
		t.Errorf("kind: got %s", parsed.Kind)
	}

	// Two files, three links: mirrors do not count as files.
	want := metahash.FlagMultiFile | metahash.FlagPathAuthoritative | metahash.FlagProtected
	if parsed.Flags != want {
		t.Errorf("flags: got %#x, want %#x", uint8(parsed.Flags), uint8(want))
	}
}
