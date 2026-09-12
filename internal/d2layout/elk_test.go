//go:build !noelk

package d2layout

import (
	"context"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
)

func TestELKOptions(t *testing.T) {
	engine := newELK()
	wantOpts := d2elklayout.ConfigurableOpts{
		Algorithm:       "layered",
		NodeSpacing:     123,
		Padding:         "[top=11,left=22,bottom=33,right=44]",
		EdgeNodeSpacing: 67,
		SelfLoopSpacing: 89,
	}
	want := layoutGraph(t, func(ctx context.Context, g *d2graph.Graph) error {
		return d2elklayout.Layout(ctx, g, &wantOpts)
	})
	defaults := layoutGraph(t, d2elklayout.DefaultLayout)
	if defaults == want {
		t.Fatal("test graph does not exercise configured ELK spacing")
	}
	if err := engine.Configure(configuredState(t, engine,
		"--elk-algorithm=layered", "--elk-nodeNodeBetweenLayers=123",
		"--elk-padding=[top=11,left=22,bottom=33,right=44]",
		"--elk-edgeNodeBetweenLayers=67", "--elk-nodeSelfLoop=89")); err != nil {
		t.Fatal(err)
	}
	if got := layoutGraph(t, engine.Layout); got != want {
		t.Fatal("ELK command-line options differ from native options")
	}
	other := newELK()
	if got := layoutGraph(t, other.Layout); got != defaults {
		t.Fatal("configuring one ELK instance changed another's defaults")
	}
	if err := other.Configure(configuredState(t, other, "--elk-nodeNodeBetweenLayers=234")); err != nil {
		t.Fatal(err)
	}
	if got := layoutGraph(t, engine.Layout); got != want {
		t.Fatal("configuring a second ELK instance changed the first")
	}
	if err := engine.Configure(nil); err != nil {
		t.Fatal(err)
	}
	if got := layoutGraph(t, engine.Layout); got != defaults {
		t.Fatal("Configure(nil) did not restore ELK defaults")
	}
}
