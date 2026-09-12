// Package d2layout selects and configures D2's built-in layout engines.
package d2layout

import (
	"fmt"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/util-go/xmain"
)

// Engine is one independently configured built-in layout engine.
type Engine struct {
	Name       string
	ShortHelp  string
	LongHelp   string
	Flags      []Flag
	Layout     d2graph.LayoutGraph
	RouteEdges d2graph.RouteEdges

	features  features
	configure func(*xmain.State) error
}

// Configure applies CLI options, using defaults for flags absent from ms.
// A nil state restores the defaults. Layout calls snapshot their options, so
// configuration can safely change while earlier layouts are running.
func (e *Engine) Configure(ms *xmain.State) error {
	return e.configure(ms)
}

// List returns fresh instances of the built-in engines included in this build.
func List() []*Engine {
	engines := make([]*Engine, 0, 3)
	if engine := newDagre(); engine != nil {
		engines = append(engines, engine)
	}
	if engine := newELK(); engine != nil {
		engines = append(engines, engine)
	}
	return append(engines, newTALA())
}

// Find returns a fresh instance of the named built-in engine.
func Find(name string) (*Engine, error) {
	var engine *Engine
	switch strings.ToLower(name) {
	case "dagre":
		engine = newDagre()
	case "elk":
		engine = newELK()
	case "tala":
		engine = newTALA()
	}
	if engine == nil {
		return nil, fmt.Errorf("layout engine %q is not available in this build", name)
	}
	return engine, nil
}

// Flag describes a command-line option for a built-in layout engine.
type Flag struct {
	Name    string
	Default any
	Usage   string
}

func (f Flag) AddToOpts(opts *xmain.Opts) {
	switch value := f.Default.(type) {
	case string:
		opts.String("", f.Name, "", value, f.Usage)
	case int64:
		opts.Int64("", f.Name, "", value, f.Usage)
	case []int64:
		opts.Int64Slice("", f.Name, "", slices.Clone(value), f.Usage)
	}
}

func helpWithFlags(help string, flags []Flag) string {
	opts := xmain.NewOpts(nil, nil)
	for _, flag := range flags {
		flag.AddToOpts(opts)
	}
	return help + "\nFlags:\n" + opts.Defaults() + "\n"
}

// optionValues reads the fixed set of typed engine flags and remembers the
// first invalid flag type, so a failed configuration never partially applies.
type optionValues struct {
	state *xmain.State
	err   error
}

func (v *optionValues) has(name string) bool {
	return v.state != nil && v.state.Opts != nil && v.state.Opts.Flags != nil &&
		v.state.Opts.Flags.Lookup(name) != nil
}

func (v *optionValues) string(name, fallback string) string {
	if !v.has(name) || v.err != nil {
		return fallback
	}
	value, err := v.state.Opts.Flags.GetString(name)
	v.err = err
	return value
}

func (v *optionValues) integer(name string, fallback int) int {
	if !v.has(name) || v.err != nil {
		return fallback
	}
	value, err := v.state.Opts.Flags.GetInt64(name)
	v.err = err
	return int(value)
}

func (v *optionValues) integers(name string, fallback []int64) []int64 {
	if !v.has(name) || v.err != nil {
		return slices.Clone(fallback)
	}
	value, err := v.state.Opts.Flags.GetInt64Slice(name)
	v.err = err
	return slices.Clone(value)
}
