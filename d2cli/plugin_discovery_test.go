package d2cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/util-go/xmain"
)

const (
	pluginDiscoveryHelperEnv       = "D2_CLI_PLUGIN_DISCOVERY_HELPER_PROCESS"
	pluginDiscoveryHelperMarkerEnv = "D2_CLI_PLUGIN_DISCOVERY_HELPER_MARKER"
	pluginDiscoveryHelperNameEnv   = "D2_CLI_PLUGIN_DISCOVERY_HELPER_NAME"
	pluginDiscoveryDefault         = "declared-default"
)

func TestMain(m *testing.M) {
	if os.Getenv(pluginDiscoveryHelperEnv) == "1" {
		runPluginDiscoveryHelper()
	}
	os.Exit(m.Run())
}

func runPluginDiscoveryHelper() {
	if marker := os.Getenv(pluginDiscoveryHelperMarkerEnv); marker != "" {
		f, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
		err = json.NewEncoder(f).Encode(os.Args[1:])
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			if err == nil {
				err = closeErr
			}
			fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
	}

	if len(os.Args) == 2 && os.Args[1] == "info" {
		name := os.Getenv(pluginDiscoveryHelperNameEnv)
		if name == "" {
			name = "external"
		}
		fmt.Fprintf(os.Stdout, `{"name":%q,"features":[]}`, name)
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "flags" {
		fmt.Fprintf(os.Stdout, `[{"Name":"external-option","Type":"string","Default":%q,"Usage":"test option","Tag":"external-option"}]`, pluginDiscoveryDefault)
		os.Exit(0)
	}
	if len(os.Args) >= 2 && os.Args[1] == "layout" {
		in, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
		var graph d2graph.Graph
		if err := d2graph.DeserializeGraph(in, &graph); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
		if err := d2dagrelayout.DefaultLayout(context.Background(), &graph); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
		out, err := d2graph.SerializeGraph(&graph)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
		if _, err := os.Stdout.Write(out); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "postprocess" {
		if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}

	fmt.Fprintf(os.Stderr, "unexpected helper arguments: %q", os.Args[1:])
	os.Exit(2)
}

func TestLayoutFromArgs(t *testing.T) {
	testCases := []struct {
		name   string
		args   []string
		want   string
		wantOK bool
	}{
		{name: "fallback", args: []string{"in.d2"}, want: "dagre", wantOK: true},
		{name: "long separate", args: []string{"--layout", "elk", "in.d2"}, want: "elk", wantOK: true},
		{name: "long equals", args: []string{"--layout=tala", "in.d2"}, want: "tala", wantOK: true},
		{name: "short separate", args: []string{"-l", "elk", "in.d2"}, want: "elk", wantOK: true},
		{name: "short equals", args: []string{"-l=tala", "in.d2"}, want: "tala", wantOK: true},
		{name: "short attached", args: []string{"-lelk", "in.d2"}, want: "elk", wantOK: true},
		{name: "combined shorthand", args: []string{"-sltala", "in.d2"}, want: "tala", wantOK: true},
		{name: "last wins", args: []string{"-l", "elk", "--layout=tala", "in.d2"}, want: "tala", wantOK: true},
		{name: "end of flags", args: []string{"--", "--layout=elk"}, want: "dagre", wantOK: true},
		{name: "help terminates", args: []string{"--help", "--layout=external"}, want: "dagre", wantOK: false},
		{name: "known flag consumes layout", args: []string{"--browser", "--layout=external", "--version"}, want: "dagre", wantOK: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.StringP("layout", "l", "dagre", "")
			flags.BoolP("sketch", "s", false, "")
			flags.String("browser", "", "")
			flags.Bool("version", false, "")
			got, gotOK := layoutFromArgs(tc.args, flags, "dagre")
			if got != tc.want || gotOK != tc.wantOK {
				t.Fatalf("layoutFromArgs(%q) = (%q, %t), want (%q, %t)", tc.args, got, gotOK, tc.want, tc.wantOK)
			}
		})
	}
}

func TestRunExecutesOnlyParserSelectedExternalPlugin(t *testing.T) {
	testCases := []struct {
		name        string
		args        []string
		wantCalls   [][]string
		wantNoCalls bool
	}{
		{
			name:        "help terminates before layout",
			args:        []string{"--help", "--layout=external"},
			wantNoCalls: true,
		},
		{
			name:        "browser consumes layout-looking value",
			args:        []string{"--browser", "--layout=external", "--version"},
			wantNoCalls: true,
		},
		{
			name:      "combined shorthand selects layout",
			args:      []string{"-slexternal", "layout"},
			wantCalls: [][]string{{"info"}, {"flags"}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			marker := filepath.Join(directory, "plugin-calls.jsonl")
			installPluginDiscoveryHelper(t, directory, "external")
			t.Setenv(pluginDiscoveryHelperEnv, "1")
			t.Setenv(pluginDiscoveryHelperMarkerEnv, marker)
			t.Setenv(pluginDiscoveryHelperNameEnv, "external")
			t.Setenv("PATH", directory)

			if err := runPluginDiscoveryCLI(t, directory, tc.args...); err != nil {
				t.Fatal(err)
			}
			calls := readPluginDiscoveryCalls(t, marker)
			if tc.wantNoCalls && len(calls) != 0 {
				t.Fatalf("unselected external plugin calls = %q, want none", calls)
			}
			if tc.wantCalls != nil && !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("selected external plugin calls = %q, want %q", calls, tc.wantCalls)
			}
		})
	}
}

func TestRunSourceConfigExternalPluginUsesDeclaredFlagDefaults(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "plugin-calls.jsonl")
	installPluginDiscoveryHelper(t, directory, "external")
	t.Setenv(pluginDiscoveryHelperEnv, "1")
	t.Setenv(pluginDiscoveryHelperMarkerEnv, marker)
	t.Setenv(pluginDiscoveryHelperNameEnv, "external")
	t.Setenv("PATH", directory)

	input := `vars: {d2-config: {layout-engine: external}}
x
`
	if err := os.WriteFile(filepath.Join(directory, "input.d2"), []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runPluginDiscoveryCLI(t, directory, "input.d2", "output.svg"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "output.svg")); err != nil {
		t.Fatalf("render output: %v", err)
	}

	wantLayout := []string{"layout", "--external-option", pluginDiscoveryDefault}
	for _, call := range readPluginDiscoveryCalls(t, marker) {
		if len(call) > 0 && call[0] == "layout" {
			if !reflect.DeepEqual(call, wantLayout) {
				t.Fatalf("external layout call = %q, want %q", call, wantLayout)
			}
			return
		}
	}
	t.Fatal("external layout helper was not called")
}

func runPluginDiscoveryCLI(t *testing.T, directory string, args ...string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state := &xmain.TestState{
		Run:  Run,
		Args: append([]string{"d2"}, args...),
		PWD:  directory,
	}
	state.Start(t, ctx)
	defer state.Cleanup(t)
	return state.Wait(ctx)
}

func installPluginDiscoveryHelper(t *testing.T, directory, name string) {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binaryName := "d2plugin-" + name
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(directory, binaryName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func readPluginDiscoveryCalls(t *testing.T, marker string) [][]string {
	t.Helper()
	f, err := os.Open(marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var calls [][]string
	decoder := json.NewDecoder(f)
	for {
		var call []string
		if err := decoder.Decode(&call); errors.Is(err, io.EOF) {
			return calls
		} else if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
}
