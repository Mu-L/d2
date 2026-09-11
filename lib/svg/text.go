package svg

import (
	"bytes"
	"encoding/base32"
	"encoding/xml"
	"strings"
)

var xmlDoubleQuotedAttributeEscaper = strings.NewReplacer(
	"&", "&amp;",
	`"`, "&#34;",
	"<", "&lt;",
	">", "&gt;",
)

func EscapeText(text string) string {
	buf := new(bytes.Buffer)
	_ = xml.EscapeText(buf, []byte(text))
	return buf.String()
}

// EscapeAttribute escapes a value for use in a double-quoted XML attribute.
// Single quotes are left unchanged because they cannot terminate that context.
func EscapeAttribute(attribute string) string {
	return xmlDoubleQuotedAttributeEscaper.Replace(attribute)
}

func SVGID(text string) string {
	return strings.TrimRight(base32.StdEncoding.EncodeToString([]byte(text)), "=")
}
