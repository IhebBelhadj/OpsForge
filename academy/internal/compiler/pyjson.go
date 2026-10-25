package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// pyJSON writes v as Python's json.dumps(v, sort_keys=True, separators=(",", ":")): keys sorted, non-ASCII as
// \uXXXX escapes, floats as Python's repr. Content hashes are taken over these bytes: they must stay byte-identical
// to the ones Phase 10 and the Python compiler recorded, or every pass would turn stale. The bundle is written the
// same way, so the catalogue's documents keep their bytes too.
func pyJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := writePy(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// contentHash is a stable hash of any content: the first 16 hex digits of the SHA-256 of its pyJSON.
func contentHash(v any) (string, error) {
	data, err := pyJSON(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16], nil
}

func writePy(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		if math.IsInf(x, 0) || math.IsNaN(x) {
			return fmt.Errorf("%v cannot be written as JSON", x)
		}
		b.WriteString(pyFloat(x))
	case string:
		writePyString(b, x)
	case []any:
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writePy(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case []string:
		items := make([]any, len(x))
		for i, s := range x {
			items[i] = s
		}
		return writePy(b, items)
	case map[string]string:
		m := make(map[string]any, len(x))
		for k, s := range x {
			m[k] = s
		}
		return writePy(b, m)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys) // byte order of UTF-8 is code point order, Python's
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writePyString(b, k)
			b.WriteByte(':')
			if err := writePy(b, x[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("%T cannot be written as JSON", v)
	}
	return nil
}

// writePyString is json.dumps' ensure_ascii encoding: printable ASCII as is, the rest escaped (astral characters
// as surrogate pairs).
func writePyString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r >= 0x20 && r <= 0x7e:
			b.WriteRune(r)
		case r > 0xffff:
			r -= 0x10000
			fmt.Fprintf(b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		default:
			fmt.Fprintf(b, `\u%04x`, r)
		}
	}
	b.WriteByte('"')
}

// pyFloat is Python's repr of a float: the shortest digits that round-trip, in positional notation from 1e-4 up to
// 1e16 (with ".0" when integral), else in exponent notation with at least two exponent digits.
func pyFloat(f float64) string {
	e := strconv.FormatFloat(f, 'e', -1, 64) // e.g. 1.5e+16, 1e-05: Python's exponent form exactly
	exp, _ := strconv.Atoi(e[strings.IndexByte(e, 'e')+1:])
	if exp < -4 || exp >= 16 {
		return e
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsRune(s, '.') {
		s += ".0"
	}
	return s
}

// pyRepr is how Python's {value!r} shows a value in a message: strings quoted, None, True, False.
func pyRepr(v any) string {
	switch x := v.(type) {
	case string:
		quote := "'"
		if strings.Contains(x, "'") && !strings.Contains(x, `"`) {
			quote = `"`
		}
		var b strings.Builder
		b.WriteString(quote)
		for _, r := range x {
			switch {
			case r == '\\':
				b.WriteString(`\\`)
			case string(r) == quote:
				b.WriteString(`\` + quote)
			case r == '\n':
				b.WriteString(`\n`)
			case r == '\t':
				b.WriteString(`\t`)
			case r == '\r':
				b.WriteString(`\r`)
			case r < 0x20 || r == 0x7f || r == utf8.RuneError:
				fmt.Fprintf(&b, `\x%02x`, r)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteString(quote)
		return b.String()
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case float64:
		return pyFloat(x)
	}
	data, err := pyJSON(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}
