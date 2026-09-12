//go:build !nodagre

package d2layout

import (
	"context"
	"sync"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/util-go/xmain"
)

func newDagre() *Engine {
	var mu sync.RWMutex
	opts := d2dagrelayout.DefaultOpts
	engine := &Engine{
		Name:      "dagre",
		ShortHelp: "The directed graph layout library Dagre",
		Flags: []Flag{
			{
				Name:    "dagre-nodesep",
				Default: int64(d2dagrelayout.DefaultOpts.NodeSep),
				Usage:   "number of pixels that separate nodes horizontally.",
			},
			{
				Name:    "dagre-edgesep",
				Default: int64(d2dagrelayout.DefaultOpts.EdgeSep),
				Usage:   "number of pixels that separate edges horizontally.",
			},
		},
		configure: func(ms *xmain.State) error {
			values := optionValues{state: ms}
			next := d2dagrelayout.ConfigurableOpts{
				NodeSep: values.integer("dagre-nodesep", d2dagrelayout.DefaultOpts.NodeSep),
				EdgeSep: values.integer("dagre-edgesep", d2dagrelayout.DefaultOpts.EdgeSep),
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
			return d2dagrelayout.Layout(ctx, g, &snapshot)
		},
	}
	engine.LongHelp = helpWithFlags(`dagre is a directed graph layout algorithm implemented natively in Go by Dagro.
See https://d2lang.com/tour/dagre for more.

Dagro implements the Dagre 3.1.1 layout surface used by D2: https://github.com/d2lang/dagro.

Flags correspond to ones found at https://github.com/dagrejs/dagre/wiki.
`, engine.Flags)
	return engine
}
