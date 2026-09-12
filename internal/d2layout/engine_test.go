package d2layout

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/xmain"
)

func TestBuiltins(t *testing.T) {
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "d2plugin-custom"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", path)
	for _, name := range []string{"custom", "d2plugin-custom", "", "../tala"} {
		if _, err := Find(name); err == nil {
			t.Errorf("Find(%q) accepted an unknown engine", name)
		}
	}
	var names []string
	for _, engine := range List() {
		names = append(names, engine.Name)
		selected, err := Find(strings.ToUpper(engine.Name))
		if err != nil {
			t.Fatal(err)
		}
		if selected == engine || selected.Name != engine.Name {
			t.Fatalf("Find(%q) did not return a fresh instance", engine.Name)
		}
		if engine.ShortHelp == "" || engine.LongHelp == "" || len(engine.Flags) == 0 {
			t.Fatalf("missing help or flags for %s", engine.Name)
		}
		if (engine.RouteEdges != nil) != (engine.Name == "tala") {
			t.Fatalf("unexpected edge router for %s", engine.Name)
		}
		if err := engine.Layout(t.Context(), simpleGraph()); err != nil {
			t.Fatalf("%s cannot lay out a graph with defaults: %v", engine.Name, err)
		}
	}
	want := []string{}
	if newDagre() != nil {
		want = append(want, "dagre")
	} else if _, err := Find("dagre"); err == nil {
		t.Fatal("dagre is available despite nodagre build tag")
	}
	if newELK() != nil {
		want = append(want, "elk")
	} else if _, err := Find("elk"); err == nil {
		t.Fatal("elk is available despite noelk build tag")
	}
	want = append(want, "tala")
	if !slices.Equal(names, want) {
		t.Fatalf("built-in engines = %v, want %v", names, want)
	}
}

func TestConcurrentConfigurationAndLayout(t *testing.T) {
	for _, engine := range List() {
		t.Run(engine.Name, func(t *testing.T) {
			state := configuredState(t, engine)
			var wait sync.WaitGroup
			for range 8 {
				wait.Add(2)
				go func() {
					defer wait.Done()
					for _, state := range []*xmain.State{state, nil, {}, {Opts: &xmain.Opts{}}} {
						if err := engine.Configure(state); err != nil {
							t.Error(err)
						}
					}
				}()
				go func() {
					defer wait.Done()
					if err := engine.Layout(t.Context(), simpleGraph()); err != nil {
						t.Error(err)
					}
				}()
			}
			wait.Wait()
		})
	}
}

func TestTALAOptionsIsolation(t *testing.T) {
	first, err := Find("tala")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Find("tala")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Configure(configuredState(t, first, "--tala-seeds=1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17")); err != nil {
		t.Fatal(err)
	}
	if err := first.Layout(t.Context(), simpleGraph()); err == nil || !strings.Contains(err.Error(), "at most 16") {
		t.Fatalf("command-line seeds were not applied: %v", err)
	}
	if err := second.Layout(t.Context(), simpleGraph()); err != nil {
		t.Fatalf("configuring another instance changed fresh defaults: %v", err)
	}
	graph := simpleGraph()
	graph.Data = map[string]any{"tala-seeds": []int64{7}}
	if err := first.Layout(t.Context(), graph); err != nil {
		t.Fatalf("diagram seeds did not override command-line seeds: %v", err)
	}
	if err := first.Configure(nil); err != nil {
		t.Fatal(err)
	}
	if err := first.Layout(t.Context(), simpleGraph()); err != nil {
		t.Fatalf("Configure(nil) did not restore defaults: %v", err)
	}
}

func TestConfigureRejectsWrongFlagTypes(t *testing.T) {
	for _, engine := range List() {
		t.Run(engine.Name, func(t *testing.T) {
			state := &xmain.State{Opts: xmain.NewOpts(nil, nil)}
			state.Opts.Flags.Bool(engine.Flags[0].Name, false, "wrong type")
			if err := engine.Configure(state); err == nil {
				t.Fatal("accepted a flag with the wrong type")
			}
			if err := engine.Layout(t.Context(), simpleGraph()); err != nil {
				t.Fatalf("rejected configuration changed defaults: %v", err)
			}
		})
	}
}

func configuredState(t *testing.T, engine *Engine, args ...string) *xmain.State {
	t.Helper()
	state := &xmain.State{Opts: xmain.NewOpts(nil, nil)}
	for _, flag := range engine.Flags {
		flag.AddToOpts(state.Opts)
	}
	if err := state.Opts.Flags.Parse(args); err != nil {
		t.Fatal(err)
	}
	return state
}

func simpleGraph() *d2graph.Graph {
	graph := d2graph.NewGraph()
	object := &d2graph.Object{
		Graph:    graph,
		Parent:   graph.Root,
		ID:       "a",
		IDVal:    "a",
		Box:      geo.NewBox(geo.NewPoint(0, 0), 100, 60),
		Children: make(map[string]*d2graph.Object),
	}
	graph.Root.Children[object.ID] = object
	graph.Root.ChildrenArray = append(graph.Root.ChildrenArray, object)
	graph.Objects = append(graph.Objects, object)
	return graph
}

func layoutGraph(t *testing.T, layout d2graph.LayoutGraph) string {
	t.Helper()
	graph, _, err := d2compiler.Compile("test.d2", strings.NewReader("a -> b\na -> c\nb -> d\nc -> d\nb -> b"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := layout(context.Background(), graph); err != nil {
		t.Fatal(err)
	}
	data, err := d2graph.SerializeGraph(graph)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
