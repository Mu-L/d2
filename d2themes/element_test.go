package d2themes

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestThemableElementEscapesPaintAttributes(t *testing.T) {
	t.Parallel()

	const value = `red" onload="alert(1)`
	element := NewThemableElement("rect", nil)
	element.Fill = value
	element.Stroke = value
	element.BackgroundColor = value
	element.Color = value
	source := "<svg>" + element.Render() + "</svg>"

	want := map[string]bool{
		"fill":             false,
		"stroke":           false,
		"background-color": false,
		"color":            false,
	}
	decoder := xml.NewDecoder(strings.NewReader(source))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Render() emitted invalid XML: %v\n%s", err, source)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range start.Attr {
			if strings.HasPrefix(strings.ToLower(attr.Name.Local), "on") {
				t.Fatalf("Render() emitted an event-handler attribute %q: %s", attr.Name.Local, source)
			}
			if _, ok := want[attr.Name.Local]; ok && attr.Value == value {
				want[attr.Name.Local] = true
			}
		}
	}
	for attribute, found := range want {
		if !found {
			t.Errorf("Render() did not preserve %s value %q after XML decoding: %s", attribute, value, source)
		}
	}
}
