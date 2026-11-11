package stdx

import (
	"strconv"
	"unicode/utf8"

	"github.com/go-sphere/httpx"
)

// appendErrorBody appends body as the compact JSON json.Marshal produces for
// it, HTML escaping included, without the reflection: an error response is the
// one every probe of an unknown path gets, so it should not cost several times
// a successful one.
func appendErrorBody(dst []byte, body httpx.ErrorBody) []byte {
	dst = append(dst, `{"success":`...)
	dst = strconv.AppendBool(dst, body.Success)
	dst = append(dst, `,"code":`...)
	dst = strconv.AppendInt(dst, int64(body.Code), 10)
	dst = append(dst, `,"message":`...)
	dst = appendJSONString(dst, body.Message)
	return append(dst, '}')
}

const hexDigits = "0123456789abcdef"

// appendJSONString appends s as a JSON string the way encoding/json does with
// HTML escaping on: '<', '>' and '&' as \u003c-style escapes, U+2028 and U+2029
// escaped, and each invalid UTF-8 byte replaced by U+FFFD.
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if b >= 0x20 && b != '"' && b != '\\' && b != '<' && b != '>' && b != '&' {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			switch b {
			case '"', '\\':
				dst = append(dst, '\\', b)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[b>>4], hexDigits[b&0xF])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			dst = append(dst, s[start:i]...)
			dst = utf8.AppendRune(dst, utf8.RuneError)
		case r == '\u2028' || r == '\u2029':
			dst = append(dst, s[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[r&0xF])
		default:
			i += size
			continue
		}
		i += size
		start = i
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}
