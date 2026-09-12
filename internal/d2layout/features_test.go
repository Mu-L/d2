package d2layout

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
)

func TestFeatureSupport(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		dagreOK bool
		elkOK   bool
	}{
		{"ordinary edges", "a -> b", true, true},
		{"locked positions", "a: {top: 10; left: 20}", false, false},
		{"container dimensions", "a: {width: 300; b}", false, true},
		{"object near", "a\nb: {near: a}", false, false},
		{"constant near", "a: {near: top-center}", true, true},
		{"descendant edges", "a.b\na -> a.b", false, true},
		{"container loop", "a.b\na -> a", false, true},
		{"grid dimensions", "a: {grid-columns: 1; width: 300; b}", true, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph, _, err := d2compiler.Compile("test.d2", strings.NewReader(test.script), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, engine := range List() {
				wantOK := engine.Name == "tala" || (engine.Name == "dagre" && test.dagreOK) || (engine.Name == "elk" && test.elkOK)
				err := engine.CheckFeatures(graph)
				if (err == nil) != wantOK {
					t.Errorf("%s feature support = %v, want success %v", engine.Name, err, wantOK)
				}
				if err != nil && !strings.Contains(err.Error(), `layout engine "`+engine.Name+`"`) {
					t.Errorf("feature error does not identify the engine: %v", err)
				}
			}
		})
	}
}
