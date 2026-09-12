//go:build !nodagre

package d2layout

import (
	"context"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
)

func TestDagreOptions(t *testing.T) {
	engine := newDagre()
	wantOpts := d2dagrelayout.ConfigurableOpts{NodeSep: 123, EdgeSep: 67}
	want := layoutGraph(t, func(ctx context.Context, g *d2graph.Graph) error {
		return d2dagrelayout.Layout(ctx, g, &wantOpts)
	})
	defaults := layoutGraph(t, d2dagrelayout.DefaultLayout)
	if defaults == want {
		t.Fatal("test graph does not exercise configured Dagre spacing")
	}
	if err := engine.Configure(configuredState(t, engine, "--dagre-nodesep=123", "--dagre-edgesep=67")); err != nil {
		t.Fatal(err)
	}
	if got := layoutGraph(t, engine.Layout); got != want {
		t.Fatal("Dagre command-line spacing differs from native options")
	}
	other := newDagre()
	if got := layoutGraph(t, other.Layout); got != defaults {
		t.Fatal("configuring one Dagre instance changed another's defaults")
	}
	if err := other.Configure(configuredState(t, other, "--dagre-nodesep=234")); err != nil {
		t.Fatal(err)
	}
	if got := layoutGraph(t, engine.Layout); got != want {
		t.Fatal("configuring a second Dagre instance changed the first")
	}
	if err := engine.Configure(nil); err != nil {
		t.Fatal(err)
	}
	if got := layoutGraph(t, engine.Layout); got != defaults {
		t.Fatal("Configure(nil) did not restore Dagre defaults")
	}
}
