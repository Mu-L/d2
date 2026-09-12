package d2layout

import (
	"context"
	"slices"
	"sync"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/util-go/xmain"
)

func newTALA() *Engine {
	var mu sync.RWMutex
	opts := d2talalayout.DefaultOptions()
	opts.Seeds = slices.Clone(opts.Seeds)
	engine := &Engine{
		Name:      "tala",
		ShortHelp: "TALA is D2's native layout and edge-routing engine.",
		features:  descendantEdges | containerDimensions | nearObject | topLeft,
		Flags: []Flag{
			{
				Name:    "tala-seeds",
				Default: slices.Clone(opts.Seeds),
				Usage:   "random seeds for deterministic TALA layout attempts; the best complete result is selected.",
			},
		},
		configure: func(ms *xmain.State) error {
			values := optionValues{state: ms}
			next := d2talalayout.DefaultOptions()
			next.Seeds = values.integers("tala-seeds", next.Seeds)
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
			snapshot.Seeds = slices.Clone(opts.Seeds)
			mu.RUnlock()
			return d2talalayout.Layout(ctx, g, &snapshot)
		},
		RouteEdges: d2talalayout.RouteEdges,
	}
	engine.LongHelp = helpWithFlags(`TALA is D2's native layout and edge-routing engine for software architecture diagrams.

Diagram data under tala-seeds takes precedence over the command-line flag.
`, engine.Flags)
	return engine
}
