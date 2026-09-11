package d2cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/xmain"

	"github.com/d2lang/d2/d2target"
)

func TestBoardOutputComponent(t *testing.T) {
	for _, name := range []string{"board", "board name", "release.v2", "日本語"} {
		if got := boardOutputComponent(name); got != name {
			t.Errorf("boardOutputComponent(%q) = %q, want unchanged", name, got)
		}
	}

	unsafeNames := []string{
		".",
		"..",
		"../escape",
		`..\escape`,
		"nested/board",
		`nested\board`,
		"/absolute/board",
		`\absolute\board`,
		`C:\absolute\board`,
		`C:drive-relative`,
		`\\server\share\board`,
		"NUL.txt",
		"index",
		"INDEX",
		"layers",
		"scenarios",
		"steps",
		escapedBoardOutputPrefix + "literal",
		"_D2_literal",
	}
	seen := make(map[string]string)
	for _, name := range unsafeNames {
		got := boardOutputComponent(name)
		if got != boardOutputComponent(name) {
			t.Fatalf("boardOutputComponent(%q) is not deterministic", name)
		}
		if got == name || !strings.HasPrefix(got, escapedBoardOutputPrefix) {
			t.Errorf("boardOutputComponent(%q) = %q, want escaped component", name, got)
		}
		if strings.ContainsAny(got, `/\`) || filepath.IsAbs(got) || got == "." || got == ".." {
			t.Errorf("boardOutputComponent(%q) = unsafe component %q", name, got)
		}
		if previous, ok := seen[got]; ok {
			t.Errorf("board names %q and %q map to the same component %q", previous, name, got)
		}
		seen[got] = name
	}
}

func TestValidateBoardOutputPathsRejectsCollisions(t *testing.T) {
	for _, test := range []struct {
		name   string
		first  string
		second string
	}{
		{name: "case insensitive filesystem", first: "Board", second: "board"},
		{name: "same escaped component", first: "../board", second: "../board"},
	} {
		t.Run(test.name, func(t *testing.T) {
			diagram := &d2target.Diagram{Layers: []*d2target.Diagram{
				{Name: test.first},
				{Name: test.second},
			}}
			err := validateBoardOutputPaths(filepath.Join(t.TempDir(), "output.svg"), diagram)
			if err == nil || !strings.Contains(err.Error(), "same output path") {
				t.Fatalf("validateBoardOutputPaths() error = %v, want collision", err)
			}
		})
	}
}

func TestAppendBoardOutputNameStaysContained(t *testing.T) {
	root := filepath.Join(t.TempDir(), "output")
	for _, name := range []string{
		"..",
		"../../escape",
		"nested/board",
		`nested\board`,
		"/absolute/board",
		`C:\absolute\board`,
		`\\server\share\board`,
	} {
		path, err := appendBoardOutputName(root+".svg", name)
		if err != nil {
			t.Fatalf("appendBoardOutputName(%q): %v", name, err)
		}
		path = strings.TrimSuffix(path, ".svg")
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("appendBoardOutputName(%q) produced escaping path %q", name, path)
		}
	}

	if err := ensurePathWithin(root, filepath.Join(root, "..", "escape")); err == nil {
		t.Fatal("ensurePathWithin accepted a parent traversal")
	}

	hiddenOutput := filepath.Join(t.TempDir(), ".svg")
	hiddenBoard, err := appendBoardOutputComponent(hiddenOutput, "index")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(hiddenOutput, "index") + ".svg"; hiddenBoard != want {
		t.Fatalf("hidden output board path = %q, want %q", hiddenBoard, want)
	}
}

func TestMultiboardOutputContainsUnsafeNames(t *testing.T) {
	directory := t.TempDir()
	workDirectory := filepath.Join(directory, "work")
	victimDirectory := filepath.Join(directory, "victim")
	if err := os.MkdirAll(workDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(victimDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinelPath := filepath.Join(victimDirectory, "sentinel.txt")
	if err := os.WriteFile(sentinelPath, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	inputPath := filepath.Join(workDirectory, "input.d2")
	if err := os.WriteFile(inputPath, []byte(`root
layers: {
  "../../victim": {
    child
    layers: {
      nested: {
        leaf
      }
    }
  }
  index: {
    structural-name
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(workDirectory, "output.svg")
	runBoardOutputCLI(t, workDirectory, inputPath, outputPath)

	if got, err := os.ReadFile(sentinelPath); err != nil || string(got) != "keep me" {
		t.Fatalf("outside sentinel = %q, %v; want unchanged", got, err)
	}
	escaped := boardOutputComponent("../../victim")
	for _, path := range []string{
		filepath.Join(workDirectory, "output", "index.svg"),
		filepath.Join(workDirectory, "output", escaped, "index.svg"),
		filepath.Join(workDirectory, "output", escaped, "nested.svg"),
		filepath.Join(workDirectory, "output", boardOutputComponent("index")+".svg"),
	} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Errorf("expected rendered board %q: %v", path, err)
		}
	}
	if matches, err := filepath.Glob(filepath.Join(workDirectory, ".d2-board-output-*")); err != nil || len(matches) != 0 {
		t.Fatalf("staging directories after successful render = %v, %v", matches, err)
	}
}

func TestMultiboardOutputPreservesPreexistingDirectory(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.d2")
	if err := os.WriteFile(inputPath, []byte(`root
layers: {
  parent: {
    parent-shape
    layers: {
      child: {
        child-shape
      }
    }
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(directory, "output.svg")
	outputDirectory := filepath.Join(directory, "output")
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinelPath := filepath.Join(outputDirectory, "sentinel.txt")
	stalePath := filepath.Join(outputDirectory, "previous-board.svg")
	if err := os.WriteFile(sentinelPath, []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}

	runBoardOutputCLI(t, directory, inputPath, outputPath)
	// A second render exercises replacement of files generated by the first
	// render while the unrelated pre-existing files remain in place.
	runBoardOutputCLI(t, directory, inputPath, outputPath)

	for path, want := range map[string]string{sentinelPath: "unrelated", stalePath: "previous"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("pre-existing file %q = %q, %v; want %q", path, got, err, want)
		}
	}
	for _, path := range []string{
		filepath.Join(outputDirectory, "index.svg"),
		filepath.Join(outputDirectory, "parent", "index.svg"),
		filepath.Join(outputDirectory, "parent", "child.svg"),
	} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Errorf("expected rendered board %q: %v", path, err)
		}
	}
}

func TestMultiboardOutputRejectsPreexistingSymlink(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.d2")
	if err := os.WriteFile(inputPath, []byte(`root
layers: {
  child: {
    child-shape
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(directory, "output.svg")
	outputDirectory := filepath.Join(directory, "output")
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	victimPath := filepath.Join(directory, "victim.svg")
	if err := os.WriteFile(victimPath, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victimPath, filepath.Join(outputDirectory, "child.svg")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := runBoardOutputCLIResult(t, directory, inputPath, outputPath)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run() error = %v, want symlink rejection", err)
	}
	if got, err := os.ReadFile(victimPath); err != nil || string(got) != "keep me" {
		t.Fatalf("symlink target = %q, %v; want unchanged", got, err)
	}
	if _, err := os.Stat(filepath.Join(outputDirectory, "index.svg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("root output was published before symlink preflight completed: %v", err)
	}
}

func runBoardOutputCLI(t *testing.T, directory string, inputPath, outputPath string) {
	t.Helper()
	if err := runBoardOutputCLIResult(t, directory, inputPath, outputPath); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}
}

func runBoardOutputCLIResult(t *testing.T, directory string, inputPath, outputPath string) error {
	t.Helper()
	state := &xmain.TestState{
		Run:  Run,
		Args: []string{"d2", inputPath, outputPath},
		PWD:  directory,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state.Start(t, ctx)
	defer state.Cleanup(t)
	return state.Wait(ctx)
}
