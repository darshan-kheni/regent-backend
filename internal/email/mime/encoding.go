package mime

import (
	"strings"

	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

// DecodeCharset converts a string from the given charset to UTF-8.
// Uses htmlindex.Get() which correctly maps iso-8859-1 -> Windows-1252
// per the HTML5 encoding specification.
func DecodeCharset(s, charset string) string {
	if charset == "" || strings.EqualFold(charset, "utf-8") || strings.EqualFold(charset, "us-ascii") {
		return s
	}
	enc, err := htmlindex.Get(charset)
	if err != nil {
		return s // Unknown charset — return raw
	}
	decoded, _, err := transform.String(enc.NewDecoder(), s)
	if err != nil {
		return s
	}
	return decoded
}
