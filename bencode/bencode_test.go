package bencode

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestDecodeScalars(t *testing.T) {
	cases := []struct {
		label string
		in    string
		check func(t *testing.T, v Value)
	}{
		{
			label: "an integer",
			in:    "i42e",
			check: func(t *testing.T, v Value) {
				if v.Kind != KindInt || v.Int != 42 {
					t.Errorf("got %s %d, want integer 42", v.Kind, v.Int)
				}
			},
		},
		{
			label: "a negative integer",
			in:    "i-13e",
			check: func(t *testing.T, v Value) {
				if v.Int != -13 {
					t.Errorf("got %d, want -13", v.Int)
				}
			},
		},
		{
			label: "an empty string",
			in:    "0:",
			check: func(t *testing.T, v Value) {
				if v.Kind != KindString || len(v.Str) != 0 {
					t.Errorf("got %s %q, want an empty string", v.Kind, v.Str)
				}
			},
		},
		{
			label: "a string holding arbitrary bytes",
			in:    "3:\x00\xff\x01",
			check: func(t *testing.T, v Value) {
				if !bytes.Equal(v.Str, []byte{0x00, 0xff, 0x01}) {
					t.Errorf("got % x, want 00 ff 01", v.Str)
				}
			},
		},
		{
			label: "an empty list",
			in:    "le",
			check: func(t *testing.T, v Value) {
				if v.Kind != KindList || len(v.List) != 0 {
					t.Errorf("got %s with %d items, want an empty list", v.Kind, len(v.List))
				}
			},
		},
		{
			label: "an empty dictionary",
			in:    "de",
			check: func(t *testing.T, v Value) {
				if v.Kind != KindDict || len(v.Dict) != 0 {
					t.Errorf("got %s with %d entries, want an empty dictionary", v.Kind, len(v.Dict))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", c.in)

			v, err := Decode([]byte(c.in))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			t.Logf("output: kind=%s raw=%q", v.Kind, v.Raw)

			c.check(t, v)

			if string(v.Raw) != c.in {
				t.Errorf("the raw span must be the whole input, got %q", v.Raw)
			}
		})
	}
}

func TestDecodeNested(t *testing.T) {
	const in = "d4:infod6:lengthi1024e4:name4:testee"
	t.Logf("input:  %q", in)

	v, err := Decode([]byte(in))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	info, ok := v.GetDict("info")
	if !ok {
		t.Fatal("the info dictionary must be reachable by key")
	}
	t.Logf("output: info raw=%q", info.Raw)

	// This is the property the whole package exists for: the info span is what
	// an infohash is computed over, so it has to be the exact bytes from the
	// file rather than a re-encode.
	const wantInfo = "d6:lengthi1024e4:name4:teste"
	if string(info.Raw) != wantInfo {
		t.Errorf("info raw span: got %q, want %q", info.Raw, wantInfo)
	}

	if length, ok := info.GetInt("length"); !ok || length != 1024 {
		t.Errorf("info.length: got %d (%v), want 1024", length, ok)
	}
	if name, ok := info.GetString("name"); !ok || string(name) != "test" {
		t.Errorf("info.name: got %q (%v), want \"test\"", name, ok)
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := []struct {
		label string
		in    string
		want  error
	}{
		{"a truncated integer", "i42", ErrUnexpectedEnd},
		{"a truncated string", "5:abc", ErrUnexpectedEnd},
		{"an unterminated list", "li1e", ErrUnexpectedEnd},
		{"an unterminated dictionary", "d3:key", ErrUnexpectedEnd},
		{"empty input", "", ErrUnexpectedEnd},
		{"a bare terminator", "e", ErrSyntax},
		{"a non-integer integer", "iabce", ErrSyntax},
		{"an integer with a leading zero", "i042e", ErrSyntax},
		{"negative zero", "i-0e", ErrSyntax},
		{"an integer that overflows int64", "i9223372036854775808e", ErrSyntax},
		{"a negative string length", "-1:", ErrSyntax},
		{"a non-string dictionary key", "di1ei2ee", ErrSyntax},
		{"trailing data", "i1ei2e", ErrTrailingData},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", c.in)

			v, err := Decode([]byte(c.in))
			if err == nil {
				t.Fatalf("%s must be rejected, got %+v", c.label, v)
			}
			t.Logf("output: %v", err)

			if !errors.Is(err, c.want) {
				t.Errorf("got %v, want it to wrap %v", err, c.want)
			}
		})
	}
}

// TestDecodeHugeLength is the one that matters for a daemon reading UDP from
// strangers: a length prefix larger than the packet must cost a parse error,
// not an allocation.
func TestDecodeHugeLength(t *testing.T) {
	const in = "9223372036854775807:x"
	t.Logf("input:  %q", in)

	_, err := Decode([]byte(in))
	if err == nil {
		t.Fatal("a string longer than the input must be rejected")
	}
	t.Logf("output: %v", err)

	if !errors.Is(err, ErrUnexpectedEnd) {
		t.Errorf("got %v, want it to wrap %v", err, ErrUnexpectedEnd)
	}
}

func TestDecodeDepthLimit(t *testing.T) {
	in := strings.Repeat("l", MaxDepth+2) + strings.Repeat("e", MaxDepth+2)
	t.Logf("input:  %d nested lists", MaxDepth+2)

	_, err := Decode([]byte(in))
	if err == nil {
		t.Fatal("nesting past MaxDepth must be rejected")
	}
	t.Logf("output: %v", err)

	if !errors.Is(err, ErrDepth) {
		t.Errorf("got %v, want it to wrap %v", err, ErrDepth)
	}
}

func TestDecodePrefix(t *testing.T) {
	const in = "d1:ai1ee-- trailing bytes a real torrent sometimes carries"
	t.Logf("input:  %q", in)

	v, n, err := DecodePrefix([]byte(in))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Logf("output: consumed %d byte(s), raw=%q", n, v.Raw)

	if n != len("d1:ai1ee") {
		t.Errorf("consumed %d bytes, want %d", n, len("d1:ai1ee"))
	}
}

func TestEncode(t *testing.T) {
	cases := []struct {
		label string
		in    any
		want  string
	}{
		{"an integer", 42, "i42e"},
		{"a negative integer", -13, "i-13e"},
		{"true is 1, as BEP 5 spells implied_port", true, "i1e"},
		{"a string", "abc", "3:abc"},
		{"bytes", []byte{0, 255}, "2:\x00\xff"},
		{"a list", []any{1, "a"}, "li1e1:ae"},
		{"a dictionary with sorted keys", map[string]any{"b": 2, "a": 1}, "d1:ai1e1:bi2ee"},
		{
			"a KRPC query",
			map[string]any{"t": "aa", "y": "q", "q": "ping", "a": map[string]any{"id": "01234567890123456789"}},
			"d1:ad2:id20:01234567890123456789e1:q4:ping1:t2:aa1:y1:qe",
		},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %#v", c.in)

			out, err := Encode(c.in)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			t.Logf("output: %q", out)

			if string(out) != c.want {
				t.Errorf("got %q, want %q", out, c.want)
			}
		})
	}
}

// TestEncodeValuePassesRawThrough pins the rule that keeps infohashes intact:
// re-encoding a decoded value emits its original bytes, not a re-serialisation
// that might order keys differently than the file did.
func TestEncodeValuePassesRawThrough(t *testing.T) {
	// Deliberately out of order, as a sloppy generator would write it.
	const unsorted = "d4:name4:test6:lengthi1024ee"
	t.Logf("input:  %q (keys not sorted)", unsorted)

	v, err := Decode([]byte(unsorted))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.SortedKeys() {
		t.Error("SortedKeys must report this input as unsorted")
	}

	out, err := Encode(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	t.Logf("output: %q", out)

	if string(out) != unsorted {
		t.Errorf("a decoded value must re-encode byte for byte: got %q, want %q", out, unsorted)
	}
}

func TestEncodeRejectsUnsupported(t *testing.T) {
	_, err := Encode(3.14)
	if err == nil {
		t.Fatal("a float has no bencoding and must be rejected")
	}
	t.Logf("output: %v", err)
}

func TestRoundTrip(t *testing.T) {
	in := map[string]any{
		"id":     []byte("01234567890123456789"),
		"nested": map[string]any{"list": []any{1, 2, 3}, "s": "x"},
		"n":      int64(-5),
	}
	t.Logf("input:  %#v", in)

	encoded, err := Encode(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	again, err := Encode(decoded)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	t.Logf("output: %q", again)

	if !bytes.Equal(encoded, again) {
		t.Errorf("round trip changed the bytes:\n got %q\nwant %q", again, encoded)
	}
	if !decoded.SortedKeys() {
		t.Error("Encode must sort keys, so the decode must see them sorted")
	}
}

// FuzzDecode is the real test of the untrusted-input claim. Any input at all may
// be rejected, but none may panic, and whatever decodes must report a raw span
// that is exactly the bytes it consumed.
func FuzzDecode(f *testing.F) {
	seeds := []string{
		"i42e", "3:abc", "le", "de", "d4:infod6:lengthi1e4:name1:aee",
		"li1eli2eee", "i-0e", "5:ab", "d1:ai1ee",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		v, n, err := DecodePrefix(data)
		if err != nil {
			return
		}

		if n < 0 || n > len(data) {
			t.Fatalf("consumed %d of %d bytes", n, len(data))
		}
		if !bytes.Equal(v.Raw, data[:n]) {
			t.Fatalf("raw span %q is not the consumed prefix %q", v.Raw, data[:n])
		}

		// A decoded value must re-encode to its own span, which is the property
		// every hash in this project depends on.
		out, err := Encode(v)
		if err != nil {
			t.Fatalf("re-encoding a decoded value: %v", err)
		}
		if !bytes.Equal(out, v.Raw) {
			t.Fatalf("re-encode changed the bytes:\n got %q\nwant %q", out, v.Raw)
		}
	})
}
