// Package config performs the only file reading HowlForge does.
//
// Every byte HowlForge parses arrives from a role file, a runtime file, a
// persona file, an availability state file or a handoff bundle, and all of them
// are untrusted input. Centralizing reads here means the traversal guard, the
// size cap and the strict decoders cannot be forgotten at one call site.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Default limits. They are generous for legitimate configuration and small
// enough that a hostile file cannot exhaust memory.
const (
	DefaultMaxFileBytes = 1 << 20 // 1 MiB per configuration file
	DefaultMaxFiles     = 1024    // per directory listing
)

// ErrNotFound is returned when a requested path does not exist.
var ErrNotFound = errors.New("not found")

// Loader reads files confined to a single resolved root directory.
type Loader struct {
	root         string
	maxFileBytes int64
	maxFiles     int
}

// Option customizes a Loader.
type Option func(*Loader)

// WithMaxFileBytes overrides the per-file size cap.
func WithMaxFileBytes(n int64) Option {
	return func(l *Loader) { l.maxFileBytes = n }
}

// WithMaxFiles overrides the per-directory file count cap.
func WithMaxFiles(n int) Option {
	return func(l *Loader) { l.maxFiles = n }
}

// NewLoader resolves root and returns a Loader confined to it. Symlinks in the
// root itself are resolved once, so the confinement check downstream compares
// real paths against a real path.
func NewLoader(root string, opts ...Option) (*Loader, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("configuration root must not be empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve configuration root %q: %w", root, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("configuration root %q: %w", root, ErrNotFound)
		}
		return nil, fmt.Errorf("resolve configuration root %q: %w", root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat configuration root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("configuration root %q is not a directory", root)
	}
	l := &Loader{
		root:         resolved,
		maxFileBytes: DefaultMaxFileBytes,
		maxFiles:     DefaultMaxFiles,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l, nil
}

// Root returns the resolved root directory.
func (l *Loader) Root() string { return l.root }

// resolve turns a root-relative path into a confined absolute path. It rejects
// absolute inputs, parent traversal and any symlink whose target escapes the
// root.
func (l *Loader) resolve(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q must be relative to %s", rel, l.root)
	}
	cleaned := filepath.Clean(rel)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes %s", rel, l.root)
	}
	joined := filepath.Join(l.root, cleaned)

	// EvalSymlinks fails on a path that does not exist, so fall back to the
	// lexical check for missing files and let the caller report ErrNotFound.
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if !l.contains(joined) {
				return "", fmt.Errorf("path %q escapes %s", rel, l.root)
			}
			return joined, nil
		}
		return "", fmt.Errorf("resolve path %q: %w", rel, err)
	}
	if !l.contains(resolved) {
		return "", fmt.Errorf("path %q resolves outside %s (symlink escape refused)", rel, l.root)
	}
	return resolved, nil
}

// contains reports whether an absolute path lies inside the root.
func (l *Loader) contains(path string) bool {
	if path == l.root {
		return true
	}
	return strings.HasPrefix(path, l.root+string(filepath.Separator))
}

// ReadFile returns the contents of a root-relative regular file, refusing
// anything larger than the configured cap.
func (l *Loader) ReadFile(rel string) ([]byte, error) {
	resolved, err := l.resolve(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", rel, ErrNotFound)
		}
		return nil, fmt.Errorf("stat %s: %w", rel, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory, not a file", rel)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", rel)
	}
	if info.Size() > l.maxFileBytes {
		return nil, fmt.Errorf("%s is %d bytes, over the %d byte limit", rel, info.Size(), l.maxFileBytes)
	}

	file, err := os.Open(resolved)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", rel, err)
	}
	defer file.Close()

	// Read one byte past the cap so a file that grew between Stat and Open is
	// still refused rather than truncated silently.
	data, err := io.ReadAll(io.LimitReader(file, l.maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	if int64(len(data)) > l.maxFileBytes {
		return nil, fmt.Errorf("%s exceeds the %d byte limit", rel, l.maxFileBytes)
	}
	return data, nil
}

// Exists reports whether a root-relative path exists inside the root.
func (l *Loader) Exists(rel string) bool {
	resolved, err := l.resolve(rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(resolved)
	return err == nil
}

// ListFiles returns the regular files directly inside a root-relative
// directory whose names carry one of the given extensions, in lexical order.
// Subdirectories are not descended into: configuration is a flat set of files
// per kind, which keeps load order obvious and prevents a deep tree from
// turning a listing into a walk.
func (l *Loader) ListFiles(relDir string, extensions ...string) ([]string, error) {
	resolved, err := l.resolve(relDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", relDir, ErrNotFound)
		}
		return nil, fmt.Errorf("read directory %s: %w", relDir, err)
	}
	if len(entries) > l.maxFiles {
		return nil, fmt.Errorf("%s holds %d entries, over the %d entry limit", relDir, len(entries), l.maxFiles)
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !hasExtension(name, extensions) {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Join(relDir, name)))
	}
	sort.Strings(out)
	return out, nil
}

func hasExtension(name string, extensions []string) bool {
	if len(extensions) == 0 {
		return true
	}
	lower := strings.ToLower(name)
	for _, ext := range extensions {
		if strings.HasSuffix(lower, strings.ToLower(ext)) {
			return true
		}
	}
	return false
}

// DecodeYAMLStrict decodes YAML into target, rejecting any field the target
// does not declare. Unknown fields are a hard error because a misspelled
// capability key that decodes to nothing would silently weaken a role.
func DecodeYAMLStrict(data []byte, target interface{}) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("file is empty")
		}
		return err
	}
	// Refuse trailing documents so a second document cannot hide behind the
	// first one that parsed cleanly.
	var extra interface{}
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("file contains more than one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// DecodeJSONStrict decodes JSON into target, rejecting unknown fields and
// trailing content.
func DecodeJSONStrict(data []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("file is empty")
		}
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("file contains trailing content after the JSON document")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
