package d2layout

import (
	"fmt"

	"github.com/d2lang/d2/d2graph"
)

type features uint8

const (
	nearObject features = 1 << iota
	containerDimensions
	topLeft
	descendantEdges
)

// CheckFeatures rejects diagram attributes unsupported by this engine.
func (engine *Engine) CheckFeatures(g *d2graph.Graph) error {
	for _, obj := range g.Objects {
		if obj.Top != nil || obj.Left != nil {
			if engine.features&topLeft == 0 {
				return fmt.Errorf(`Object "%s" has attribute "top" and/or "left" set, but layout engine "%s" does not support locked positions. See https://d2lang.com/tour/layouts/#layout-specific-functionality for more.`, obj.AbsID(), engine.Name)
			}
		}
		if (obj.WidthAttr != nil || obj.HeightAttr != nil) &&
			len(obj.ChildrenArray) > 0 && !obj.IsGridDiagram() {
			if engine.features&containerDimensions == 0 {
				return fmt.Errorf(`Object "%s" has attribute "width" and/or "height" set, but layout engine "%s" does not support dimensions set on containers. See https://d2lang.com/tour/layouts/#layout-specific-functionality for more.`, obj.AbsID(), engine.Name)
			}
		}

		if obj.NearKey != nil {
			_, isKey := g.Root.HasChild(d2graph.Key(obj.NearKey))
			if isKey {
				if engine.features&nearObject == 0 {
					return fmt.Errorf(`Object "%s" has "near" set to another object, but layout engine "%s" only supports constant values for "near". See https://d2lang.com/tour/layouts/#layout-specific-functionality for more.`, obj.AbsID(), engine.Name)
				}
			}
		}
	}
	if engine.features&descendantEdges == 0 {
		for _, e := range g.Edges {
			// descendant edges are ok in sequence diagrams
			if e.Src.OuterSequenceDiagram() != nil || e.Dst.OuterSequenceDiagram() != nil {
				continue
			}
			if !e.Src.IsContainer() && !e.Dst.IsContainer() {
				continue
			}
			if e.Src == e.Dst {
				return fmt.Errorf(`Connection "%s" is a self loop on a container, but layout engine "%s" does not support this. See https://d2lang.com/tour/layouts/#layout-specific-functionality for more.`, e.AbsID(), engine.Name)
			}
			if e.Src.IsDescendantOf(e.Dst) || e.Dst.IsDescendantOf(e.Src) {
				return fmt.Errorf(`Connection "%s" goes from a container to a descendant, but layout engine "%s" does not support this. See https://d2lang.com/tour/layouts/#layout-specific-functionality for more.`, e.AbsID(), engine.Name)
			}
		}
	}
	return nil
}
