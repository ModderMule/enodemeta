package bencode

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
)

// Encode writes a value in bencoding.
//
// Accepted Go types are the ones a KRPC message is built from: string, []byte,
// int and the sized integers, bool (as 1/0, which is how BEP 5 spells flags
// like `implied_port`), map[string]any, []any, and a Value from a previous
// decode. Anything else is an error rather than a guess.
//
// Dictionary keys are sorted, as BEP 3 requires. That is what makes an encoded
// KRPC query reproducible, which the tests depend on.
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeTo(&buf, v); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// MustEncode is Encode for values built in code rather than parsed from input.
// It panics on an unsupported type, which is a programming error, not a runtime
// condition.
func MustEncode(v any) []byte {
	out, err := Encode(v)
	if err != nil {
		panic(err)
	}

	return out
}

// -- internals ---------------------------------------------------------------

func encodeTo(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		return fmt.Errorf("bencode: cannot encode nil")

	case Value:
		// A decoded value re-encodes as the exact bytes it came from, never as
		// a re-serialisation: that is what keeps an infohash intact when a
		// dictionary is passed through.
		if len(t.Raw) == 0 {
			return fmt.Errorf("bencode: cannot encode a Value with no raw span")
		}
		buf.Write(t.Raw)

		return nil

	case string:
		writeString(buf, []byte(t))

		return nil

	case []byte:
		writeString(buf, t)

		return nil

	case bool:
		n := 0
		if t {
			n = 1
		}
		writeInt(buf, int64(n))

		return nil

	case int:
		writeInt(buf, int64(t))

		return nil
	case int8:
		writeInt(buf, int64(t))

		return nil
	case int16:
		writeInt(buf, int64(t))

		return nil
	case int32:
		writeInt(buf, int64(t))

		return nil
	case int64:
		writeInt(buf, t)

		return nil

	case uint8:
		writeInt(buf, int64(t))

		return nil
	case uint16:
		writeInt(buf, int64(t))

		return nil
	case uint32:
		writeInt(buf, int64(t))

		return nil
	case uint:
		if t > 1<<63-1 {
			return fmt.Errorf("bencode: %d does not fit in an int64", t)
		}
		writeInt(buf, int64(t))

		return nil
	case uint64:
		if t > 1<<63-1 {
			return fmt.Errorf("bencode: %d does not fit in an int64", t)
		}
		writeInt(buf, int64(t))

		return nil

	case []any:
		buf.WriteByte('l')
		for _, item := range t {
			if err := encodeTo(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte('e')

		return nil

	case []string:
		buf.WriteByte('l')
		for _, item := range t {
			writeString(buf, []byte(item))
		}
		buf.WriteByte('e')

		return nil

	case [][]byte:
		buf.WriteByte('l')
		for _, item := range t {
			writeString(buf, item)
		}
		buf.WriteByte('e')

		return nil

	case map[string]any:
		return encodeDict(buf, t)

	default:
		return fmt.Errorf("bencode: unsupported type %T", v)
	}
}

func encodeDict(buf *bytes.Buffer, m map[string]any) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf.WriteByte('d')
	for _, k := range keys {
		writeString(buf, []byte(k))
		if err := encodeTo(buf, m[k]); err != nil {
			return fmt.Errorf("bencode: key %q: %w", k, err)
		}
	}
	buf.WriteByte('e')

	return nil
}

func writeString(buf *bytes.Buffer, s []byte) {
	buf.WriteString(strconv.Itoa(len(s)))
	buf.WriteByte(':')
	buf.Write(s)
}

func writeInt(buf *bytes.Buffer, n int64) {
	buf.WriteByte('i')
	buf.WriteString(strconv.FormatInt(n, 10))
	buf.WriteByte('e')
}
