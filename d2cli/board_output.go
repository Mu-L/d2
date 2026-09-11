package d2cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/d2lang/d2/d2target"
)

const escapedBoardOutputPrefix = "_d2_"

// boardOutputComponent preserves ordinary board names while mapping names that
// have filesystem meaning to a portable, single path component. The prefix is
// reserved so a literal board name cannot collide with an escaped name.
func boardOutputComponent(name string) string {
	if isPortableBoardOutputComponent(name) && !isReservedBoardOutputComponent(name) {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return escapedBoardOutputPrefix + hex.EncodeToString(sum[:])
}

func isReservedBoardOutputComponent(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, escapedBoardOutputPrefix) {
		return true
	}
	switch lower {
	case "index", "layers", "scenarios", "steps":
		return true
	default:
		return false
	}
}

func isPortableBoardOutputComponent(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	if !filepath.IsLocal(name) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			return false
		}
	}

	// Windows reserves these names even when they have an extension.
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}

func appendBoardOutputName(outputPath, boardName string) (string, error) {
	return appendBoardOutputComponent(outputPath, boardOutputComponent(boardName))
}

func appendBoardOutputComponent(outputPath, component string) (string, error) {
	outputDir, ext := splitBoardOutputPath(outputPath)
	joined := filepath.Join(outputDir, component)
	if err := ensurePathWithin(outputDir, joined); err != nil {
		return "", fmt.Errorf("invalid board output component %q: %w", component, err)
	}
	return joined + ext, nil
}

func splitBoardOutputPath(outputPath string) (root, ext string) {
	ext = filepath.Ext(outputPath)
	if strings.TrimSuffix(filepath.Base(outputPath), ext) == "" {
		return outputPath, ext
	}
	return strings.TrimSuffix(outputPath, ext), ext
}

func ensurePathWithin(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return err
	}
	if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes root %q", path, root)
	}
	return nil
}

type boardOutputPaths struct {
	writePath   string
	displayPath string
}

type boardOutputPlan struct {
	base      boardOutputPaths
	board     boardOutputPaths
	layers    boardOutputPaths
	scenarios boardOutputPaths
	steps     boardOutputPaths
}

func newBoardOutputPaths(path string) boardOutputPaths {
	return boardOutputPaths{writePath: path, displayPath: path}
}

func (p boardOutputPaths) withBoardName(name string) (boardOutputPaths, error) {
	writePath, err := appendBoardOutputName(p.writePath, name)
	if err != nil {
		return boardOutputPaths{}, err
	}
	displayPath, err := appendBoardOutputName(p.displayPath, name)
	if err != nil {
		return boardOutputPaths{}, err
	}
	return boardOutputPaths{writePath: writePath, displayPath: displayPath}, nil
}

func (p boardOutputPaths) withComponent(component string) (boardOutputPaths, error) {
	writePath, err := appendBoardOutputComponent(p.writePath, component)
	if err != nil {
		return boardOutputPaths{}, err
	}
	displayPath, err := appendBoardOutputComponent(p.displayPath, component)
	if err != nil {
		return boardOutputPaths{}, err
	}
	return boardOutputPaths{writePath: writePath, displayPath: displayPath}, nil
}

func planBoardOutput(paths boardOutputPaths, diagram *d2target.Diagram) (boardOutputPlan, error) {
	var err error
	if diagram.Name != "" {
		paths, err = paths.withBoardName(diagram.Name)
		if err != nil {
			return boardOutputPlan{}, err
		}
	}

	plan := boardOutputPlan{
		base:      paths,
		board:     paths,
		layers:    paths,
		scenarios: paths,
		steps:     paths,
	}
	if len(diagram.Layers) > 0 || len(diagram.Scenarios) > 0 || len(diagram.Steps) > 0 {
		plan.board, err = plan.board.withComponent("index")
		if err != nil {
			return boardOutputPlan{}, err
		}
	}
	if len(diagram.Scenarios) > 0 || len(diagram.Steps) > 0 {
		plan.layers, err = plan.layers.withComponent("layers")
		if err != nil {
			return boardOutputPlan{}, err
		}
	}
	if len(diagram.Layers) > 0 || len(diagram.Steps) > 0 {
		plan.scenarios, err = plan.scenarios.withComponent("scenarios")
		if err != nil {
			return boardOutputPlan{}, err
		}
	}
	if len(diagram.Layers) > 0 || len(diagram.Scenarios) > 0 {
		plan.steps, err = plan.steps.withComponent("steps")
		if err != nil {
			return boardOutputPlan{}, err
		}
	}
	return plan, nil
}

func validateBoardOutputPaths(outputPath string, diagram *d2target.Diagram) error {
	return validateBoardOutputPathsRecursive("root", newBoardOutputPaths(outputPath), diagram, make(map[string]string))
}

func validateBoardOutputPathsRecursive(diagramPath string, outputPaths boardOutputPaths, diagram *d2target.Diagram, seen map[string]string) error {
	plan, err := planBoardOutput(outputPaths, diagram)
	if err != nil {
		return err
	}
	if !diagram.IsFolderOnly {
		key := boardOutputCollisionKey(plan.board.displayPath)
		if previous, ok := seen[key]; ok {
			return fmt.Errorf("boards %q and %q resolve to the same output path %q", previous, diagramPath, plan.board.displayPath)
		}
		seen[key] = diagramPath
	}
	for _, child := range diagram.Layers {
		if err := validateBoardOutputPathsRecursive(diagramPath+".layers."+child.Name, plan.layers, child, seen); err != nil {
			return err
		}
	}
	for _, child := range diagram.Scenarios {
		if err := validateBoardOutputPathsRecursive(diagramPath+".scenarios."+child.Name, plan.scenarios, child, seen); err != nil {
			return err
		}
	}
	for _, child := range diagram.Steps {
		if err := validateBoardOutputPathsRecursive(diagramPath+".steps."+child.Name, plan.steps, child, seen); err != nil {
			return err
		}
	}
	return nil
}

func boardOutputCollisionKey(path string) string {
	return norm.NFC.String(strings.ToLower(filepath.Clean(path)))
}

// boardOutputWorkspace keeps all content-derived paths inside a directory
// created by this process. Publishing merges generated files into an existing
// output tree without recursively deleting that tree or unrelated files.
type boardOutputWorkspace struct {
	finalRoot string
	stageRoot string
	stageInfo fs.FileInfo
	extension string
}

func newBoardOutputWorkspace(outputPath string) (*boardOutputWorkspace, error) {
	finalRoot, ext := splitBoardOutputPath(outputPath)
	if err := validateBoardOutputRoot(finalRoot); err != nil {
		return nil, err
	}
	parent := filepath.Dir(finalRoot)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("create board output parent: %w", err)
	}
	stageRoot, err := os.MkdirTemp(parent, ".d2-board-output-")
	if err != nil {
		return nil, fmt.Errorf("create board output staging directory: %w", err)
	}
	if err := os.Chmod(stageRoot, 0o755); err != nil {
		return nil, errors.Join(
			fmt.Errorf("set board output staging permissions: %w", err),
			func() error {
				if cleanupErr := os.Remove(stageRoot); cleanupErr != nil {
					return fmt.Errorf("remove board output staging directory %q: %w", stageRoot, cleanupErr)
				}
				return nil
			}(),
		)
	}
	stageInfo, err := os.Lstat(stageRoot)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("inspect board output staging directory: %w", err),
			func() error {
				if cleanupErr := os.Remove(stageRoot); cleanupErr != nil {
					return fmt.Errorf("remove board output staging directory %q: %w", stageRoot, cleanupErr)
				}
				return nil
			}(),
		)
	}
	return &boardOutputWorkspace{finalRoot: finalRoot, stageRoot: stageRoot, stageInfo: stageInfo, extension: ext}, nil
}

func validateBoardOutputRoot(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect board output root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("board output root %q must be a real directory", path)
	}
	return nil
}

func (w *boardOutputWorkspace) outputPaths(displayPath string) boardOutputPaths {
	return boardOutputPaths{
		writePath:   w.stageRoot + w.extension,
		displayPath: displayPath,
	}
}

func (w *boardOutputWorkspace) discard() error {
	if w.stageRoot == "" {
		return nil
	}
	stageRoot := w.stageRoot
	stageInfo, err := os.Lstat(stageRoot)
	if errors.Is(err, os.ErrNotExist) {
		w.stageRoot = ""
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect board output staging directory %q: %w", stageRoot, err)
	}
	if stageInfo.Mode()&os.ModeSymlink != 0 || !stageInfo.IsDir() || !os.SameFile(w.stageInfo, stageInfo) {
		return fmt.Errorf("refusing to remove replaced board output staging path %q", stageRoot)
	}
	if err := os.RemoveAll(stageRoot); err != nil {
		return fmt.Errorf("remove board output staging directory %q: %w", stageRoot, err)
	}
	w.stageRoot = ""
	return nil
}

type stagedBoardOutput struct {
	rel  string
	mode fs.FileMode
	dir  bool
}

func (w *boardOutputWorkspace) publish() (touched bool, err error) {
	defer func() {
		if cleanupErr := w.discard(); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()

	if err := validateBoardOutputRoot(w.finalRoot); err != nil {
		return false, err
	}
	if _, err := os.Lstat(w.finalRoot); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(w.stageRoot, w.finalRoot); err != nil {
			return false, fmt.Errorf("publish board output tree: %w", err)
		}
		w.stageRoot = ""
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect board output root: %w", err)
	}

	entries, err := w.preflightMerge()
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		destination := filepath.Join(w.finalRoot, entry.rel)
		if entry.dir {
			if entry.rel == "." {
				continue
			}
			if err := ensureRealDirectory(destination, entry.mode.Perm()); err != nil {
				return touched, err
			}
			continue
		}
		source := filepath.Join(w.stageRoot, entry.rel)
		if err := publishBoardOutputFile(source, destination, entry.mode.Perm()); err != nil {
			return touched, err
		}
		touched = true
	}
	return touched, nil
}

func (w *boardOutputWorkspace) preflightMerge() ([]stagedBoardOutput, error) {
	var entries []stagedBoardOutput
	err := filepath.WalkDir(w.stageRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(w.stageRoot, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(w.finalRoot, rel)
		if err := ensurePathWithin(w.finalRoot, destination); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("staged board output %q is not a regular file or directory", path)
		}

		destinationInfo, err := os.Lstat(destination)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			if destinationInfo.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to publish board output through symlink %q", destination)
			}
			if info.IsDir() != destinationInfo.IsDir() || (!info.IsDir() && !destinationInfo.Mode().IsRegular()) {
				return fmt.Errorf("board output path %q has an incompatible existing type", destination)
			}
		}
		entries = append(entries, stagedBoardOutput{rel: rel, mode: info.Mode(), dir: info.IsDir()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("preflight board output tree: %w", err)
	}
	return entries, nil
}

func ensureRealDirectory(path string, mode fs.FileMode) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, mode); err != nil {
			return fmt.Errorf("create board output directory %q: %w", path, err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("board output directory %q is not a real directory", path)
	}
	return nil
}

func publishBoardOutputFile(source, destination string, mode fs.FileMode) (err error) {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open staged board output %q: %w", source, err)
	}
	defer func() {
		err = errors.Join(err, input.Close())
	}()

	temporary, err := os.CreateTemp(filepath.Dir(destination), ".d2-board-file-")
	if err != nil {
		return fmt.Errorf("create temporary board output for %q: %w", destination, err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()

	if _, err := io.Copy(temporary, input); err != nil {
		return errors.Join(
			fmt.Errorf("copy board output to %q: %w", destination, err),
			temporary.Close(),
		)
	}
	if err := temporary.Chmod(mode); err != nil {
		return errors.Join(
			fmt.Errorf("set board output permissions for %q: %w", destination, err),
			temporary.Close(),
		)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary board output for %q: %w", destination, err)
	}

	renameErr := os.Rename(temporaryPath, destination)
	if renameErr == nil {
		return nil
	}

	// Windows does not replace an existing file with os.Rename. Move the old
	// regular file aside, install the completed replacement, then remove it.
	destinationInfo, statErr := os.Lstat(destination)
	if statErr != nil {
		return fmt.Errorf("publish board output %q: %w", destination, renameErr)
	}
	if !destinationInfo.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace non-regular board output %q", destination)
	}
	backupPath := temporaryPath + ".old"
	if renameErr := os.Rename(destination, backupPath); renameErr != nil {
		return fmt.Errorf("prepare existing board output %q for replacement: %w", destination, renameErr)
	}
	if renameErr := os.Rename(temporaryPath, destination); renameErr != nil {
		rollbackErr := os.Rename(backupPath, destination)
		return errors.Join(
			fmt.Errorf("publish board output %q: %w", destination, renameErr),
			func() error {
				if rollbackErr != nil {
					return fmt.Errorf("restore existing board output %q: %w", destination, rollbackErr)
				}
				return nil
			}(),
		)
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("remove replaced board output backup %q: %w", backupPath, err)
	}
	return nil
}
