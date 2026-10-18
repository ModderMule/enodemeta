package linkcontainer

import (
	"crypto/sha256"
	"errors"
	"testing"
)

// TestIdentityIsTheBytes: the identity is the SHA-256 of the file as served,
// and what cannot be a container has none.
func TestIdentityIsTheBytes(t *testing.T) {
	content := []byte("a stand-in container")

	got, err := IdentityOf(content)
	t.Logf("input:  %q", content)
	t.Logf("output: %x %v", got, err)

	if err != nil || got != sha256.Sum256(content) {
		t.Errorf("got %x %v, want the bytes' SHA-256", got, err)
	}

	for _, c := range []struct {
		label   string
		content []byte
		want    error
	}{
		{"no bytes", nil, ErrEmpty},
		{"more than 1 MiB", make([]byte, MaxSize+1), ErrTooLarge},
	} {
		_, err := IdentityOf(c.content)
		t.Logf("input:  %s -> %v", c.label, err)

		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.label, err, c.want)
		}
	}
}

// TestFormat reads a container's format from its name or address, and back
// from its content type.
func TestFormat(t *testing.T) {
	for _, c := range []struct {
		name, format, contentType string
	}{
		{"Some.Release.2026.dlc", FormatDLC, ContentTypeDLC},
		{"https://protector.test/DLC/ABCDEF.DLC?x=1", FormatDLC, ContentTypeDLC},
		{"links.ccf", FormatCCF, ContentTypeCCF},
		{"links.rsdf#top", FormatRSDF, ContentTypeRSDF},
		{"Some.Release.2026.torrent", "", ""},
		{"dlc", "", ""},
	} {
		format, contentType := Format(c.name), ContentType(c.name)
		t.Logf("input:  %q", c.name)
		t.Logf("output: format %q, content type %q", format, contentType)

		if format != c.format || contentType != c.contentType {
			t.Errorf("%s: got %q %q, want %q %q", c.name, format, contentType, c.format, c.contentType)
		}
		if c.format != "" && FormatOf(contentType+"; charset=binary") != c.format {
			t.Errorf("%s: %q does not read back as %q", c.name, contentType, c.format)
		}
	}
}
