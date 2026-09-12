//go:build !noelk

package d2layout

import (
	"context"
	"sync"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
	"github.com/d2lang/util-go/xmain"
)

func newELK() *Engine {
	var mu sync.RWMutex
	opts := d2elklayout.DefaultOpts
	engine := &Engine{
		Name:      "elk",
		ShortHelp: "Eclipse Layout Kernel (ELK) with the Layered algorithm.",
		features:  containerDimensions | descendantEdges,
		Flags: []Flag{
			{
				Name:    "elk-algorithm",
				Default: d2elklayout.DefaultOpts.Algorithm,
				Usage:   "layout algorithm",
			},
			{
				Name:    "elk-nodeNodeBetweenLayers",
				Default: int64(d2elklayout.DefaultOpts.NodeSpacing),
				Usage:   "the spacing to be preserved between any pair of nodes of two adjacent layers",
			},
			{
				Name:    "elk-padding",
				Default: d2elklayout.DefaultOpts.Padding,
				Usage:   "the padding to be left to a parent element’s border when placing child elements",
			},
			{
				Name:    "elk-edgeNodeBetweenLayers",
				Default: int64(d2elklayout.DefaultOpts.EdgeNodeSpacing),
				Usage:   "the spacing to be preserved between nodes and edges that are routed next to the node’s layer",
			},
			{
				Name:    "elk-nodeSelfLoop",
				Default: int64(d2elklayout.DefaultOpts.SelfLoopSpacing),
				Usage:   "spacing to be preserved between a node and its self loops",
			},
		},
		configure: func(ms *xmain.State) error {
			values := optionValues{state: ms}
			next := d2elklayout.ConfigurableOpts{
				Algorithm:       values.string("elk-algorithm", d2elklayout.DefaultOpts.Algorithm),
				NodeSpacing:     values.integer("elk-nodeNodeBetweenLayers", d2elklayout.DefaultOpts.NodeSpacing),
				Padding:         values.string("elk-padding", d2elklayout.DefaultOpts.Padding),
				EdgeNodeSpacing: values.integer("elk-edgeNodeBetweenLayers", d2elklayout.DefaultOpts.EdgeNodeSpacing),
				SelfLoopSpacing: values.integer("elk-nodeSelfLoop", d2elklayout.DefaultOpts.SelfLoopSpacing),
			}
			if values.err != nil {
				return values.err
			}
			mu.Lock()
			opts = next
			mu.Unlock()
			return nil
		},
		Layout: func(ctx context.Context, g *d2graph.Graph) error {
			mu.RLock()
			snapshot := opts
			mu.RUnlock()
			return d2elklayout.Layout(ctx, g, &snapshot)
		},
	}
	engine.LongHelp = helpWithFlags(`ELK is a layout engine offered by Eclipse.
Originally written in Java, D2's ELK.js 0.12.0 layout profile is bundled through the native Go elk-go port. Layered remains the default.
See https://d2lang.com/tour/elk for more.

Flags correspond to ones found at https://www.eclipse.org/elk/reference.html.
`, engine.Flags)
	return engine
}
