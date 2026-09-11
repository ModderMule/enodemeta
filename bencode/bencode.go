// Package bencode decodes and encodes the bencoding used by BitTorrent's
// metainfo files (BEP 3) and by the DHT's KRPC messages (BEP 5).
//
// # Raw spans
//
// A decoded Value keeps the exact bytes it was decoded from. That is not a
// convenience: a torrent's infohash is the SHA-1 of the `info` dictionary
// *as encoded in that file*, and re-encoding a decoded dictionary is not
// guaranteed to reproduce it byte for byte — a generator may have emitted keys
// out of order, or an integer with a form this encoder would write differently.
// Hashing anything but the original span produces an infohash that matches no
// swarm. So Value.Raw is what verification and storage use, and the decoded
// structure is only for reading fields.
//
// # Untrusted input
//
// Every input here arrives from the network. The decoder allocates nothing it
// has not already seen the bytes for, bounds every length against the remaining
// input, and limits nesting depth, so a hostile packet costs a parse error
// rather than memory.
package bencode

import (
	"errors"
	"fmt"
)

// MaxDepth bounds nesting. A real torrent nests a handful deep — the v2 file
// tree is the deepest thing in practice — so this only ever stops a crafted
// input designed to exhaust the stack.
const MaxDepth = 64

// Kind is the type of a decoded Value.
type Kind uint8

// The four bencode types.
const (
	KindInvalid Kind = iota
	KindInt
	KindString
	KindList
	KindDict
)

// String names the kind, for error messages.
func (k Kind) String() string {
	switch k {
	case KindInt:
		return "integer"
	case KindString:
		return "string"
	case KindList:
		return "list"
	case KindDict:
		return "dictionary"
	default:
		return "invalid"
	}
}

// Errors returned by Decode. They are distinguishable because the crawler
// counts parse failures by cause: a truncated packet is a different signal from
// a well-formed one carrying nonsense.
var (
	ErrUnexpectedEnd = errors.New("bencode: unexpected end of input")
	ErrSyntax        = errors.New("bencode: malformed input")
	ErrTrailingData  = errors.New("bencode: trailing data after value")
	ErrDepth         = errors.New("bencode: nesting too deep")
	ErrNotFound      = errors.New("bencode: key not found")
)

// Value is one decoded bencode value.
//
// Str, List, Dict and Raw all alias the input buffer rather than copying it, so
// a Value must not outlive the bytes it was decoded from, and the caller must
// not mutate them. Clone what you keep.
type Value struct {
	Kind Kind

	// Raw is the exact encoded form of this value, including its own framing.
	// For a dictionary this is the "d...e" span, which is what an infohash is
	// computed over.
	Raw []byte

	Int  int64
	Str  []byte
	List []Value
	Dict []Entry
}

// Entry is one key/value pair of a dictionary, kept in the order it appeared.
//
// Order matters twice: BEP 3 requires keys to be sorted, so a file whose keys
// are not is a thing worth noticing, and the v2 file tree's traversal order is
// the order its file indexes are assigned in.
type Entry struct {
	Key   []byte
	Value Value
}

// Decode parses one value and requires it to be the whole input.
func Decode(data []byte) (Value, error) {
	v, n, err := DecodePrefix(data)
	if err != nil {
		return Value{}, err
	}
	if n != len(data) {
		return Value{}, fmt.Errorf("%w: %d byte(s) left", ErrTrailingData, len(data)-n)
	}

	return v, nil
}

// DecodePrefix parses one value from the front of data and reports how many
// bytes it used.
//
// Torrents in the wild do carry trailing bytes after the top-level dictionary,
// and a KRPC datagram can be padded, so both cases need a decode that stops at
// the end of the value rather than failing.
func DecodePrefix(data []byte) (Value, int, error) {
	d := &decoder{buf: data}

	v, err := d.value(0)
	if err != nil {
		return Value{}, 0, err
	}

	return v, d.pos, nil
}

// Get returns the value of a dictionary key.
func (v Value) Get(key string) (Value, bool) {
	if v.Kind != KindDict {
		return Value{}, false
	}

	for _, e := range v.Dict {
		if string(e.Key) == key {
			return e.Value, true
		}
	}

	return Value{}, false
}

// GetString returns a string-valued key.
func (v Value) GetString(key string) ([]byte, bool) {
	got, ok := v.Get(key)
	if !ok || got.Kind != KindString {
		return nil, false
	}

	return got.Str, true
}

// GetInt returns an integer-valued key.
func (v Value) GetInt(key string) (int64, bool) {
	got, ok := v.Get(key)
	if !ok || got.Kind != KindInt {
		return 0, false
	}

	return got.Int, true
}

// GetList returns a list-valued key.
func (v Value) GetList(key string) ([]Value, bool) {
	got, ok := v.Get(key)
	if !ok || got.Kind != KindList {
		return nil, false
	}

	return got.List, true
}

// GetDict returns a dictionary-valued key.
func (v Value) GetDict(key string) (Value, bool) {
	got, ok := v.Get(key)
	if !ok || got.Kind != KindDict {
		return Value{}, false
	}

	return got, true
}

// Text returns a string value as a Go string, copying it.
func (v Value) Text() string {
	if v.Kind != KindString {
		return ""
	}

	return string(v.Str)
}

// SortedKeys reports whether a dictionary's keys are in the ascending order
// BEP 3 requires. A file that fails this still decodes; it is the caller's
// business whether to care.
func (v Value) SortedKeys() bool {
	if v.Kind != KindDict {
		return true
	}

	for i := 1; i < len(v.Dict); i++ {
		if string(v.Dict[i-1].Key) >= string(v.Dict[i].Key) {
			return false
		}
	}

	return true
}

// -- internals ---------------------------------------------------------------

type decoder struct {
	buf []byte
	pos int
}

func (d *decoder) value(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, ErrDepth
	}
	if d.pos >= len(d.buf) {
		return Value{}, ErrUnexpectedEnd
	}

	start := d.pos

	switch c := d.buf[d.pos]; {
	case c == 'i':
		return d.integer(start)
	case c == 'l':
		return d.list(start, depth)
	case c == 'd':
		return d.dict(start, depth)
	case c >= '0' && c <= '9':
		return d.str(start)
	default:
		return Value{}, fmt.Errorf("%w: unexpected %q at offset %d", ErrSyntax, c, d.pos)
	}
}

func (d *decoder) integer(start int) (Value, error) {
	d.pos++ // 'i'

	end := d.indexFrom('e')
	if end < 0 {
		return Value{}, ErrUnexpectedEnd
	}

	n, err := parseInt(d.buf[d.pos:end])
	if err != nil {
		return Value{}, err
	}
	d.pos = end + 1

	return Value{Kind: KindInt, Int: n, Raw: d.buf[start:d.pos]}, nil
}

func (d *decoder) str(start int) (Value, error) {
	colon := d.indexFrom(':')
	if colon < 0 {
		return Value{}, ErrUnexpectedEnd
	}

	length, err := parseInt(d.buf[d.pos:colon])
	if err != nil {
		return Value{}, err
	}
	if length < 0 {
		return Value{}, fmt.Errorf("%w: negative string length %d", ErrSyntax, length)
	}

	// Compared against what is left rather than added to an offset: a crafted
	// length near the integer limit would overflow the addition and pass.
	if remaining := int64(len(d.buf) - (colon + 1)); length > remaining {
		return Value{}, fmt.Errorf("%w: string of %d byte(s) with %d left", ErrUnexpectedEnd, length, remaining)
	}

	d.pos = colon + 1 + int(length)

	return Value{Kind: KindString, Str: d.buf[colon+1 : d.pos], Raw: d.buf[start:d.pos]}, nil
}

func (d *decoder) list(start, depth int) (Value, error) {
	d.pos++ // 'l'

	var items []Value
	for {
		if d.pos >= len(d.buf) {
			return Value{}, ErrUnexpectedEnd
		}
		if d.buf[d.pos] == 'e' {
			d.pos++

			return Value{Kind: KindList, List: items, Raw: d.buf[start:d.pos]}, nil
		}

		item, err := d.value(depth + 1)
		if err != nil {
			return Value{}, err
		}
		items = append(items, item)
	}
}

func (d *decoder) dict(start, depth int) (Value, error) {
	d.pos++ // 'd'

	var entries []Entry
	for {
		if d.pos >= len(d.buf) {
			return Value{}, ErrUnexpectedEnd
		}
		if d.buf[d.pos] == 'e' {
			d.pos++

			return Value{Kind: KindDict, Dict: entries, Raw: d.buf[start:d.pos]}, nil
		}

		keyStart := d.pos
		if c := d.buf[d.pos]; c < '0' || c > '9' {
			return Value{}, fmt.Errorf("%w: dictionary key at offset %d is not a string", ErrSyntax, keyStart)
		}

		key, err := d.str(keyStart)
		if err != nil {
			return Value{}, err
		}

		val, err := d.value(depth + 1)
		if err != nil {
			return Value{}, err
		}

		entries = append(entries, Entry{Key: key.Str, Value: val})
	}
}

// indexFrom finds the next occurrence of c at or after the cursor.
func (d *decoder) indexFrom(c byte) int {
	for i := d.pos; i < len(d.buf); i++ {
		if d.buf[i] == c {
			return i
		}
	}

	return -1
}

// parseInt reads a bencoded integer, which is stricter than strconv: no plus
// sign, no leading zeros, and no "-0". The strictness is worth keeping because
// it is what makes a re-encode of a well-formed file byte-identical.
func parseInt(b []byte) (int64, error) {
	if len(b) == 0 {
		return 0, fmt.Errorf("%w: empty integer", ErrSyntax)
	}

	neg := b[0] == '-'
	digits := b
	if neg {
		digits = b[1:]
		if len(digits) == 0 {
			return 0, fmt.Errorf("%w: %q is not an integer", ErrSyntax, b)
		}
	}

	if len(digits) > 1 && digits[0] == '0' {
		return 0, fmt.Errorf("%w: %q has a leading zero", ErrSyntax, b)
	}
	if neg && digits[0] == '0' {
		return 0, fmt.Errorf("%w: negative zero", ErrSyntax)
	}

	var n int64
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("%w: %q is not an integer", ErrSyntax, b)
		}

		d := int64(c - '0')
		// Checked before the multiply rather than after: overflow in Go wraps
		// silently, and a wrapped file length would become a plausible one.
		if n > (1<<63-1-d)/10 {
			return 0, fmt.Errorf("%w: integer %q overflows int64", ErrSyntax, b)
		}
		n = n*10 + d
	}

	if neg {
		return -n, nil
	}

	return n, nil
}
