// Package localfile provides explicit policies for opening local files.
// The zero value denies access. Callers must deliberately choose either a
// symlink-safe filesystem root or unrestricted host-filesystem access.
package localfile

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrDenied reports that a policy does not permit a local-file path.
var ErrDenied = errors.New("localfile: access denied")

type policyMode uint8

const (
	modeDenied policyMode = iota
	modeRooted
	modeUnrestricted
)

// Policy controls access to local files. Its zero value denies all local-file
// access and is safe for untrusted input.
//
// Policy implements fs.FS so it can also be supplied explicitly to D2's
// compiler APIs when imports are desired.
type Policy struct {
	mode policyMode
	root string
}

// Rooted returns a policy that permits files at or beneath root. Opens use
// os.Root, so symbolic links cannot escape root on supported platforms.
func Rooted(root string) (Policy, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Policy{}, fmt.Errorf("localfile: resolve root %q: %w", root, err)
	}
	absolute = filepath.Clean(absolute)
	handle, err := os.OpenRoot(absolute)
	if err != nil {
		return Policy{}, fmt.Errorf("localfile: open root %q: %w", root, err)
	}
	if err := handle.Close(); err != nil {
		return Policy{}, fmt.Errorf("localfile: close root %q: %w", root, err)
	}
	return Policy{mode: modeRooted, root: absolute}, nil
}

// Unrestricted returns a policy that permits arbitrary host-filesystem reads.
// Use it only for trusted local input, such as the D2 command-line interface.
func Unrestricted() Policy {
	return Policy{mode: modeUnrestricted}
}

// RootPath returns the absolute configured root and true for a rooted policy.
// It returns an empty string and false for denied and unrestricted policies.
func (p Policy) RootPath() (string, bool) {
	return p.root, p.mode == modeRooted
}

// CacheKey returns a policy-scoped, canonical key for name. It rejects names
// outside the policy's lexical scope; Open additionally enforces the rooted
// symbolic-link boundary. The key prevents a cache filled under an unrestricted
// policy from bypassing a rooted or denied policy.
func (p Policy) CacheKey(name string) (string, error) {
	resolved, err := p.resolve(name)
	if err != nil {
		return "", err
	}
	switch p.mode {
	case modeRooted:
		rootHash := sha256.Sum256([]byte(p.root))
		return fmt.Sprintf("root:%x:%s", rootHash, filepath.ToSlash(resolved.relative)), nil
	case modeUnrestricted:
		return "unrestricted:" + filepath.ToSlash(resolved.absolute), nil
	default:
		panic("localfile: resolved path under invalid policy mode")
	}
}

// Open opens name according to the policy. For rooted policies, relative names
// are interpreted relative to the configured root. Absolute names are accepted
// only when they are lexically beneath that root, and os.Root enforces the same
// boundary while following symbolic links.
func (p Policy) Open(name string) (fs.File, error) {
	resolved, err := p.resolve(name)
	if err != nil {
		return nil, err
	}
	if p.mode == modeUnrestricted {
		return os.Open(resolved.absolute)
	}

	root, err := os.OpenRoot(p.root)
	if err != nil {
		return nil, fmt.Errorf("localfile: open root %q: %w", p.root, err)
	}
	file, openErr := root.Open(resolved.relative)
	closeErr := root.Close()
	if openErr != nil {
		var closeRootErr error
		if closeErr != nil {
			closeRootErr = fmt.Errorf("localfile: close root %q: %w", p.root, closeErr)
		}
		return nil, errors.Join(
			fmt.Errorf("localfile: open %q beneath root %q: %w", name, p.root, openErr),
			closeRootErr,
		)
	}
	if closeErr != nil {
		return nil, errors.Join(
			fmt.Errorf("localfile: close root %q: %w", p.root, closeErr),
			file.Close(),
		)
	}
	return file, nil
}

type resolvedPath struct {
	absolute string
	relative string
}

func (p Policy) resolve(name string) (resolvedPath, error) {
	switch p.mode {
	case modeDenied:
		return resolvedPath{}, fmt.Errorf("%w: local-file access is disabled", ErrDenied)
	case modeUnrestricted:
		absolute, err := filepath.Abs(name)
		if err != nil {
			return resolvedPath{}, fmt.Errorf("localfile: resolve %q: %w", name, err)
		}
		return resolvedPath{absolute: filepath.Clean(absolute)}, nil
	case modeRooted:
		var relative string
		if filepath.IsAbs(name) {
			var err error
			relative, err = filepath.Rel(p.root, filepath.Clean(name))
			if err != nil {
				return resolvedPath{}, fmt.Errorf("localfile: resolve %q beneath root %q: %w", name, p.root, err)
			}
		} else {
			relative = filepath.Clean(name)
		}
		if !filepath.IsLocal(relative) {
			return resolvedPath{}, fmt.Errorf("%w: %q is outside configured root %q", ErrDenied, name, p.root)
		}
		return resolvedPath{
			absolute: filepath.Join(p.root, relative),
			relative: relative,
		}, nil
	default:
		return resolvedPath{}, errors.New("localfile: invalid policy")
	}
}
